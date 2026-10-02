package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
)

func (p *localControlPanel) cacheLabService() (*cachelab.Service, error) {
	p.cacheLabMu.Lock()
	defer p.cacheLabMu.Unlock()
	if p.cacheLab != nil {
		return p.cacheLab, nil
	}
	root, err := absoluteFromWorkingDir(durableDataPath("cache-lab"))
	if err != nil {
		return nil, err
	}
	s, err := cachelab.New(root, cachelab.Options{Workers: &p.workers, ResolveCluster: p.resolveLabCluster})
	if err == nil {
		p.cacheLab = s
	}
	return s, err
}

func cacheLabBackfill(s *cachelab.Service, candidates []clusterWorkspaceRecord) error {
	return s.BackfillClusters(func(w cachelab.Workspace) string {
		if w.Kind == "steve" {
			for _, candidate := range candidates {
				if candidate.ID == localLabClusterWorkspaceID("steve", w.URL) && candidate.Role == "steve" && candidate.RunID == w.URL {
					return candidate.ID
				}
			}
			return ""
		}
		host := w.URL
		if w.Kind == "kubeconfig" {
			host = ""
		}
		return uniqueLabCluster(candidates, host, w.Kubeconfig, w.Context)
	})
}
