package test

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func readinessDailyFixture(t *testing.T, root string, clock func() time.Time, calls *atomic.Int32) *dailyReadinessService {
	t.Helper()
	s, err := newDailyReadinessService(root)
	if err != nil {
		t.Fatal(err)
	}
	s.now = clock
	snapshot := myWorkSnapshot{Config: myWorkConfig{Repo: "rancher/rancher", Milestone: 14, Scope: "mine", User: "tester"}, Milestone: issueRadarMilestone{Number: 14, Title: "v2.14"}, Issues: []issueRadarIssue{{Number: 1, Title: "First", URL: "https://github.com/rancher/rancher/issues/1", State: "open"}, {Number: 2, Title: "Second", URL: "https://github.com/rancher/rancher/issues/2", State: "open"}}}
	s.scope = func() (myWorkSnapshot, error) { return snapshot, nil }
	s.fetch = func(_ context.Context, _ myWorkConfig) (myWorkSnapshot, error) { calls.Add(1); return snapshot, nil }
	s.plans = func() ([]testPackage, error) {
		return []testPackage{{ID: "planned", IssueURL: snapshot.Issues[0].URL, Cases: []testPackageCase{{Title: "test"}}}}, nil
	}
	s.scanFactory = func() func(context.Context, prbuild.Request) (prbuild.Report, error) {
		return func(_ context.Context, req prbuild.Request) (prbuild.Report, error) {
			qa := prbuild.QA{State: "found", Complete: true}
			verdict := "ready"
			if req.IssueURL == snapshot.Issues[1].URL {
				qa.State = "not_found"
				verdict = "in_progress"
			}
			return prbuild.Report{Issue: issueRadarIssue{Title: "Observed", State: "open"}, Verdict: verdict, Complete: true, QA: qa}, nil
		}
	}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
	})
	return s
}
func waitReadinessDaily(t *testing.T, s *dailyReadinessService) dailyReadinessState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		idle := s.cancel == nil
		s.mu.Unlock()
		state := s.snapshot()
		if idle && state.Report != nil && state.Report.Status != "running" {
			return state
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("daily scan did not finish")
	return dailyReadinessState{}
}
func TestDailyReadinessOncePerLocalDayAndRestart(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	now := time.Date(2026, 10, 1, 3, 59, 0, 0, time.UTC) // Sept 30 in New York.
	clock := func() time.Time { return now }
	s := readinessDailyFixture(t, root, clock, &calls)
	req := dailyReadinessRequest{Action: "auto", Timezone: "America/New_York"}
	if err := s.change(req); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || s.snapshot().Report != nil {
		t.Fatal("disabled scan ran")
	}
	enabled := true
	if err := s.change(dailyReadinessRequest{Action: "settings", Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.change(req); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	state := waitReadinessDaily(t, s)
	if calls.Load() != 1 || state.Report.Ready != 1 || state.Report.NeedsPlan != 1 || state.Report.QAFound != 1 || state.Report.QAMissing != 1 || state.Report.Remaining != 0 || state.Report.Day != "2026-09-30" {
		t.Fatalf("calls=%d state=%+v", calls.Load(), state.Report)
	}
	s = readinessDailyFixture(t, root, clock, &calls)
	if err := s.change(req); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("restart ran duplicate scan")
	}
	now = now.Add(2 * time.Minute)
	if err := s.change(req); err != nil {
		t.Fatal(err)
	}
	state = waitReadinessDaily(t, s)
	if calls.Load() != 2 || state.Report.Day != "2026-10-01" {
		t.Fatal("local midnight did not start new report")
	}
	if _, err := os.Stat(filepath.Join(root, ".daily-readiness.json")); err != nil {
		t.Fatal(err)
	}
}
func TestDailyReadinessCancellationAndInterruptedRestart(t *testing.T) {
	var calls atomic.Int32
	s := readinessDailyFixture(t, t.TempDir(), time.Now, &calls)
	started := make(chan struct{})
	s.scanFactory = func() func(context.Context, prbuild.Request) (prbuild.Report, error) {
		return func(ctx context.Context, _ prbuild.Request) (prbuild.Report, error) {
			close(started)
			<-ctx.Done()
			return prbuild.Report{}, ctx.Err()
		}
	}
	if err := s.change(dailyReadinessRequest{Action: "scan", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.change(dailyReadinessRequest{Action: "cancel"}); err != nil {
		t.Fatal(err)
	}
	state := waitReadinessDaily(t, s)
	if state.Report.Status != "cancelled" || state.Report.Remaining != 2 || state.Report.Checked != 0 {
		t.Fatalf("cancelled=%+v", state.Report)
	}
	s.mu.Lock()
	s.state.Report.Status = "running"
	err := s.persistLocked(s.state)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := newDailyReadinessService(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if restored.snapshot().Report.Status != "interrupted" {
		t.Fatal("crashed scan restored as running")
	}
}
func TestDailyReadinessPartialAndScopeChange(t *testing.T) {
	var calls atomic.Int32
	s := readinessDailyFixture(t, t.TempDir(), time.Now, &calls)
	enabled := true
	_ = s.change(dailyReadinessRequest{Action: "settings", Enabled: &enabled})
	s.scanFactory = func() func(context.Context, prbuild.Request) (prbuild.Report, error) {
		return func(_ context.Context, req prbuild.Request) (prbuild.Report, error) {
			if req.IssueURL == "https://github.com/rancher/rancher/issues/2" {
				return prbuild.Report{}, fmt.Errorf("rate limited")
			}
			return prbuild.Report{Issue: issueRadarIssue{State: "closed"}, Complete: true, QA: prbuild.QA{State: "found", Complete: true}}, nil
		}
	}
	req := dailyReadinessRequest{Action: "auto", Timezone: "UTC"}
	if err := s.change(req); err != nil {
		t.Fatal(err)
	}
	state := waitReadinessDaily(t, s)
	if state.Report.Status != "partial" || state.Report.QAUnknown != 1 || state.Report.Incomplete != 1 || state.Report.QAFound != 0 {
		t.Fatalf("partial=%+v", state.Report)
	}
	scope, _ := s.scope()
	scope.Config.Milestone = 15
	s.scope = func() (myWorkSnapshot, error) { return scope, nil }
	if err := s.change(req); err != nil {
		t.Fatal(err)
	}
	state = waitReadinessDaily(t, s)
	if calls.Load() != 2 || state.Report.Config.Milestone != 15 {
		t.Fatal("changed scope was not scanned")
	}
}

func TestDailyReadinessSkipsQANoneAndRemovesNewlyExemptPlanCount(t *testing.T) {
	var calls atomic.Int32
	s := readinessDailyFixture(t, t.TempDir(), time.Now, &calls)
	snapshot, _ := s.scope()
	snapshot.Issues[0].Labels = []struct {
		Name string `json:"name"`
	}{{Name: "QA/None"}}
	s.fetch = func(context.Context, myWorkConfig) (myWorkSnapshot, error) { return snapshot, nil }
	s.scanFactory = func() func(context.Context, prbuild.Request) (prbuild.Report, error) {
		return func(ctx context.Context, req prbuild.Request) (prbuild.Report, error) {
			if req.IssueURL == snapshot.Issues[0].URL {
				t.Error("QA/None was unnecessarily scanned")
			}
			prbuild.Notify(ctx, "Reading updated QA labels")
			issue := snapshot.Issues[1]
			issue.Labels = snapshot.Issues[0].Labels
			return prbuild.Report{Issue: issue, Verdict: "qa_not_required", Complete: true, QA: prbuild.QA{State: "not_required", Complete: true}}, nil
		}
	}
	if err := s.change(dailyReadinessRequest{Action: "scan", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	r := waitReadinessDaily(t, s).Report
	if r.Total != 1 || r.NeedsPlan != 0 || r.QAMissing != 0 || !r.Entries[0].QANone || len(r.Progress) != 1 {
		t.Fatalf("incorrect exempt counts: %+v", r)
	}
}
