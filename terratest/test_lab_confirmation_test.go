package test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestLabRunConfirmationPreservesReviewAndGuards(t *testing.T) {
	s := testLabFixture(t)
	req := testLabFixtureRequest()
	reviewed, err := s.review(req)
	if err != nil {
		t.Fatal(err)
	}
	review := reviewed.(map[string]any)
	if review["confirmation"] != "confirm" || review["host"] != "example.test" || review["sha"] != req.SHA {
		t.Fatalf("incomplete review: %#v", review)
	}
	// Keep execution impossible while proving only the exact phrase reaches the
	// existing lifecycle guard. No process or integration test starts here.
	s.cancel = func() {}
	for _, phrase := range []string{"", "CONFIRM", "Confirm", " confirm", "confirm ", "RUN example.test"} {
		req.Confirm = phrase
		if _, err = s.startRun(req); err == nil || !strings.Contains(err.Error(), "type confirm") {
			t.Fatalf("accepted phrase %q: %v", phrase, err)
		}
	}
	req.Confirm = "confirm"
	if _, err = s.startRun(req); err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("lost lifecycle guard: %v", err)
	}
	s.cancel = nil
	req.SHA = strings.Repeat("a", 40)
	if _, err = s.startRun(req); err == nil || !strings.Contains(err.Error(), "catalog") {
		t.Fatalf("lost revision guard: %v", err)
	}
	if len(s.library.Runs) != 0 {
		t.Fatal("created a run while testing confirmation")
	}
}

func TestTestLabLocalDeleteConfirmation(t *testing.T) {
	for _, action := range []string{"delete-plan", "delete-run"} {
		t.Run(action, func(t *testing.T) {
			s := testLabFixture(t)
			id := cacheLabID()
			s.library.Plans = []testLabPlan{{ID: id}}
			s.library.Runs = []testLabRun{{ID: id, Status: "failed"}}
			req := testLabRequest{Action: action, ID: id}
			for _, phrase := range []string{"", "CONFIRM", " confirm", "confirm ", "DELETE PLAN", "DELETE RUN"} {
				req.Confirm = phrase
				if _, err := s.mutate(req); err == nil {
					t.Fatalf("accepted %q", phrase)
				}
			}
			req.Confirm = "confirm"
			if _, err := s.mutate(req); err != nil {
				t.Fatal(err)
			}
			if action == "delete-plan" && len(s.library.Plans) != 0 || action == "delete-run" && len(s.library.Runs) != 0 {
				t.Fatal("did not delete selected local item")
			}
		})
	}
}

func TestTestLabConfigDeleteConfirmation(t *testing.T) {
	s := testLabFixture(t)
	s.configRoot = filepath.Join(t.TempDir(), "configs")
	value, err := s.configAction(testLabRequest{Action: "folder-save", Name: "Saved"})
	if err != nil {
		t.Fatal(err)
	}
	folder := value.(testLabConfigLibrary).Folders[0].ID
	value, err = s.configAction(testLabRequest{Action: "config-save", Name: "Example", Config: testLabFixtureConfig})
	if err != nil {
		t.Fatal(err)
	}
	config := value.(testLabConfigFile)
	for _, req := range []testLabRequest{{Action: "folder-delete", ID: folder}, {Action: "config-delete", ID: config.ID, Revision: config.Revision}} {
		for _, phrase := range []string{"", "CONFIRM", "confirm ", "DELETE FOLDER", "DELETE CONFIG"} {
			req.Confirm = phrase
			if _, err = s.configAction(req); err == nil {
				t.Fatalf("%s accepted %q", req.Action, phrase)
			}
		}
		req.Confirm = "confirm"
		if req.Action == "config-delete" {
			stale := req
			stale.Revision = "stale"
			if _, err = s.configAction(stale); err == nil {
				t.Fatal("deleted a stale config revision")
			}
		}
		if _, err = s.configAction(req); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTestLabGitHubTypedConfirmation(t *testing.T) {
	for _, action := range []string{"github-create", "github-link", "github-disconnect"} {
		t.Run(action, func(t *testing.T) {
			s := testLabFixture(t)
			s.library.GitHub.Login = "fixture"
			s.credential.AccessToken = "fake"
			writes := 0
			s.client = &http.Client{Transport: testLabRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					writes++
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"full_name":"fixture/test-runner","private":true}`)), Header: http.Header{}}, nil
			})}
			req := testLabRequest{Action: action, Name: "test-runner", Repository: "fixture/test-runner"}
			for _, phrase := range []string{"", "CONFIRM", "confirm ", "DISCONNECT", "fixture/test-runner"} {
				req.Confirm = phrase
				if _, err := s.githubAction(context.Background(), req); err == nil {
					t.Fatalf("accepted %q", phrase)
				}
			}
			if writes != 0 || s.library.GitHub.Login != "fixture" || s.library.GitHub.Repository != "" {
				t.Fatal("changed connection before confirmation")
			}
			req.Confirm = "confirm"
			if _, err := s.githubAction(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if action == "github-disconnect" {
				if s.library.GitHub.Login != "" || s.credential.AccessToken != "" {
					t.Fatal("did not disconnect")
				}
			} else if s.library.GitHub.Repository != req.Repository {
				t.Fatal("did not connect selected repository")
			}
			if action == "github-create" && writes != 1 || action != "github-create" && writes != 0 {
				t.Fatalf("unexpected API writes: %d", writes)
			}
		})
	}
}
