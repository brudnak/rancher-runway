package test

import (
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"io"
	"log"
)

func (s *interactiveServer) runResolution() {
	tap := &logTap{}
	tap.onLine = func(line string) { s.appendLog(line) }

	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(io.MultiWriter(originalWriter, tap))
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	totalHAs := configuredRancherInstanceCount()
	var err error
	if !isHostedTenantK3SDeployment() && !isLinodeDockerDeployment() {
		err = settings.ValidateCustomHostnameConfig(totalHAs)
	}
	var plans []*RancherResolvedPlan
	if err == nil {
		if isHostedTenantK3SDeployment() {
			plans, err = prepareHostedTenantRancherConfiguration(totalHAs)
		} else if isLinodeDockerDeployment() {
			plans, err = prepareLinodeDockerPlans(totalHAs)
		} else {
			plans, err = prepareRancherConfiguration(totalHAs)
		}
	}
	if err == nil {
		logResolvedPlans(plans)
	}

	tap.flush()

	if err != nil {
		s.returnResolutionFailureToEditor(err)
		return
	}

	planText := buildResolvedPlansDialogMessage(plans)

	s.mu.Lock()
	s.plans = plans
	s.planText = planText
	s.phase = phaseReview
	s.mu.Unlock()

	s.broadcast(interactiveEvent{Type: "plan", Plan: planText})
	s.broadcast(interactiveEvent{Type: "phase", Phase: phaseReview})
}

func (s *interactiveServer) returnResolutionFailureToEditor(err error) {
	s.mu.Lock()
	s.resolveErr = err.Error()
	s.phase = phaseEditor
	s.submitted = false
	s.mu.Unlock()

	// Resolution failures are editable. Keep standalone and embedded setup
	// sessions alive so the user can change the registry selection and retry.
	s.broadcast(interactiveEvent{Type: "error", Error: err.Error()})
	s.broadcast(interactiveEvent{Type: "phase", Phase: phaseEditor})
}
