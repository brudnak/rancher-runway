package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type testLabGitHub struct {
	ClientID          string `json:"clientId"`
	Slug              string `json:"slug"`
	Login             string `json:"login"`
	Connected         bool   `json:"connected"`
	Repository        string `json:"repository"`
	CreatedRepository bool   `json:"createdRepository"`
	RepositoryStatus  string `json:"repositoryStatus"`
}
type testLabCredential struct {
	ClientID     string    `json:"clientId"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Error        string    `json:"error"`
}
type testLabDevice struct {
	DeviceCode      string    `json:"device_code"`
	UserCode        string    `json:"user_code"`
	VerificationURI string    `json:"verification_uri"`
	ExpiresIn       int       `json:"expires_in"`
	Interval        int       `json:"interval"`
	ExpiresAt       time.Time `json:"-"`
	NextPoll        time.Time `json:"-"`
}

var testLabRepoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)
var testLabClientPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{5,100}$`)

func (s *testLabService) githubOAuth(ctx context.Context, endpoint string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/"+endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub sign-in is unavailable; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub sign-in returned HTTP %d", resp.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("GitHub sign-in returned an unreadable response")
	}
	return nil
}
func (s *testLabService) saveGitHubCredential(cred testLabCredential) error {
	if cred.AccessToken == "" {
		return fmt.Errorf("GitHub did not return a user token")
	}
	if cred.ExpiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(cred.ExpiresIn) * time.Second)
	}
	// Retain a rotated token in memory even if Keychain is temporarily locked;
	// the previous refresh token has already been invalidated by GitHub.
	s.mu.Lock()
	cred.ClientID = s.library.GitHub.ClientID
	s.mu.Unlock()
	s.credential = cred
	data, _ := json.Marshal(cred)
	_, err := s.keychain("write", "github", string(data))
	return err
}
func (s *testLabService) githubToken(ctx context.Context) (string, error) {
	if s.credential.AccessToken == "" {
		data, err := s.keychain("read", "github", "")
		if err != nil {
			return "", err
		}
		if json.Unmarshal([]byte(data), &s.credential) != nil {
			return "", fmt.Errorf("reconnect GitHub")
		}
	}
	s.mu.Lock()
	clientID := s.library.GitHub.ClientID
	s.mu.Unlock()
	if s.credential.ClientID != "" && s.credential.ClientID != clientID {
		return "", fmt.Errorf("saved authorization belongs to another GitHub App; reconnect")
	}
	if !s.credential.ExpiresAt.IsZero() && time.Until(s.credential.ExpiresAt) < time.Minute {
		if s.credential.RefreshToken == "" {
			return "", fmt.Errorf("GitHub authorization expired; reconnect")
		}
		s.mu.Lock()
		clientID := s.library.GitHub.ClientID
		s.mu.Unlock()
		var next testLabCredential
		if err := s.githubOAuth(ctx, "oauth/access_token", url.Values{"client_id": {clientID}, "grant_type": {"refresh_token"}, "refresh_token": {s.credential.RefreshToken}}, &next); err != nil {
			return "", err
		}
		if next.Error != "" {
			return "", fmt.Errorf("GitHub authorization expired or was revoked; reconnect")
		}
		if err := s.saveGitHubCredential(next); err != nil {
			return "", err
		}
	}
	return s.credential.AccessToken, nil
}
func (s *testLabService) githubAPI(ctx context.Context, method, endpoint string, body any, out any) error {
	token, err := s.githubToken(ctx)
	if err != nil {
		return err
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	// Deliberately no general URL or delete-repository operation. Only fixed
	// api.github.com endpoints constructed from validated names reach this helper.
	if !strings.HasPrefix(endpoint, "/") || strings.Contains(endpoint, "://") {
		return fmt.Errorf("invalid GitHub API path")
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub request failed; check the connection")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &testLabGitHubError{Code: resp.StatusCode}
	}
	if out != nil && resp.StatusCode != 204 {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
			return fmt.Errorf("GitHub returned an unreadable response")
		}
	}
	return nil
}

type testLabGitHubError struct{ Code int }

func (e *testLabGitHubError) Error() string {
	switch e.Code {
	case 401:
		return "GitHub authorization is unavailable; reconnect"
	case 403:
		return "GitHub denied this action. Check App installation permissions, organization policy, and API rate limits."
	case 404:
		return "Repository or resource unavailable. It may be inaccessible, renamed, or removed."
	case 409, 422:
		return "GitHub could not apply this change. Check the repository name, branch rules, and installation access."
	}
	return fmt.Sprintf("GitHub returned HTTP %d", e.Code)
}
func (s *testLabService) githubAction(ctx context.Context, req testLabRequest) (any, error) {
	s.githubMu.Lock()
	defer s.githubMu.Unlock()
	s.mu.Lock()
	gh := s.library.GitHub
	s.mu.Unlock()
	switch req.Action {
	case "github-config":
		if !testLabClientPattern.MatchString(req.ClientID) || !regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`).MatchString(req.Slug) {
			return nil, fmt.Errorf("enter the registered GitHub App client ID and app slug")
		}
		if gh.Login != "" {
			return nil, fmt.Errorf("disconnect before changing the integration")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.library.GitHub.ClientID = req.ClientID
		s.library.GitHub.Slug = req.Slug
		return map[string]bool{"ok": true}, s.persistLocked()
	case "github-start":
		if !testLabClientPattern.MatchString(gh.ClientID) {
			return nil, fmt.Errorf("Runway's GitHub App must be registered first; see the integration setup guide")
		}
		var device testLabDevice
		if err := s.githubOAuth(ctx, "device/code", url.Values{"client_id": {gh.ClientID}}, &device); err != nil {
			return nil, err
		}
		if device.DeviceCode == "" || device.VerificationURI != "https://github.com/login/device" || device.ExpiresIn <= 0 {
			return nil, fmt.Errorf("enable device flow in the registered GitHub App")
		}
		if device.Interval < 5 {
			device.Interval = 5
		}
		device.ExpiresAt = time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
		device.NextPoll = time.Now().Add(time.Duration(device.Interval) * time.Second)
		s.device = device
		return map[string]any{"userCode": device.UserCode, "url": device.VerificationURI, "expiresAt": device.ExpiresAt, "interval": device.Interval}, nil
	case "github-poll":
		if s.device.DeviceCode == "" || time.Now().After(s.device.ExpiresAt) {
			s.device = testLabDevice{}
			return nil, fmt.Errorf("sign-in expired; start again")
		}
		if time.Now().Before(s.device.NextPoll) {
			return map[string]any{"pending": true, "interval": s.device.Interval}, nil
		}
		s.device.NextPoll = time.Now().Add(time.Duration(s.device.Interval) * time.Second)
		var credential testLabCredential
		if err := s.githubOAuth(ctx, "oauth/access_token", url.Values{"client_id": {gh.ClientID}, "device_code": {s.device.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &credential); err != nil {
			return nil, err
		}
		if credential.Error == "slow_down" {
			s.device.Interval += 5
			s.device.NextPoll = time.Now().Add(time.Duration(s.device.Interval) * time.Second)
		}
		if credential.Error == "authorization_pending" || credential.Error == "slow_down" {
			return map[string]any{"pending": true, "interval": s.device.Interval}, nil
		}
		if credential.Error != "" {
			s.device = testLabDevice{}
			return nil, fmt.Errorf("GitHub sign-in was declined or expired; start again")
		}
		if err := s.saveGitHubCredential(credential); err != nil {
			return nil, err
		}
		s.device = testLabDevice{}
		var user struct {
			Login string `json:"login"`
		}
		if err := s.githubAPI(ctx, "GET", "/user", nil, &user); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.library.GitHub.Login != "" && s.library.GitHub.Login != user.Login {
			s.library.GitHub.Repository = ""
			s.library.GitHub.CreatedRepository = false
			s.library.GitHub.RepositoryStatus = ""
		}
		s.library.GitHub.Login = user.Login
		s.library.GitHub.Connected = true
		return map[string]bool{"connected": true}, s.persistLocked()
	case "github-status":
		var user struct {
			Login string `json:"login"`
		}
		err := s.githubAPI(ctx, "GET", "/user", nil, &user)
		s.mu.Lock()
		s.library.GitHub.Connected = err == nil
		if err == nil {
			s.library.GitHub.Login = user.Login
		}
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if gh.Repository != "" {
			_, err = s.githubRepository(ctx, gh.Repository)
			s.mu.Lock()
			if err != nil {
				s.library.GitHub.RepositoryStatus = "Repository unavailable"
			} else {
				s.library.GitHub.RepositoryStatus = "Connected"
			}
			s.mu.Unlock()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.library.GitHub, s.persistLocked()
	case "github-disconnect":
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to forget the saved connection")
		}
		if _, err := s.keychain("delete", "github", ""); err != nil {
			return nil, err
		}
		s.credential = testLabCredential{}
		s.device = testLabDevice{}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.library.GitHub = testLabGitHub{ClientID: gh.ClientID, Slug: gh.Slug}
		return map[string]bool{"ok": true}, s.persistLocked()
	case "github-repositories":
		repos := []testLabRepo{}
		for page := 1; page <= 20; page++ {
			var installations struct {
				Installations []struct {
					ID int64 `json:"id"`
				} `json:"installations"`
			}
			if err := s.githubAPI(ctx, "GET", fmt.Sprintf("/user/installations?per_page=100&page=%d", page), nil, &installations); err != nil {
				return nil, err
			}
			for _, installation := range installations.Installations {
				for rp := 1; rp <= 20; rp++ {
					var response struct {
						Repositories []testLabRepo `json:"repositories"`
					}
					if err := s.githubAPI(ctx, "GET", fmt.Sprintf("/user/installations/%d/repositories?per_page=100&page=%d", installation.ID, rp), nil, &response); err != nil {
						return nil, err
					}
					for _, repo := range response.Repositories {
						if repo.Private && !repo.Archived {
							repos = append(repos, repo)
						}
					}
					if len(response.Repositories) < 100 {
						break
					}
				}
			}
			if len(installations.Installations) < 100 {
				break
			}
		}
		return map[string]any{"repositories": repos}, nil
	case "github-link":
		repo, err := s.githubRepository(ctx, req.Repository)
		if err != nil {
			return nil, err
		}
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to connect this repository")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		created := gh.Repository == repo.FullName && gh.CreatedRepository
		s.library.GitHub.Repository = repo.FullName
		s.library.GitHub.CreatedRepository = created
		s.library.GitHub.RepositoryStatus = "Connected"
		return repo, s.persistLocked()
	case "github-unlink":
		s.mu.Lock()
		defer s.mu.Unlock()
		s.library.GitHub.Repository = ""
		s.library.GitHub.CreatedRepository = false
		s.library.GitHub.RepositoryStatus = ""
		return map[string]bool{"ok": true}, s.persistLocked()
	case "github-create":
		if gh.Login == "" {
			return nil, fmt.Errorf("connect GitHub first")
		}
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`).MatchString(req.Name) {
			return nil, fmt.Errorf("enter a repository name using letters, numbers, dots, dashes, or underscores")
		}
		if req.Confirm != typedConfirmationPhrase {
			return nil, fmt.Errorf("type confirm to create this private repository")
		}
		var repo testLabRepo
		if err := s.githubAPI(ctx, "POST", "/user/repos", map[string]any{"name": req.Name, "private": true, "auto_init": true, "description": "Dedicated Rancher Runway test workspace"}, &repo); err != nil {
			return nil, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.library.GitHub.Repository = repo.FullName
		s.library.GitHub.CreatedRepository = true
		s.library.GitHub.RepositoryStatus = "Grant App access, then verify"
		return repo, s.persistLocked()
	}
	return nil, fmt.Errorf("unsupported GitHub action")
}

type testLabRepo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	DefaultBranch string `json:"default_branch"`
}

func (s *testLabService) githubRepository(ctx context.Context, name string) (testLabRepo, error) {
	var repo testLabRepo
	if !testLabRepoPattern.MatchString(name) {
		return repo, fmt.Errorf("enter owner/repository")
	}
	err := s.githubAPI(ctx, "GET", "/repos/"+name, nil, &repo)
	if err != nil {
		return repo, err
	}
	if !repo.Private || repo.Archived {
		return repo, fmt.Errorf("choose an active private repository for test execution")
	}
	if !testLabRepoPattern.MatchString(repo.FullName) {
		return repo, fmt.Errorf("invalid GitHub repository response")
	}
	return repo, nil
}
