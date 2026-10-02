package imagelookup

import (
	"context"
	"errors"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestImageLookupFetchSourceBuildYAMLUsesPinnedDeclaredGitHubSource(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	revision := "9c6326c89e3f89c092ff3a80c02bbde96195bccb"
	image := newImageLookupFixtureImage(t, "amd64", "", time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC), map[string]string{
		"README.txt": "source fallback fixture",
	})
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatalf("read source fixture config: %v", err)
	}
	config.Config.Labels[SourceLabel] = "https://github.com/rancher/rancher-prime.git"
	config.Config.Labels[RevisionLabel] = revision
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatalf("write source fixture config: %v", err)
	}
	referenceText := imageLookupTestServerHost(t, registryServer) + "/rancher/rancher:v2.16.0-rcs-0844.1"
	reference, err := name.NewTag(referenceText, name.StrictValidation, name.Insecure)
	if err != nil {
		t.Fatalf("parse source fixture reference: %v", err)
	}
	if err := remote.Write(reference, image,
		remote.WithTransport(registryServer.Client().Transport),
		remote.WithAuth(authn.Anonymous),
	); err != nil {
		t.Fatalf("push source fixture image: %v", err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatalf("source fixture digest: %v", err)
	}

	t.Setenv("GH_DEBUG", "api")
	t.Setenv("GH_PROMPT_DISABLED", "not-sanitized")
	t.Setenv("GIT_TERMINAL_PROMPT", "not-sanitized")
	t.Setenv("GH_PAGER", "not-sanitized")
	var commandCalls int
	var commandName string
	var commandArguments, commandEnvironment []string
	var commandLimit int64
	service.runCommand = func(ctx context.Context, executable string, arguments, environment []string, outputLimit int64) ([]byte, error) {
		commandCalls++
		commandName = executable
		commandArguments = append([]string(nil), arguments...)
		commandEnvironment = append([]string(nil), environment...)
		commandLimit = outputLimit
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > imageLookupGHTimeout+time.Second {
			t.Fatalf("command context deadline = %v, want a live deadline within %s", deadline, imageLookupGHTimeout)
		}
		return []byte("fleetVersion: 110.0.0+up0.16.0-rc.5\nwebhookVersion: 110.0.0+up0.11.0\n"), nil
	}

	response, err := service.FetchSourceBuildYAML(context.Background(), SourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest.String(),
	})
	if err != nil {
		t.Fatalf("FetchSourceBuildYAML: %v", err)
	}
	if commandCalls != 1 || commandName != "gh" || commandLimit != imageLookupMaxSourceBuildYAML {
		t.Fatalf("command invocation = calls %d name %q limit %d", commandCalls, commandName, commandLimit)
	}
	wantArguments := []string{
		"api",
		"--hostname", "github.com",
		"--method", http.MethodGet,
		"-H", "Accept:application/vnd.github.raw+json",
		"/repos/rancher/rancher-prime/contents/build.yaml?ref=" + revision,
	}
	if !slices.Equal(commandArguments, wantArguments) {
		t.Fatalf("gh arguments = %#v, want %#v", commandArguments, wantArguments)
	}
	environment := map[string]string{}
	for _, entry := range commandEnvironment {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			environment[parts[0]] = parts[1]
		}
	}
	if environment["GH_PROMPT_DISABLED"] != "1" || environment["GIT_TERMINAL_PROMPT"] != "0" || environment["GH_PAGER"] != "cat" {
		t.Fatalf("sanitized gh environment = %#v", environment)
	}
	if _, ok := environment["GH_DEBUG"]; ok {
		t.Fatalf("sanitized gh environment retained GH_DEBUG: %#v", environment)
	}
	if !response.Found || response.Path != "build.yaml" || response.Origin != "declared-source" {
		t.Fatalf("source build.yaml identity = %#v", response)
	}
	if response.Data["fleetVersion"] != "110.0.0+up0.16.0-rc.5" || response.Data["webhookVersion"] != "110.0.0+up0.11.0" {
		t.Fatalf("source build.yaml data = %#v", response.Data)
	}
	if response.Provenance.RepositoryURL != "https://github.com/rancher/rancher-prime" || response.Provenance.Revision != revision || response.Provenance.Path != "build.yaml" || response.Provenance.ImageReference != referenceText || response.Provenance.ImageDigest != digest.String() || response.Provenance.Platform != "linux/amd64" {
		t.Fatalf("source build.yaml provenance = %#v", response.Provenance)
	}
	if response.Provenance.SourceLabel != SourceLabel || response.Provenance.RevisionLabel != RevisionLabel {
		t.Fatalf("source build.yaml provenance labels = %#v", response.Provenance)
	}

	_, err = service.FetchSourceBuildYAML(context.Background(), SourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: "sha256:" + strings.Repeat("a", 64),
	})
	var conflictErr *imageLookupConflictError
	if !errors.As(err, &conflictErr) || HTTPStatus(err) != http.StatusConflict {
		t.Fatalf("moved digest error = %T %v, want conflict", err, err)
	}
	if commandCalls != 1 {
		t.Fatalf("moved digest invoked gh; command calls = %d", commandCalls)
	}

	service.runCommand = func(context.Context, string, []string, []string, int64) ([]byte, error) {
		return nil, errors.New("secret-token-from-gh-stderr")
	}
	_, err = service.FetchSourceBuildYAML(context.Background(), SourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest.String(),
	})
	if err == nil || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "confirm GitHub CLI authentication") {
		t.Fatalf("sanitized gh failure = %v", err)
	}
}

func TestImageLookupFetchSourceBuildYAMLFallsBackToPinnedOSSRevisionOnlyForRancherPrime(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	service := newImageLookupTestService(t, registryServer)

	privateRevision := "aa818cc35ef525f237c376c136703263113082fa"
	ossRevision := "f7821ed280af6d120b93917cbded7b6946d77d35"
	referenceText, digest := pushImageLookupSourceFixture(t, registryServer, "v2.11.16-alpha6", map[string]string{
		SourceLabel:      "https://github.com/rancher/rancher-prime.git",
		RevisionLabel:    privateRevision,
		OSSRevisionLabel: ossRevision,
	})

	var commandArguments [][]string
	service.runCommand = func(_ context.Context, executable string, arguments, _ []string, outputLimit int64) ([]byte, error) {
		if executable != "gh" || outputLimit != imageLookupMaxSourceBuildYAML {
			t.Fatalf("command = %q limit = %d", executable, outputLimit)
		}
		commandArguments = append(commandArguments, append([]string(nil), arguments...))
		if len(commandArguments) == 1 {
			return nil, errors.New("private repository returned 404 with sensitive details")
		}
		return []byte("fleetVersion: 110.0.0+up0.16.0-rc.5\nwebhookVersion: 110.0.0+up0.11.0\n"), nil
	}

	response, err := service.FetchSourceBuildYAML(context.Background(), SourceBuildYAMLRequest{
		Reference:      referenceText,
		Platform:       "linux/amd64",
		ExpectedDigest: digest,
	})
	if err != nil {
		t.Fatalf("FetchSourceBuildYAML with OSS fallback: %v", err)
	}
	if len(commandArguments) != 2 {
		t.Fatalf("gh command calls = %d, want private source then OSS source", len(commandArguments))
	}
	wantPrivateEndpoint := "/repos/rancher/rancher-prime/contents/build.yaml?ref=" + privateRevision
	wantOSSEndpoint := "/repos/rancher/rancher/contents/build.yaml?ref=" + ossRevision
	if commandArguments[0][len(commandArguments[0])-1] != wantPrivateEndpoint || commandArguments[1][len(commandArguments[1])-1] != wantOSSEndpoint {
		t.Fatalf("gh endpoints = %q then %q, want %q then %q", commandArguments[0][len(commandArguments[0])-1], commandArguments[1][len(commandArguments[1])-1], wantPrivateEndpoint, wantOSSEndpoint)
	}
	if !response.Found || response.Origin != "declared-oss-source" || response.Data["webhookVersion"] != "110.0.0+up0.11.0" {
		t.Fatalf("OSS fallback response = %#v", response)
	}
	if response.Provenance.RepositoryURL != "https://github.com/rancher/rancher" || response.Provenance.Revision != ossRevision || response.Provenance.RevisionLabel != OSSRevisionLabel || response.Provenance.SourceLabel != SourceLabel {
		t.Fatalf("OSS fallback provenance = %#v", response.Provenance)
	}

	arbitraryReference, arbitraryDigest := pushImageLookupSourceFixture(t, registryServer, "v2.11.16-alpha7", map[string]string{
		SourceLabel:      "https://github.com/rancher/embargoed-security",
		RevisionLabel:    privateRevision,
		OSSRevisionLabel: ossRevision,
	})
	var arbitraryCalls int
	service.runCommand = func(context.Context, string, []string, []string, int64) ([]byte, error) {
		arbitraryCalls++
		return nil, errors.New("secret arbitrary source failure")
	}
	_, err = service.FetchSourceBuildYAML(context.Background(), SourceBuildYAMLRequest{
		Reference:      arbitraryReference,
		Platform:       "linux/amd64",
		ExpectedDigest: arbitraryDigest,
	})
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "confirm GitHub CLI authentication") {
		t.Fatalf("arbitrary source failure = %v", err)
	}
	if arbitraryCalls != 1 {
		t.Fatalf("arbitrary source gh calls = %d, want no OSS fallback", arbitraryCalls)
	}
}

func TestImageLookupParseGitHubSourceRequiresExactPinnedGitHubLabels(t *testing.T) {
	revision := "9c6326c89e3f89c092ff3a80c02bbde96195bccb"
	for _, source := range []string{
		"https://github.com/rancher/embargoed-security",
		"https://github.com/rancher/rancher-prime.git",
	} {
		owner, repository, err := ParseGitHubSource(source, revision)
		wantRepository := "embargoed-security"
		if strings.HasSuffix(source, "/rancher-prime.git") {
			wantRepository = "rancher-prime"
		}
		if err != nil || owner != "rancher" || repository != wantRepository {
			t.Errorf("valid GitHub source %q = owner %q repository %q error %v", source, owner, repository, err)
		}
	}
	for _, testCase := range []struct {
		source   string
		revision string
	}{
		{"", revision},
		{"https://github.com/rancher/embargoed-security", ""},
		{"https://github.com/rancher/embargoed-security", "9c6326c"},
		{" https://github.com/rancher/embargoed-security", revision},
		{"https://github.com/rancher/embargoed-security", revision + " "},
		{"http://github.com/rancher/embargoed-security", revision},
		{"https://user@github.com/rancher/embargoed-security", revision},
		{"https://github.com/rancher/embargoed-security/", revision},
		{"https://github.com/rancher/embargoed-security?ref=main", revision},
		{"https://github.com/rancher/embargoed-security#build", revision},
		{"https://github.com/rancher/embargoed-security/extra", revision},
		{"https://github.com/rancher/rancher-prime.git/", revision},
		{"https://github.com/rancher/rancher-prime.git?ref=main", revision},
		{"https://github.com/rancher/rancher-prime.git#build", revision},
		{"https://github.com/rancher/.git", revision},
		{"https://github.com/rancher/rancher-prime.git.git", revision},
		{"https://github.com/rancher/rancher-prime.GIT", revision},
		{"https://gitlab.com/rancher/embargoed-security", revision},
	} {
		_, _, err := ParseGitHubSource(testCase.source, testCase.revision)
		var metadataErr *imageLookupSourceMetadataError
		if !errors.As(err, &metadataErr) || HTTPStatus(err) != http.StatusUnprocessableEntity {
			t.Errorf("source %q revision %q error = %T %v, want source metadata error", testCase.source, testCase.revision, err, err)
		}
	}
}
