package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type packageArchiveTransport func(*http.Request) (*http.Response, error)

func (f packageArchiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type packageArchiveMock struct {
	private, archived, conflict, redirect bool
	files                                 map[string][]byte
	puts, posts, requests                 int
	shaOverride                           string
}

func testPackageArchiveMockService(t *testing.T) (*testLabService, *packageArchiveMock) {
	t.Helper()
	service, err := newTestLabService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.library.GitHub = testLabGitHub{Connected: true, Repository: "fixture/private-archive"}
	service.credential = testLabCredential{AccessToken: "mock-archive-token"}
	mock := &packageArchiveMock{private: true, files: map[string][]byte{}}
	respond := func(status int, body any) *http.Response {
		raw, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}
	}
	service.client = &http.Client{Transport: packageArchiveTransport(func(req *http.Request) (*http.Response, error) {
		mock.requests++
		if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.Header.Get("Authorization") != "Bearer mock-archive-token" {
			t.Fatalf("request escaped trusted API: %s", req.URL)
		}
		if req.Method == http.MethodDelete {
			t.Fatal("archive attempted remote deletion")
		}
		if req.URL.Path == "/repos/fixture/private-archive" {
			return respond(200, testLabRepo{FullName: "fixture/private-archive", Private: mock.private, Archived: mock.archived, DefaultBranch: "main"}), nil
		}
		if req.URL.Path == "/user/repos" && req.Method == http.MethodPost {
			mock.posts++
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["private"] != true || body["auto_init"] != true {
				t.Fatal("archive repo creation not private and initialized")
			}
			return respond(201, testLabRepo{FullName: "fixture/" + body["name"].(string), Private: true, DefaultBranch: "main"}), nil
		}
		if strings.Contains(req.URL.Path, "/git/blobs/") {
			sha := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
			for _, raw := range mock.files {
				if testPackageGitBlobSHA(raw) == sha {
					return respond(200, testPackageGitHubContent{SHA: sha, Size: len(raw), Encoding: "base64", Content: base64.StdEncoding.EncodeToString(raw)}), nil
				}
			}
			return respond(404, nil), nil
		}
		prefix := "/repos/fixture/private-archive/contents/"
		if !strings.HasPrefix(req.URL.Path, prefix) {
			t.Fatalf("unexpected archive endpoint %s", req.URL)
		}
		path := strings.TrimPrefix(req.URL.Path, prefix)
		if mock.redirect {
			response := respond(302, nil)
			response.Header.Set("Location", "https://not-github.example.test/steal")
			return response, nil
		}
		if path == testPackageArchiveDirectory && req.Method == http.MethodGet {
			rows := []testPackageGitHubContent{}
			for name, raw := range mock.files {
				rows = append(rows, testPackageGitHubContent{Type: "file", Path: name, SHA: testPackageGitBlobSHA(raw), Size: len(raw)})
			}
			rows = append(rows, testPackageGitHubContent{Type: "file", Path: ".github/workflows/ignored.yml", SHA: strings.Repeat("a", 40)})
			return respond(200, rows), nil
		}
		if !strings.HasPrefix(path, testPackageArchiveDirectory+"/") {
			t.Fatal("archive used a path outside packages")
		}
		if req.Method == http.MethodGet {
			raw, exists := mock.files[path]
			if !exists {
				return respond(404, nil), nil
			}
			sha := testPackageGitBlobSHA(raw)
			if mock.shaOverride != "" {
				sha = mock.shaOverride
			}
			content := testPackageGitHubContent{Type: "file", Path: path, SHA: sha, Size: len(raw), Encoding: "base64", Content: base64.StdEncoding.EncodeToString(raw)}
			if len(raw) > 1<<20 {
				content.Encoding = "none"
				content.Content = ""
			}
			return respond(200, content), nil
		}
		if req.Method == http.MethodPut {
			mock.puts++
			var body struct{ Content, SHA, Branch string }
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			current, exists := mock.files[path]
			if mock.conflict || (exists && body.SHA != testPackageGitBlobSHA(current)) || (!exists && body.SHA != "") {
				return respond(409, nil), nil
			}
			if body.Branch != "main" {
				t.Fatal("backup branch not pinned")
			}
			raw, err := base64.StdEncoding.DecodeString(body.Content)
			if err != nil {
				t.Fatal(err)
			}
			mock.files[path] = raw
			return respond(201, map[string]any{"content": testPackageGitHubContent{Type: "file", Path: path, SHA: testPackageGitBlobSHA(raw), Size: len(raw)}}), nil
		}
		return nil, fmt.Errorf("unexpected method %s", req.Method)
	})}
	return service, mock
}

func TestTestPackageArchiveBackupRestoreAndPrivacy(t *testing.T) {
	p, s, pkg := testPackageTransferFixture(t)
	github, mock := testPackageArchiveMockService(t)
	p.testLab = github
	call := func(req testPackageRequest) any {
		t.Helper()
		result, err := p.handleTestPackageTransfer(context.Background(), s, req)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	req := testPackageRequest{Action: "archive-inspect", ID: pkg.ID, Repository: "fixture/private-archive"}
	inspection := call(req).(map[string]any)
	if inspection["exists"] != false || mock.puts != 0 {
		t.Fatal("inspect mutated remote state")
	}
	req.Action = "archive-backup"
	req.ReviewToken = inspection["reviewToken"].(string)
	result := call(req).(map[string]any)
	if mock.puts != 1 || result["sha"] == "" {
		t.Fatal("backup did not produce verified remote identity")
	}
	path, _ := testPackageArchivePath(pkg.ID)
	if bytes.Contains(mock.files[path], []byte("private-note-marker")) || bytes.Contains(mock.files[path], []byte("Saved local test result")) {
		t.Fatal("default archive included private notes or artifact bytes")
	}
	if _, err := p.handleTestPackageTransfer(context.Background(), s, req); err == nil {
		t.Fatal("stale remote review overwrote a newer backup")
	}
	list := call(testPackageRequest{Action: "archive-list", Repository: req.Repository}).(map[string]any)
	if len(list["items"].([]map[string]any)) != 1 {
		t.Fatal("archive listed files outside package directory")
	}
	loaded := call(testPackageRequest{Action: "archive-load", Repository: req.Repository, SourceID: pkg.ID, RemoteSHA: result["sha"].(string)}).(map[string]any)
	bundle := loaded["bundle"].(testPackageBundle)
	preview := loaded["preview"].(testPackageImportPreview)
	before, _ := s.list()
	if len(before) != 1 {
		t.Fatal("archive-load wrote local package without confirmation")
	}
	imported := call(testPackageRequest{Action: "import", Bundle: &bundle, ReviewToken: preview.ReviewToken, Confirm: typedConfirmationPhrase}).(map[string]any)["package"].(testPackage)
	if imported.OriginID != pkg.ID || imported.ID == pkg.ID {
		t.Fatal("remote restore overwrote original identity")
	}
	// A private repository can change visibility between review and backup.
	req.Action = "archive-inspect"
	inspection = call(req).(map[string]any)
	req.Action = "archive-backup"
	req.ReviewToken = inspection["reviewToken"].(string)
	req.RemoteSHA = inspection["remoteSha"].(string)
	mock.private = false
	if _, err := p.handleTestPackageTransfer(context.Background(), s, req); err == nil {
		t.Fatal("backup to public repository accepted")
	}
	if mock.puts != 1 {
		t.Fatal("privacy failure still wrote remote data")
	}
}

func TestTestPackageArchiveRejectsStaleLocalAndRemoteConflicts(t *testing.T) {
	p, s, pkg := testPackageTransferFixture(t)
	github, mock := testPackageArchiveMockService(t)
	p.testLab = github
	req := testPackageRequest{Action: "archive-inspect", ID: pkg.ID, Repository: "fixture/private-archive"}
	value, err := p.handleTestPackageTransfer(context.Background(), s, req)
	if err != nil {
		t.Fatal(err)
	}
	inspection := value.(map[string]any)
	req.Action = "archive-backup"
	req.ReviewToken = inspection["reviewToken"].(string)
	pkg.Revision = cacheLabID()
	pkg.Title = "Changed after review"
	if err = s.saveLocked(pkg); err != nil {
		t.Fatal(err)
	}
	if _, err = p.handleTestPackageTransfer(context.Background(), s, req); err == nil || mock.puts != 0 {
		t.Fatal("stale local revision was uploaded")
	}
	req.Action = "archive-inspect"
	value, err = p.handleTestPackageTransfer(context.Background(), s, req)
	if err != nil {
		t.Fatal(err)
	}
	inspection = value.(map[string]any)
	req.Action = "archive-backup"
	req.ReviewToken = inspection["reviewToken"].(string)
	mock.conflict = true
	if _, err = p.handleTestPackageTransfer(context.Background(), s, req); err == nil {
		t.Fatal("remote compare-and-swap failure hidden")
	}
	restored, err := s.get(pkg.ID)
	if err != nil || restored.Title != pkg.Title || restored.Revision != pkg.Revision {
		t.Fatal("remote failure changed local package")
	}
	mock.conflict = false
	req.ArtifactNames = []string{pkg.Sessions[0].Evidence[0].Artifact.Name}
	if _, err = p.handleTestPackageTransfer(context.Background(), s, req); err == nil {
		t.Fatal("artifact inclusion changed after review")
	}
}

func TestTestPackageArchiveFixedPathsLargeBlobAndRedirectGuard(t *testing.T) {
	github, mock := testPackageArchiveMockService(t)
	repo := testLabRepo{FullName: "fixture/private-archive", Private: true, DefaultBranch: "main"}
	id := cacheLabID()
	path, _ := testPackageArchivePath(id)
	mock.files[path] = []byte(strings.Repeat("a", (1<<20)+100))
	raw, record, err := testPackageReadArchive(context.Background(), github, repo, id)
	if err != nil || len(raw) != (1<<20)+100 || record.SHA != testPackageGitBlobSHA(raw) {
		t.Fatalf("large immutable blob: %v", err)
	}
	for _, invalid := range []string{"../secrets", ".github/workflows/run", "", strings.Repeat("a", 25)} {
		count := mock.requests
		if _, _, err = testPackageReadArchive(context.Background(), github, repo, invalid); err == nil || mock.requests != count {
			t.Fatal("invalid path reached GitHub")
		}
	}
	mock.files[path] = []byte("small blob")
	mock.shaOverride = strings.Repeat("0", 40)
	if _, _, err = testPackageReadArchive(context.Background(), github, repo, id); err == nil {
		t.Fatal("corrupt remote content identity accepted")
	}
	mock.shaOverride = ""
	mock.redirect = true
	if _, _, err = testPackageReadArchive(context.Background(), github, repo, id); err == nil {
		t.Fatal("credential-bearing redirect accepted")
	}
}

func TestTestPackageArchiveCreateIsExplicitAndPrivate(t *testing.T) {
	p, s, _ := testPackageTransferFixture(t)
	github, mock := testPackageArchiveMockService(t)
	p.testLab = github
	req := testPackageRequest{Action: "archive-create", Repository: "package-backup"}
	if _, err := p.handleTestPackageTransfer(context.Background(), s, req); err == nil || mock.posts != 0 {
		t.Fatal("repository created without confirmation")
	}
	req.Confirm = typedConfirmationPhrase
	if _, err := p.handleTestPackageTransfer(context.Background(), s, req); err != nil || mock.posts != 1 {
		t.Fatal("private repo creation failed", err)
	}
	req.Repository = "other/existing"
	if _, err := p.handleTestPackageTransfer(context.Background(), s, req); err == nil || mock.posts != 1 {
		t.Fatal("creation accepted existing repository path")
	}
}

func TestTestPackageArchivePreservesUnrecognizedRemoteFile(t *testing.T) {
	p, s, pkg := testPackageTransferFixture(t)
	github, mock := testPackageArchiveMockService(t)
	p.testLab = github
	path, _ := testPackageArchivePath(pkg.ID)
	existing := []byte(`{"important":"not a Runway package"}`)
	mock.files[path] = existing
	req := testPackageRequest{Action: "archive-inspect", ID: pkg.ID, Repository: "fixture/private-archive"}
	if _, err := p.handleTestPackageTransfer(context.Background(), s, req); err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatal("unrecognized remote file was accepted for replacement", err)
	}
	if mock.puts != 0 || !bytes.Equal(mock.files[path], existing) {
		t.Fatal("remote file changed")
	}
	bundle, err := s.buildBundle(pkg.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Package.ID = cacheLabID()
	raw, _ := json.Marshal(bundle)
	mock.files[path] = raw
	if _, err := p.handleTestPackageTransfer(context.Background(), s, req); err == nil {
		t.Fatal("different package at same path accepted")
	}
}
