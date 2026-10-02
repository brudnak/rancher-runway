package test

import "net/http"

func (p *localControlPanel) handleIssueReadiness(w http.ResponseWriter, r *http.Request) {
	p.prHandlers().Readiness(w, r)
}
