package test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Connection discovery exposes only management environments already known to
// Runway. A downstream cluster's kubeconfig cannot capture its Rancher server.
type rancherConnectionTarget struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	Version        string `json:"version,omitempty"`
	RunID          string `json:"runId,omitempty"`
	Role           string `json:"role,omitempty"`
	Kubeconfig     string `json:"kubeconfig,omitempty"`
	Provisioning   bool   `json:"provisioning"`
	Reachable      bool   `json:"reachable"`
	CacheSupported bool   `json:"cacheSupported"`
}

func (p *localControlPanel) rancherConnectionTargets() []rancherConnectionTarget {
	p.mu.Lock()
	clusters := make([]clusterView, 0, len(p.clusterSnapshot))
	for _, c := range p.clusterSnapshot {
		clusters = append(clusters, c)
	}
	p.mu.Unlock()
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].ID < clusters[j].ID })
	byURL := map[string]rancherConnectionTarget{}
	for _, c := range clusters {
		if c.Type == "downstream" || c.Role == "downstream" {
			continue
		}
		base, err := cachelab.URL(c.RancherURL)
		if err != nil || base == "" {
			continue
		}
		target := rancherConnectionTarget{ID: c.ID, Name: c.Name, URL: base, Version: c.Version, RunID: c.RunID, Role: c.Role, Provisioning: c.Provisioning, Reachable: c.Reachable, CacheSupported: c.Role != "docker" && c.Type != "linode"}
		if target.Name == "" {
			target.Name = cachelab.SourceLabel(base)
		}
		if target.CacheSupported && c.KubeconfigPath != "" {
			if info, err := os.Stat(c.KubeconfigPath); err == nil && info.Mode().IsRegular() {
				target.Kubeconfig = c.KubeconfigPath
			}
		}
		old, exists := byURL[base]
		if !exists || (old.Provisioning && !target.Provisioning) || (old.Provisioning == target.Provisioning && old.Kubeconfig == "" && target.Kubeconfig != "") {
			byURL[base] = target
		}
	}
	targets := make([]rancherConnectionTarget, 0, len(byURL))
	for _, target := range byURL {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Name == targets[j].Name {
			return targets[i].URL < targets[j].URL
		}
		return targets[i].Name < targets[j].Name
	})
	return targets
}

func (p *localControlPanel) handleRancherTargets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", 403)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	writeJSON(w, map[string]any{"targets": p.rancherConnectionTargets()})
}

type rancherTokenRequest struct {
	UseBootstrapPassword bool   `json:"useBootstrapPassword"`
	URL                  string `json:"url"`
	Username             string `json:"username"`
	Password             string `json:"password"`
	TTLMinutes           int64  `json:"ttlMinutes"`
	Purpose              string `json:"purpose"`
	Insecure             bool   `json:"insecure"`
	CAPEM                string `json:"caPem"`
}
type rancherTokenResult struct {
	Token       string `json:"token"`
	ID          string `json:"id"`
	Description string `json:"description"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
	Warning     string `json:"warning,omitempty"`
}
type rancherTokenResponse struct {
	Token     string `json:"token"`
	ID        string `json:"id"`
	ExpiresAt string `json:"expiresAt"`
	Created   string `json:"created"`
	TTL       int64  `json:"ttl"`
}

var rancherCredentialID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func (in *rancherTokenRequest) validate() error {
	base, err := cachelab.URL(in.URL)
	if err != nil {
		return errors.New("Enter a Rancher URL without credentials, a query, or a fragment.")
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return errors.New("Enter a valid Rancher URL.")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))) {
		return errors.New("Password sign-in requires HTTPS, except for a Rancher on localhost.")
	}
	if u.RawPath != "" || strings.Contains(u.Path, "..") {
		return errors.New("Enter the base Rancher URL without encoded or parent path segments.")
	}
	in.URL = base
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || len(in.Username) > 256 || strings.ContainsAny(in.Username, "\r\n\x00") {
		return errors.New("Enter a local Rancher username.")
	}
	if len(in.Password) == 0 || len(in.Password) > 16384 {
		return errors.New("Enter a Rancher password of at most 16 KiB.")
	}
	if in.TTLMinutes < 1 || in.TTLMinutes > 30*24*60 {
		return errors.New("Choose a token expiry between one minute and 30 days.")
	}
	if in.Purpose != "cache" && in.Purpose != "test" && in.Purpose != "downstream" {
		return errors.New("Choose Cache Lab, Test Lab, or downstream creation for this token.")
	}
	return nil
}

func (p *localControlPanel) handleRancherToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !p.authorizedLocalAction(r) {
		http.Error(w, "invalid control panel token", 403)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var in rancherTokenRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		http.Error(w, "Invalid token request. Check the connection fields.", 400)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "Expected one token request.", 400)
		return
	}
	if err := p.useConfiguredRancherPassword(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := in.validate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, err := generateRancherConnectionToken(ctx, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

// Passwords and response bodies must never enter logs, command arguments, or
// persistent state. Only the derived token is returned to the connection draft.
func generateRancherConnectionToken(ctx context.Context, in rancherTokenRequest) (result rancherTokenResult, err error) {
	if err = in.validate(); err != nil {
		return result, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: in.Insecure} // Explicit per-connection user choice.
	if strings.TrimSpace(in.CAPEM) != "" {
		roots, poolErr := x509.SystemCertPool()
		if poolErr != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(in.CAPEM)) {
			return result, errors.New("The custom CA does not contain a valid PEM certificate.")
		}
		tlsConfig.RootCAs = roots
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	purpose := "Cache Lab"
	if in.Purpose == "test" {
		purpose = "Test Lab"
	} else if in.Purpose == "downstream" {
		purpose = "Downstream creation"
	}
	description := "Rancher Runway · " + purpose + " · " + time.Now().UTC().Format(time.RFC3339)
	login, err := rancherCredentialRequest(ctx, client, http.MethodPost, in.URL+"/v3-public/localProviders/local?action=login", "", map[string]any{"username": in.Username, "password": in.Password, "responseType": "token", "ttl": int64(5 * time.Minute / time.Millisecond), "description": "Rancher Runway · temporary sign-in"}, "sign-in")
	in.Password = ""
	if err != nil {
		return result, err
	}
	loginID := rancherResponseTokenID(login)
	if loginID == "" || !validRancherCredential(login.Token) {
		return result, errors.New("Rancher did not return a usable sign-in token. Check API & Keys in Rancher before trying again.")
	}
	// Revoke only the temporary session from this request, even when the client
	// disconnects. Never follow server-provided links when sending credentials.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, cleanupErr := rancherCredentialRequest(cleanupCtx, client, http.MethodPost, in.URL+"/v3/tokens?action=logout", login.Token, map[string]any{}, "temporary sign-in cleanup")
		if cleanupErr != nil {
			warning := "The temporary sign-in could not be revoked. Remove session " + loginID + " (Rancher Runway temporary sign-in) in Rancher’s API & Keys."
			if err != nil {
				err = fmt.Errorf("%s %s", err.Error(), warning)
			} else {
				result.Warning = warning
			}
		}
	}()
	created, err := rancherCredentialRequest(ctx, client, http.MethodPost, in.URL+"/v3/tokens", login.Token, map[string]any{"type": "token", "description": description, "ttl": in.TTLMinutes * 60000}, "token creation")
	if err != nil {
		return result, err
	}
	id := rancherResponseTokenID(created)
	if !validRancherCredential(created.Token) || id == "" || id == loginID {
		return result, errors.New("Rancher returned an unexpected token. Check API & Keys in Rancher before trying again.")
	}
	result = rancherTokenResult{Token: created.Token, ID: id, Description: description, ExpiresAt: rancherCredentialExpiry(created)}
	return result, nil
}

func rancherCredentialExpiry(response rancherTokenResponse) string {
	if expiry, err := time.Parse(time.RFC3339, response.ExpiresAt); err == nil {
		return expiry.UTC().Format(time.RFC3339)
	}
	// Some Rancher versions populate expiresAt asynchronously. Derive it only
	// from server-returned creation time and TTL, never the requested lifetime.
	if response.TTL > 0 && response.TTL <= int64(365*24*time.Hour/time.Millisecond) {
		if created, err := time.Parse(time.RFC3339, response.Created); err == nil {
			return created.Add(time.Duration(response.TTL) * time.Millisecond).UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func validRancherCredential(token string) bool {
	return len(token) >= 8 && len(token) <= 32768 && !strings.ContainsAny(token, "\r\n\x00 ")
}
func rancherResponseTokenID(response rancherTokenResponse) string {
	// The bearer prefix identifies the actual token. Reject conflicting metadata.
	id, _, found := strings.Cut(response.Token, ":")
	if !found || !rancherCredentialID.MatchString(id) || (response.ID != "" && response.ID != id) {
		return ""
	}
	return id
}
func rancherCredentialRequest(ctx context.Context, client *http.Client, method, endpoint, bearer string, payload any, stage string) (rancherTokenResponse, error) {
	var result rancherTokenResponse
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return result, errors.New("Could not prepare Rancher sign-in.")
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return result, errors.New("Invalid Rancher connection.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := client.Do(req)
	if err != nil {
		var certificateError *tls.CertificateVerificationError
		if errors.As(err, &certificateError) {
			return result, errors.New("Rancher’s TLS certificate could not be verified. Provide a trusted CA in Cache Lab, or explicitly allow this server’s self-signed certificate.")
		}
		return result, fmt.Errorf("Rancher %s could not finish. Check the URL and connectivity. Before generating another token, check API & Keys in Rancher for an entry from this attempt.", stage)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch {
		case response.StatusCode >= 300 && response.StatusCode < 400:
			return result, errors.New("Rancher redirected the request. Enter its final HTTPS address; credentials were not forwarded.")
		case response.StatusCode == 401:
			return result, errors.New("Rancher rejected sign-in. Check the local username and password. For SSO, paste an API token instead.")
		case response.StatusCode == 403:
			return result, errors.New("Rancher denied this action. Finish initial Rancher setup and check this user’s permission to create API tokens.")
		case response.StatusCode == 429:
			return result, errors.New("Rancher is limiting sign-in requests. Wait before trying again.")
		default:
			return result, fmt.Errorf("Rancher %s returned HTTP %d. For token creation, try a shorter expiry allowed by your server.", stage, response.StatusCode)
		}
	}
	if method == http.MethodDelete || stage == "temporary sign-in cleanup" {
		return result, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &result) != nil {
		return result, errors.New("Rancher returned an unreadable credential response. Check API & Keys before trying again.")
	}
	return result, nil
}
