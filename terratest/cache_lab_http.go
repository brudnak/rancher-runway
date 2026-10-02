package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
	"github.com/brudnak/ha-rancher-rke2/internal/server"
	"net/http"
)

func (p *localControlPanel) cacheHandlers() server.CacheHandlers {
	return server.CacheHandlers{Authorize: p.authorizedLocalAction, Backend: p.cacheLabService, Steve: p.cacheLabSteve, Backfill: func(s *cachelab.Service) error {
		candidates, err := p.labClusterCandidates()
		if err != nil {
			return err
		}
		return cacheLabBackfill(s, candidates)
	}}
}
func (p *localControlPanel) handleCacheLab(w http.ResponseWriter, r *http.Request) {
	p.cacheHandlers().Handle(w, r)
}
