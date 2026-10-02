package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"
	"github.com/brudnak/ha-rancher-rke2/internal/server"
	"net/http"
)

func (p *localControlPanel) prBuildVerifierBackend() *prbuild.Service {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prBuildVerifier == nil {
		if p.imageLookup == nil {
			p.imageLookup = imagelookup.New()
		}
		p.prBuildVerifier = prbuild.New(p.imageLookup)
	}
	return p.prBuildVerifier
}

func (p *localControlPanel) prHandlers() server.PRHandlers {
	return server.PRHandlers{Authorize: p.authorizedLocalAction, Backend: p.prBuildVerifierBackend}
}
func (p *localControlPanel) handlePRBuildVerify(w http.ResponseWriter, r *http.Request) {
	p.prHandlers().Verify(w, r)
}
