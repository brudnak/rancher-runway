package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
	"net/http"
)

func (p *localControlPanel) streamIssueReadiness(w http.ResponseWriter, r *http.Request, req prbuild.Request) {
	p.prHandlers().StreamReadiness(w, r, req)
}
