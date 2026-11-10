package imagelookup

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
)

func TestRancherServerRepositories(t *testing.T) {
	want := []string{"rancher/rancher-server", "rancher/rancher"}
	if !slices.Equal(RancherServerRepositories(), want) {
		t.Fatal("unexpected server preference order")
	}
	repositories := RancherServerRepositories()
	repositories[0] = "changed"
	if !slices.Equal(RancherServerRepositories(), want) {
		t.Fatal("caller mutated shared repository order")
	}
	for _, repo := range append(want, "example/rancher-server", "rancher/rancher-server/extra", "rancher/rancher-agent", "registry.example/rancher/rancher-server") {
		if IsRancherServerRepository(repo) != slices.Contains(want, repo) {
			t.Fatalf("unexpected canonical result for %q", repo)
		}
	}
}

func TestImageLookupProductionServerHeadCanonicalIdentity(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tag := "v2.16.0-" + sha + "-head"
	for _, repository := range RancherServerRepositories() {
		for _, requestedTag := range []string{tag, "v2.16.0-head", "v2.16-head"} {
			t.Run(repository+":"+requestedTag, func(t *testing.T) {
				service := &Service{}
				parsed, err := service.parseReference("stgregistry.suse.com/"+repository+":"+requestedTag, true)
				if err != nil {
					t.Fatal(err)
				}
				labels := map[string]string{SourceLabel: "https://github.com/rancher/rancher-prime.git", OSSRevisionLabel: sha, CanonicalReferenceLabel: repository + ":" + tag}
				got := imageLookupInspectPrimeHead(parsed, labels)
				if !got.IsPrimeHead || !got.Consistent || !got.CanonicalMatchesRequest || got.ImageRole != "server" || got.CompanionReference != "stgregistry.suse.com/rancher/rancher-agent:"+tag && requestedTag == tag {
					t.Fatalf("valid server head: %#v", got)
				}
				if got.Mutable != (requestedTag != tag) {
					t.Fatalf("unexpected mutable status: %#v", got)
				}
				for _, wrongRepository := range []string{"rancher/rancher-agent", "example/rancher-server"} {
					labels[CanonicalReferenceLabel] = wrongRepository + ":" + tag
					got = imageLookupInspectPrimeHead(parsed, labels)
					if got.Consistent || got.CanonicalMatchesRequest {
						t.Fatalf("accepted wrong canonical repository %s: %#v", wrongRepository, got)
					}
				}
				other := "rancher/rancher"
				if repository == other {
					other = "rancher/rancher-server"
				}
				labels[CanonicalReferenceLabel] = other + ":" + tag
				if got := imageLookupInspectPrimeHead(parsed, labels); got.Consistent || got.CanonicalMatchesRequest {
					t.Fatalf("accepted a different server repository: %#v", got)
				}
			})
		}
	}
}

func TestImageLookupPrimeSearchKeepsServerRepositoriesIndependent(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	sha := strings.Repeat("a", 40)
	tag := "v2.16.0-" + sha + "-head"
	created := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-server", tag, sha, created)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher", tag, strings.Repeat("b", 40), created)
	pushImageLookupPrimeFixture(t, registryServer, "rancher/rancher-agent", tag, sha, created.Add(time.Minute))
	service := newImageLookupRewritingTestService(t, registryServer, "")
	for _, query := range []string{tag, "v2.16.0-head"} {
		got, err := service.Search(context.Background(), SearchRequest{Registry: "all", Repository: "all", Query: query})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Groups) != 3 {
			t.Fatalf("groups = %#v", got.Groups)
		}
		for _, group := range got.Groups {
			if len(group.Tags) != 1 {
				t.Fatalf("unexpected tags: %#v", group)
			}
			result := group.Tags[0]
			wantStatus := "verified"
			if group.Repository == "rancher/rancher" {
				wantStatus = "invalid"
			}
			if result.PairStatus != wantStatus {
				t.Fatalf("%s: got %s want %s", group.Repository, result.PairStatus, wantStatus)
			}
			if result.CanonicalReference != "stgregistry.suse.com/"+group.Repository+":"+tag {
				t.Fatalf("wrong provenance attached: %#v", result)
			}
			if group.ImageRole == "agent" && result.CompanionReference != "stgregistry.suse.com/rancher/rancher-server:"+tag {
				t.Fatalf("agent did not prefer production server: %#v", result)
			}
		}
	}
	for _, repo := range RancherServerRepositories() {
		got, err := service.Search(context.Background(), SearchRequest{Registry: "all", Repository: repo, Query: tag})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Groups) != 1 || got.Groups[0].Repository != repo {
			t.Fatalf("explicit target expanded: %#v", got.Groups)
		}
	}
}

func TestImageLookupAgentSearchFallsBackToLegacyServer(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	sha := strings.Repeat("c", 40)
	tag := "v2.15.1-" + sha + "-head"
	created := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	for _, repo := range []string{"rancher/rancher", "rancher/rancher-agent"} {
		pushImageLookupPrimeFixture(t, registryServer, repo, tag, sha, created)
	}
	service := newImageLookupRewritingTestService(t, registryServer, "")
	got, err := service.Search(context.Background(), SearchRequest{Registry: "all", Repository: "rancher/rancher-agent", Query: tag})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Tags) != 1 {
		t.Fatalf("unexpected result: %#v", got.Groups)
	}
	result := got.Groups[0].Tags[0]
	if result.PairStatus != "verified" || result.CompanionReference != "stgregistry.suse.com/rancher/rancher:"+tag {
		t.Fatalf("legacy fallback lost actual companion: %#v", result)
	}
}

func TestImageLookupExactPrimePairRejectsWrongServerCanonicalRepository(t *testing.T) {
	tag := "v2.16.0-abcdef1-head"
	server := Provenance{Reference: "stgregistry.suse.com/rancher/rancher-server:" + tag, CanonicalReference: "rancher/rancher:" + tag}
	agent := Provenance{CanonicalReference: "rancher/rancher-agent:" + tag}
	if err := imageLookupValidateExactPrimeHeadPair(tag, server, agent); err == nil {
		t.Fatal("accepted wrong server repository canonical label")
	}
	server.CanonicalReference = "rancher/rancher-server:" + tag
	if err := imageLookupValidateExactPrimeHeadPair(tag, server, agent); err != nil {
		t.Fatal(err)
	}
	server.CanonicalReference = "rancher/rancher-server:v2.16.0-abcdef2-head"
	if err := imageLookupValidateExactPrimeHeadPair(tag, server, agent); err == nil {
		t.Fatal("accepted wrong tag")
	}
}

func TestImageLookupPrimePairRanksSharedAcrossServerAndAgent(t *testing.T) {
	registryServer := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer registryServer.Close()
	created := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	firstTag := "v2.16.0-" + strings.Repeat("b", 40) + "-head"
	secondTag := "v2.16.0-" + strings.Repeat("a", 40) + "-head"
	for _, sha := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		tag := "v2.16.0-" + sha + "-head"
		for _, repo := range append(RancherServerRepositories(), "rancher/rancher-agent") {
			pushImageLookupPrimeFixture(t, registryServer, repo, tag, sha, created)
		}
	}
	service := newImageLookupRewritingTestService(t, registryServer, "")
	response, err := service.Search(context.Background(), SearchRequest{Registry: "all", Repository: "all", Query: "v2.16.0-head"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Groups) != 3 {
		t.Fatalf("unexpected groups: %#v", response.Groups)
	}
	for _, group := range response.Groups {
		if len(group.Tags) != 2 {
			t.Fatalf("unexpected tags: %#v", group)
		}
		for _, tag := range group.Tags {
			// Equal timestamps sort by descending immutable tag, then the production
			// server before legacy. An agent shares its resolved server pair's rank.
			expectedRank := 1
			if tag.Name == secondTag {
				expectedRank = 3
			} else if tag.Name != firstTag {
				t.Fatalf("unexpected tag %s", tag.Name)
			}
			if group.Repository == "rancher/rancher" {
				expectedRank++
			}
			if tag.PairStatus != "verified" || tag.ResolvedRank != expectedRank {
				t.Fatalf("%s:%s rank=%d, want %d (status %s)", group.Repository, tag.Name, tag.ResolvedRank, expectedRank, tag.PairStatus)
			}
		}
	}
}
