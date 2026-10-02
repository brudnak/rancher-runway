package test

import (
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"
)

func (p *localControlPanel) cacheLabSteve(s *cachelab.Service, runID string) (any, error) {
	record, ok := p.readSteveLabRunRecord(safeRunPathSegment(runID))
	if !ok {
		return nil, fmt.Errorf("Steve session not found")
	}
	clusterID, err := p.resolveLabCluster(localLabClusterWorkspaceID("steve", record.RunID), "", "", "")
	if err != nil {
		return nil, err
	}
	return s.CaptureSteve(cachelab.SteveSource{RunID: record.RunID, SteveRef: record.SteveRef, SourceDir: record.SourceDir}, clusterID)
}
