package test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func (p *localControlPanel) handleTestPackages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if (r.Method == http.MethodGet && !p.authorizedLocalBrowserRead(r)) || (r.Method == http.MethodPost && !p.authorizedLocalAction(r)) {
		http.Error(w, "unauthorized", 401)
		return
	}
	s, err := p.testPackageService()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.Method == http.MethodGet {
		p.syncPackageRuns()
		packages, err := s.list()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fingerprints := map[string]string{}
		for _, pkg := range packages {
			fingerprints[pkg.ID] = testPackagePlanFingerprint(testPackageEditablePlan(pkg))
		}
		writeJSON(w, map[string]any{"packages": packages, "planFingerprints": fingerprints, "library": s.packageLibrarySnapshot()})
		return
	}
	var req testPackageRequest
	rawRequest, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 96<<20))
	if readErr != nil || decodeTestPackage(rawRequest, &req) != nil {
		http.Error(w, "invalid Test Packages request", 400)
		return
	}
	var result any
	var environment *testPackageEnvironment
	var evidence *testPackageEvidence
	var artifact []byte
	switch req.Action {
	case "start-session":
		var value testPackageEnvironment
		value, err = p.testPackageEnvironment(req.Environment)
		environment = &value
	case "attach-evidence":
		var value testPackageEvidence
		value, artifact, err = p.testPackageEvidence(req)
		evidence = &value
	}
	if err == nil {
		switch req.Action {
		case "library-save":
			result, err = s.savePackageLibrary(req)
		case "library-export":
			result, err = s.exportPackageLibrary(req)
		case "automation-prepare":
			result, err = s.preparePackageAutomation(req)
		case "draft-read", "draft-write", "draft-clear":
			result, err = s.handleWritingDraft(req)
		case "read-evidence":
			result, err = s.readEvidence(req)
		case "preview-environment":
			var value testPackageEnvironment
			value, err = p.testPackageEnvironment(req.Environment)
			result = map[string]any{"environment": value}
		case "refresh-environment":
			var value testPackageEnvironment
			value, err = p.refreshTestPackageEnvironment(r.Context(), req.Environment)
			result = map[string]any{"environment": value}
		case "log-targets", "capture-logs":
			result, err = p.handleTestPackageLogs(r.Context(), s, req)
		case "fix-lookup":
			result, err = p.testPackageFixLookup(r.Context(), req)
		case "create", "update", "delete", "start-session", "mark-step", "case-result", "finish-session", "delete-session", "attach-evidence", "remove-evidence":
			result, err = s.mutate(req, environment, evidence, artifact)
		default:
			result, err = p.handleTestPackageTransfer(r.Context(), s, req)
		}
	}
	if err != nil {
		code := 400
		if errors.Is(err, errTestPackageConflict) {
			code = 409
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSON(w, result)
}

func (s *testPackageService) readEvidence(req testPackageRequest) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, ok := s.packages[req.ID]
	if !ok {
		return nil, fmt.Errorf("test package not found")
	}
	for _, session := range pkg.Sessions {
		if session.ID != req.SessionID {
			continue
		}
		for _, e := range session.Evidence {
			if e.ID != req.SourceID {
				continue
			}
			if e.Artifact == nil || e.Artifact.Omitted {
				return nil, fmt.Errorf("this evidence has metadata only; no log file was included")
			}
			if e.Artifact.MediaType != "text/plain" {
				return nil, fmt.Errorf("only text evidence can be opened in the log reader")
			}
			path, err := s.artifactPath(pkg.ID, e.Artifact.Name)
			if err != nil {
				return nil, err
			}
			raw, err := testPackageReadBounded(path, 8<<20)
			if err != nil {
				return nil, fmt.Errorf("preserved log is unavailable: %w", err)
			}
			sum := sha256.Sum256(raw)
			if int64(len(raw)) != e.Artifact.Bytes || hex.EncodeToString(sum[:]) != e.Artifact.SHA256 {
				return nil, fmt.Errorf("preserved log checksum changed; the file was not opened")
			}
			return map[string]any{"text": string(raw), "name": e.Name, "mediaType": e.Artifact.MediaType}, nil
		}
	}
	return nil, fmt.Errorf("evidence not found")
}
