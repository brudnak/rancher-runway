package test

// Writing drafts deliberately live outside package.json. A recovered draft is
// unfinished work, never a preserved plan, result, report, or exported artifact.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"os"
	"path/filepath"
	"time"
)

const testPackageDraftLimit = 8 << 20

type testPackageDraftPlan struct {
	Title      string            `json:"title"`
	IssueURL   string            `json:"issueUrl"`
	IssueTitle string            `json:"issueTitle"`
	FixURL     string            `json:"fixUrl,omitempty"`
	FixTitle   string            `json:"fixTitle,omitempty"`
	Summary    string            `json:"summary"`
	Status     string            `json:"status"`
	Notes      string            `json:"notes"`
	Cases      []testPackageCase `json:"cases"`
}

type testPackageObservationDraft struct {
	SessionID string `json:"sessionId"`
	CaseID    string `json:"caseId"`
	BaseNotes string `json:"baseNotes"`
	Notes     string `json:"notes"`
}

type testPackageWritingDraft struct {
	Revision            string                        `json:"revision"`
	UpdatedAt           *time.Time                    `json:"updatedAt,omitempty"`
	BasePlanFingerprint string                        `json:"basePlanFingerprint"`
	Plan                *testPackageDraftPlan         `json:"plan"`
	Observations        []testPackageObservationDraft `json:"observations"`
}

type testPackageDraftEnvelope struct {
	Version int                     `json:"version"`
	Draft   testPackageWritingDraft `json:"draft"`
}

type testPackageObservationConflict struct {
	SessionID string `json:"sessionId"`
	CaseID    string `json:"caseId"`
	Reason    string `json:"reason"`
}

type testPackageDraftResponse struct {
	Draft                  testPackageWritingDraft          `json:"draft"`
	CurrentPlanFingerprint string                           `json:"currentPlanFingerprint"`
	PlanChanged            bool                             `json:"planChanged"`
	ObservationConflicts   []testPackageObservationConflict `json:"observationConflicts"`
	Warnings               []string                         `json:"warnings"`
}

var errTestPackageDraftConflict = fmt.Errorf("writing draft changed in another view; recover or reload that draft before saving: %w", errTestPackageConflict)

func testPackageEditablePlan(pkg testPackage) testPackageDraftPlan {
	return testPackageDraftPlan{Title: pkg.Title, IssueURL: pkg.IssueURL, IssueTitle: pkg.IssueTitle, FixURL: pkg.FixURL, FixTitle: pkg.FixTitle, Summary: pkg.Summary, Status: pkg.Status, Notes: pkg.Notes, Cases: pkg.Cases}
}

func testPackagePlanFingerprint(plan testPackageDraftPlan) string {
	// Normalize optional slices so a JS round trip does not change the identity.
	plan.Cases = append([]testPackageCase{}, plan.Cases...)
	for i := range plan.Cases {
		if plan.Cases[i].Steps == nil {
			plan.Cases[i].Steps = []testPackageStep{}
		}
		if plan.Cases[i].Selection == nil {
			plan.Cases[i].Selection = []string{}
		}
	}
	raw, _ := json.Marshal(plan)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func validateTestPackageWritingDraft(draft testPackageWritingDraft) error {
	if len(draft.BasePlanFingerprint) != 64 {
		return fmt.Errorf("draft must identify its saved plan checkpoint")
	}
	if _, err := hex.DecodeString(draft.BasePlanFingerprint); err != nil {
		return fmt.Errorf("invalid draft plan fingerprint")
	}
	if plan := draft.Plan; plan != nil {
		// Incomplete titles, instructions, and URLs are valid while writing. Only
		// bounds, field types, and stable identity are required at this stage.
		if !packageText(plan.Title, 240, false) || !packageText(plan.IssueURL, 2048, false) || !packageText(plan.IssueTitle, 500, false) || !packageText(plan.FixURL, 2048, false) || !packageText(plan.FixTitle, 500, false) || !packageText(plan.Summary, 32000, false) || !packageText(plan.Notes, 100000, false) || !packageEnum(plan.Status, "planning", "reproducing", "awaiting-fix", "validating", "verified", "archived") || len(plan.Cases) > 250 {
			return fmt.Errorf("writing draft exceeds the package field limits")
		}
		ids := map[string]bool{}
		for _, c := range plan.Cases {
			if !cachelab.IDPattern.MatchString(c.ID) || ids[c.ID] || !packageText(c.Title, 240, false) || !packageText(c.Preconditions, 16000, false) || !packageText(c.Expected, 16000, false) || !packageEnum(c.Automation, "manual", "planned", "automated") || !packageText(c.AutomationURL, 2048, false) || len(c.Steps) > 100 || len(c.Selection) > 500 {
				return fmt.Errorf("invalid or oversized case in writing draft")
			}
			if err := validatePackageAutomation(c); err != nil {
				return err
			}
			ids[c.ID] = true
			steps := map[string]bool{}
			for _, step := range c.Steps {
				if !cachelab.IDPattern.MatchString(step.ID) || steps[step.ID] || !packageText(step.Instruction, 16000, false) || !packageText(step.Expected, 16000, false) {
					return fmt.Errorf("invalid or oversized step in writing draft")
				}
				steps[step.ID] = true
			}
			for _, selection := range c.Selection {
				if !packageText(selection, 1024, false) {
					return fmt.Errorf("oversized automated selection in writing draft")
				}
			}
		}
	}
	if len(draft.Observations) > 2000 {
		return fmt.Errorf("too many unfinished case observations; save or discard some before continuing")
	}
	seen := map[string]bool{}
	for _, observation := range draft.Observations {
		key := observation.SessionID + "/" + observation.CaseID
		if !cachelab.IDPattern.MatchString(observation.SessionID) || !cachelab.IDPattern.MatchString(observation.CaseID) || seen[key] || !packageText(observation.BaseNotes, 32000, false) || !packageText(observation.Notes, 32000, false) {
			return fmt.Errorf("invalid or oversized observation draft")
		}
		seen[key] = true
	}
	return nil
}

func (s *testPackageService) readWritingDraftLocked(id string, pkg testPackage) (testPackageWritingDraft, error) {
	raw, err := testPackageReadBounded(filepath.Join(s.root, id, "draft.json"), testPackageDraftLimit)
	if errors.Is(err, os.ErrNotExist) {
		return testPackageWritingDraft{BasePlanFingerprint: testPackagePlanFingerprint(testPackageEditablePlan(pkg)), Observations: []testPackageObservationDraft{}}, nil
	}
	if err != nil {
		return testPackageWritingDraft{}, fmt.Errorf("writing draft could not be read; its existing file was preserved: %w", err)
	}
	var envelope testPackageDraftEnvelope
	if err := decodeTestPackage(raw, &envelope); err != nil || envelope.Version != 1 || !cachelab.IDPattern.MatchString(envelope.Draft.Revision) || envelope.Draft.UpdatedAt == nil || envelope.Draft.UpdatedAt.IsZero() {
		return testPackageWritingDraft{}, fmt.Errorf("writing draft is invalid; its existing file was preserved")
	}
	if err := validateTestPackageWritingDraft(envelope.Draft); err != nil {
		return testPackageWritingDraft{}, fmt.Errorf("writing draft is invalid; its existing file was preserved: %w", err)
	}
	if envelope.Draft.Observations == nil {
		envelope.Draft.Observations = []testPackageObservationDraft{}
	}
	return envelope.Draft, nil
}

func testPackageWritingDraftResponse(pkg testPackage, draft testPackageWritingDraft) testPackageDraftResponse {
	current := testPackagePlanFingerprint(testPackageEditablePlan(pkg))
	response := testPackageDraftResponse{Draft: draft, CurrentPlanFingerprint: current, ObservationConflicts: []testPackageObservationConflict{}, Warnings: []string{}}
	response.PlanChanged = draft.Plan != nil && draft.BasePlanFingerprint != current && testPackagePlanFingerprint(*draft.Plan) != current
	if response.PlanChanged {
		response.Warnings = append(response.Warnings, "The saved plan changed after this draft began. Review the recovered draft before replacing that checkpoint.")
	}
	for _, observation := range draft.Observations {
		reason := "The original session or case was removed. This draft is still available to copy."
		for _, session := range pkg.Sessions {
			if session.ID != observation.SessionID {
				continue
			}
			for _, result := range session.Results {
				if result.CaseID != observation.CaseID {
					continue
				}
				reason = ""
				if result.Notes == observation.Notes {
					// Saving the result may have completed just before the app
					// closed. Identical checkpointed text needs no recovery.
					break
				} else if session.Status != "active" {
					reason = "The original session is preserved. This draft is still available to copy, but cannot rewrite its observations."
				} else if result.Notes != observation.BaseNotes && result.Notes != observation.Notes {
					reason = "The saved observation changed in another view. Review both versions before saving this draft."
				}
				break
			}
			break
		}
		if reason != "" {
			response.ObservationConflicts = append(response.ObservationConflicts, testPackageObservationConflict{SessionID: observation.SessionID, CaseID: observation.CaseID, Reason: reason})
		}
	}
	if len(response.ObservationConflicts) > 0 {
		response.Warnings = append(response.Warnings, "Some unfinished observations need review. Their text remains in the recovered draft.")
	}
	return response
}

func (s *testPackageService) handleWritingDraft(req testPackageRequest) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg, ok := s.packages[req.ID]
	if !ok || !cachelab.IDPattern.MatchString(req.ID) {
		return nil, fmt.Errorf("test package not found")
	}
	draft, err := s.readWritingDraftLocked(req.ID, pkg)
	if err != nil {
		return nil, err
	}
	if req.Action == "draft-read" {
		return testPackageWritingDraftResponse(pkg, draft), nil
	}
	if !packageEnum(req.Action, "draft-write", "draft-clear") {
		return nil, fmt.Errorf("unknown writing draft action")
	}
	if req.DraftRevision != draft.Revision {
		return nil, errTestPackageDraftConflict
	}
	if req.Action == "draft-clear" {
		draft = testPackageWritingDraft{BasePlanFingerprint: testPackagePlanFingerprint(testPackageEditablePlan(pkg)), Observations: []testPackageObservationDraft{}}
	} else {
		if req.Draft == nil {
			return nil, fmt.Errorf("writing draft is required")
		}
		draft = *req.Draft
		if err := validateTestPackageWritingDraft(draft); err != nil {
			return nil, err
		}
		if draft.Observations == nil {
			draft.Observations = []testPackageObservationDraft{}
		}
	}
	// A durable empty revision is a tombstone: a late request with the old
	// revision can never resurrect text that another view already discarded.
	draft.Revision = cachelab.ID()
	now := time.Now().UTC()
	draft.UpdatedAt = &now
	raw, err := json.MarshalIndent(testPackageDraftEnvelope{Version: 1, Draft: draft}, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(raw) > testPackageDraftLimit {
		return nil, fmt.Errorf("writing draft exceeds 8 MiB; save or split some unfinished work")
	}
	if err := writePrivateConfigAtomically(filepath.Join(s.root, req.ID, "draft.json"), raw); err != nil {
		return nil, fmt.Errorf("writing draft could not be saved: %w", err)
	}
	return testPackageWritingDraftResponse(pkg, draft), nil
}
