package test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const testPackageArchiveDirectory = "packages"
const testPackageArchiveLimit = 64 << 20

var testPackageArchiveSHAPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type testPackageArchiveFile struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	SHA   string `json:"sha"`
	Bytes int    `json:"bytes"`
}

type testPackageGitHubContent struct {
	Type         string `json:"type"`
	Path         string `json:"path"`
	SHA          string `json:"sha"`
	Size         int    `json:"size"`
	Encoding     string `json:"encoding"`
	Content      string `json:"content"`
	SubmoduleURL string `json:"submodule_git_url"`
	Target       string `json:"target"`
}

func testPackageArchivePath(id string) (string, error) {
	if !cachelab.IDPattern.MatchString(id) {
		return "", errors.New("choose a valid archived package")
	}
	return testPackageArchiveDirectory + "/" + id + ".runway-test-package.json", nil
}

// Callers hold githubMu for the full explicit review/save/restore action, so a
// reconnect cannot rotate credentials or switch repositories mid-operation.
func testPackageArchiveRepository(ctx context.Context, github *testLabService, repository string) (testLabRepo, error) {
	github.mu.Lock()
	connection := github.library.GitHub
	github.mu.Unlock()
	if repository == "" {
		repository = connection.Repository
	}
	if !connection.Connected || repository == "" {
		return testLabRepo{}, errors.New("connect GitHub in Test Lab and choose a private repository before using the archive")
	}
	repo, err := github.githubRepository(ctx, repository)
	if err != nil {
		return repo, err
	}
	if !strings.EqualFold(repo.FullName, repository) || repo.DefaultBranch == "" || len(repo.DefaultBranch) > 255 || strings.ContainsAny(repo.DefaultBranch, "\x00\r\n") {
		return repo, errors.New("the connected repository changed; verify its connection before archiving")
	}
	return repo, nil
}

func testPackageListArchive(ctx context.Context, github *testLabService, repo testLabRepo) ([]testPackageArchiveFile, error) {
	var entries []testPackageGitHubContent
	endpoint := "/repos/" + repo.FullName + "/contents/" + testPackageArchiveDirectory + "?ref=" + url.QueryEscape(repo.DefaultBranch)
	if err := testPackageArchiveAPI(ctx, github, http.MethodGet, endpoint, nil, &entries); err != nil {
		var response *testLabGitHubError
		if errors.As(err, &response) && response.Code == 404 {
			return []testPackageArchiveFile{}, nil
		}
		return nil, err
	}
	if len(entries) >= 1000 {
		return nil, errors.New("archive listing reached GitHub's 1,000-file limit; restore a known package ID or use a smaller dedicated archive")
	}
	result := []testPackageArchiveFile{}
	for _, entry := range entries {
		filename := strings.TrimPrefix(entry.Path, testPackageArchiveDirectory+"/")
		id := strings.TrimSuffix(filename, ".runway-test-package.json")
		path, err := testPackageArchivePath(id)
		if err != nil || entry.Path != path || entry.Type != "file" || entry.Target != "" || entry.SubmoduleURL != "" || !testPackageArchiveSHAPattern.MatchString(entry.SHA) || entry.Size < 0 || entry.Size > testPackageArchiveLimit {
			continue
		}
		result = append(result, testPackageArchiveFile{ID: id, Path: path, SHA: entry.SHA, Bytes: entry.Size})
	}
	return result, nil
}

func testPackageReadArchive(ctx context.Context, github *testLabService, repo testLabRepo, id string) ([]byte, testPackageArchiveFile, error) {
	path, err := testPackageArchivePath(id)
	if err != nil {
		return nil, testPackageArchiveFile{}, err
	}
	var content testPackageGitHubContent
	endpoint := "/repos/" + repo.FullName + "/contents/" + path + "?ref=" + url.QueryEscape(repo.DefaultBranch)
	if err = testPackageArchiveAPI(ctx, github, http.MethodGet, endpoint, nil, &content); err != nil {
		return nil, testPackageArchiveFile{}, err
	}
	if content.Type != "file" || content.Path != path || content.Target != "" || content.SubmoduleURL != "" || !testPackageArchiveSHAPattern.MatchString(content.SHA) || content.Size < 0 || content.Size > testPackageArchiveLimit {
		return nil, testPackageArchiveFile{}, errors.New("GitHub archive is not a supported package file (maximum 64 MiB)")
	}
	record := testPackageArchiveFile{ID: id, Path: path, SHA: content.SHA, Bytes: content.Size}
	if content.Encoding != "base64" {
		// GitHub's contents API omits file content above 1 MiB. Fetch the exact
		// immutable blob from the same verified repository, never a download URL.
		if content.Encoding != "none" {
			return nil, record, errors.New("GitHub archive uses an unsupported encoding")
		}
		var blob testPackageGitHubContent
		if err = testPackageArchiveAPI(ctx, github, http.MethodGet, "/repos/"+repo.FullName+"/git/blobs/"+content.SHA, nil, &blob); err != nil {
			return nil, record, err
		}
		if blob.SHA != content.SHA || blob.Size != content.Size || blob.Encoding != "base64" {
			return nil, record, errors.New("GitHub archive blob did not match the reviewed file")
		}
		content.Content = blob.Content
	}
	if len(content.Content) > base64.StdEncoding.EncodedLen(testPackageArchiveLimit)+testPackageArchiveLimit/32 {
		return nil, record, errors.New("GitHub archive exceeds the size limit")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.ReplaceAll(content.Content, "\n", ""), "\r", ""))
	if err != nil || len(raw) != content.Size || testPackageGitBlobSHA(raw) != content.SHA {
		return nil, record, errors.New("GitHub archive content did not match its recorded identity")
	}
	return raw, record, nil
}

func testPackageGitBlobSHA(raw []byte) string {
	digest := sha1.New() // GitHub's SHA-1 blob identity; transport authorization uses TLS.
	fmt.Fprintf(digest, "blob %d\x00", len(raw))
	digest.Write(raw)
	return hex.EncodeToString(digest.Sum(nil))
}

func testPackageWriteArchive(ctx context.Context, github *testLabService, repo testLabRepo, id, expectedSHA string, raw []byte) (testPackageArchiveFile, error) {
	path, err := testPackageArchivePath(id)
	if err != nil {
		return testPackageArchiveFile{}, err
	}
	if len(raw) == 0 || len(raw) > testPackageArchiveLimit {
		return testPackageArchiveFile{}, errors.New("GitHub backups support packages up to 64 MiB; use a local export for a larger package")
	}
	if expectedSHA != "" && !testPackageArchiveSHAPattern.MatchString(expectedSHA) {
		return testPackageArchiveFile{}, errors.New("review the remote package before saving")
	}
	currentRaw, current, err := testPackageReadArchive(ctx, github, repo, id)
	if err != nil {
		var response *testLabGitHubError
		if !errors.As(err, &response) || response.Code != 404 {
			return testPackageArchiveFile{}, err
		}
		current = testPackageArchiveFile{}
	}
	if current.SHA != "" {
		if _, err := testPackageDecodeArchive(currentRaw, id); err != nil {
			return testPackageArchiveFile{}, err
		}
	}
	if current.SHA != expectedSHA {
		return testPackageArchiveFile{}, errors.New("the remote package changed; review the backup again before replacing it")
	}
	body := map[string]any{"message": "Back up Runway test package " + id, "content": base64.StdEncoding.EncodeToString(raw), "branch": repo.DefaultBranch}
	if expectedSHA != "" {
		body["sha"] = expectedSHA
	}
	var result struct {
		Content testPackageGitHubContent `json:"content"`
	}
	if err = testPackageArchiveAPI(ctx, github, http.MethodPut, "/repos/"+repo.FullName+"/contents/"+path, body, &result); err != nil {
		return testPackageArchiveFile{}, err
	}
	if result.Content.Path != path || result.Content.SHA != testPackageGitBlobSHA(raw) {
		return testPackageArchiveFile{}, errors.New("GitHub accepted the backup but returned an unexpected identity; refresh the archive before retrying")
	}
	return testPackageArchiveFile{ID: id, Path: path, SHA: result.Content.SHA, Bytes: len(raw)}, nil
}

// Large explicitly selected evidence stays on GitHub's fixed API host. The
// regular Test Lab helper has an 8 MiB response cap, so archives use the same
// credential helper with a separate bounded envelope and no redirect handling.
func testPackageArchiveAPI(ctx context.Context, github *testLabService, method, endpoint string, body, out any) error {
	if method != http.MethodGet && method != http.MethodPut && method != http.MethodPost {
		return errors.New("unsupported archive API method")
	}
	if !strings.HasPrefix(endpoint, "/repos/") && !strings.HasPrefix(endpoint, "/user/repos") {
		return errors.New("invalid archive API path")
	}
	if strings.Contains(endpoint, "://") || strings.ContainsAny(endpoint, "\x00\r\n") {
		return errors.New("invalid archive API path")
	}
	token, err := github.githubToken(ctx)
	if err != nil {
		return err
	}
	var raw []byte
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
		if len(raw) > 96<<20 {
			return errors.New("archive request exceeds 96 MiB")
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	if strings.Contains(endpoint, "/contents/"+testPackageArchiveDirectory+"/") {
		request.Header.Set("Accept", "application/vnd.github.object+json")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if method == http.MethodGet && strings.Contains(endpoint, "/contents/"+testPackageArchiveDirectory+"?") {
		request.Header.Set("Accept", "application/vnd.github+json")
	}
	// Never follow a credential-bearing request to an unverified destination.
	client := *github.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return errors.New("GitHub archive request failed; check the connection")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &testLabGitHubError{Code: response.StatusCode}
	}
	if out != nil && response.StatusCode != 204 {
		raw, err := io.ReadAll(io.LimitReader(response.Body, 96<<20+1))
		if err != nil || len(raw) > 96<<20 || json.Unmarshal(raw, out) != nil {
			return errors.New("GitHub returned an unreadable or oversized archive response")
		}
	}
	return nil
}

func testPackageArchiveReviewFingerprint(bundle testPackageBundle, repo testLabRepo, sha string) string {
	raw, _ := json.Marshal(struct {
		Bundle                  string
		Repository, Branch, SHA string
	}{testPackageBundleFingerprint(bundle), strings.ToLower(repo.FullName), repo.DefaultBranch, sha})
	return testPackageDigest(raw)
}

func (p *localControlPanel) testPackageArchiveAction(ctx context.Context, s *testPackageService, req testPackageRequest) (any, error) {
	github, err := p.testLabService()
	if err != nil {
		return nil, err
	}
	github.githubMu.Lock()
	defer github.githubMu.Unlock()
	if req.Action == "archive-create" {
		if req.Confirm != typedConfirmationPhrase {
			return nil, errors.New("type confirm to create a dedicated private archive repository")
		}
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`).MatchString(req.Repository) {
			return nil, errors.New("enter a new repository name using letters, numbers, dots, dashes, or underscores")
		}
		github.mu.Lock()
		connected := github.library.GitHub.Connected
		github.mu.Unlock()
		if !connected {
			return nil, errors.New("connect GitHub in Test Lab before creating an archive repository")
		}
		var created testLabRepo
		if err := testPackageArchiveAPI(ctx, github, http.MethodPost, "/user/repos", map[string]any{"name": req.Repository, "private": true, "auto_init": true, "description": "Private Runway Test Package archive"}, &created); err != nil {
			return nil, err
		}
		if !created.Private || created.Archived || !testLabRepoPattern.MatchString(created.FullName) {
			return nil, errors.New("GitHub created a repository with unexpected settings; review it in GitHub before continuing")
		}
		return map[string]any{"repository": created.FullName, "url": "https://github.com/" + created.FullName, "message": "Private repository created. Grant the Runway GitHub App access if requested, then inspect a backup."}, nil
	}
	repo, err := testPackageArchiveRepository(ctx, github, req.Repository)
	if err != nil {
		return nil, err
	}
	switch req.Action {
	case "archive-list":
		files, err := testPackageListArchive(ctx, github, repo)
		if err != nil {
			return nil, err
		}
		items := []map[string]any{}
		for _, file := range files {
			items = append(items, map[string]any{"id": file.ID, "name": "Package " + file.ID[:8], "path": file.Path, "sha": file.SHA, "bytes": file.Bytes})
		}
		return map[string]any{"repository": repo.FullName, "items": items}, nil
	case "archive-load":
		raw, remote, err := testPackageReadArchive(ctx, github, repo, req.SourceID)
		if err != nil {
			return nil, err
		}
		if req.RemoteSHA != "" && req.RemoteSHA != remote.SHA {
			return nil, errors.New("the archived package changed; refresh the archive list and review it again")
		}
		bundle, err := testPackageDecodeArchive(raw, req.SourceID)
		if err != nil {
			return nil, err
		}
		preview, err := p.previewTestPackageImport(&bundle)
		if err != nil {
			return nil, err
		}
		return map[string]any{"bundle": bundle, "preview": preview, "sha": remote.SHA, "repository": repo.FullName}, nil
	case "archive-inspect", "archive-backup":
		bundle, err := s.buildBundle(req.ID, req.IncludeNotes, req.ArtifactNames)
		if err != nil {
			return nil, err
		}
		id := bundle.Package.OriginID
		if id == "" {
			id = bundle.Package.ID
		}
		raw, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil || len(raw) > testPackageArchiveLimit {
			return nil, errors.New("GitHub backup exceeds 64 MiB; select fewer evidence files")
		}
		if req.Action == "archive-inspect" {
			remoteRaw, remote, err := testPackageReadArchive(ctx, github, repo, id)
			if err != nil {
				var response *testLabGitHubError
				if !errors.As(err, &response) || response.Code != 404 {
					return nil, err
				}
				remote = testPackageArchiveFile{}
			}
			if remote.SHA != "" {
				if _, err := testPackageDecodeArchive(remoteRaw, id); err != nil {
					return nil, err
				}
			}
			fingerprint := testPackageArchiveReviewFingerprint(bundle, repo, remote.SHA)
			return map[string]any{"repository": repo.FullName, "remoteSha": remote.SHA, "reviewToken": p.signTestPackageReview("backup", fingerprint), "exists": remote.SHA != "", "bytes": len(raw), "id": id}, nil
		}
		if err = p.checkTestPackageReview(req.ReviewToken, "backup", testPackageArchiveReviewFingerprint(bundle, repo, req.RemoteSHA)); err != nil {
			return nil, err
		}
		// Recheck the local revision immediately before publishing; a review never
		// replaces newer local work even when only omitted private notes changed.
		latest, err := s.get(req.ID)
		if err != nil {
			return nil, err
		}
		if latest.Revision != bundle.Package.Revision {
			return nil, errTestPackageConflict
		}
		remote, err := testPackageWriteArchive(ctx, github, repo, id, req.RemoteSHA, raw)
		if err != nil {
			return nil, err
		}
		return map[string]any{"repository": repo.FullName, "path": remote.Path, "url": "https://github.com/" + repo.FullName + "/blob/" + url.PathEscape(repo.DefaultBranch) + "/" + remote.Path, "sha": remote.SHA, "savedAt": time.Now().UTC()}, nil
	}
	return nil, errors.New("unknown package archive action")
}

func testPackageDecodeArchive(raw []byte, id string) (testPackageBundle, error) {
	var bundle testPackageBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return bundle, errors.New("the remote file is not a supported Runway package; it was preserved")
	}
	origin := bundle.Package.OriginID
	if origin == "" {
		origin = bundle.Package.ID
	}
	if origin != id {
		return bundle, errors.New("the remote package identity does not match its filename; it was preserved")
	}
	if _, _, err := validateTestPackageBundle(&bundle); err != nil {
		return bundle, fmt.Errorf("the remote package is invalid and was preserved: %w", err)
	}
	return bundle, nil
}
