package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/internal/server"
	"net/http"
)

func (p *localControlPanel) imageLookupBackend() *imagelookup.Service {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.imageLookup == nil {
		p.imageLookup = imagelookup.New()
	}
	return p.imageLookup
}

func (p *localControlPanel) imageHandlers() server.ImageHandlers {
	return server.ImageHandlers{Authorize: p.authorizedLocalAction, Backend: p.imageLookupBackend}
}
func (p *localControlPanel) handleImageLookupSearch(w http.ResponseWriter, r *http.Request) {
	p.imageHandlers().Search(w, r)
}
func (p *localControlPanel) handleImageLookupInspect(w http.ResponseWriter, r *http.Request) {
	p.imageHandlers().Inspect(w, r)
}
func (p *localControlPanel) handleImageLookupSourceBuildYAML(w http.ResponseWriter, r *http.Request) {
	p.imageHandlers().SourceBuildYAML(w, r)
}
func decodeImageLookupJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return server.DecodeImageJSON(w, r, dst)
}
