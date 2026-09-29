package test

import (
	"sync"
	"time"
)

// Remote inventory is informational; local workspace and operation checks still
// run on every state request. An unreachable cluster must not hold those checks
// hostage or start another discovery process on every browser retry.
type panelDiscoverySnapshot[T any] struct {
	mu         sync.Mutex
	value      T
	refreshing bool
	finishedAt time.Time
}

func (s *panelDiscoverySnapshot[T]) snapshot(collect func() T) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refreshing && (s.finishedAt.IsZero() || time.Since(s.finishedAt) >= 5*time.Second) {
		s.refreshing = true
		go func() {
			value := collect()
			s.mu.Lock()
			defer s.mu.Unlock()
			s.value = value
			s.finishedAt = time.Now()
			s.refreshing = false
		}()
	}
	return s.value, s.refreshing
}

func (p *localControlPanel) clusterDiscoveryState() panelClusterState {
	state, refreshing := p.clusterDiscovery.snapshot(func() panelClusterState {
		clusters := p.discoverClusters()
		p.rememberClusterSnapshot(clusters)
		updatedAt := time.Now()
		return panelClusterState{Items: clusters, UpdatedAt: &updatedAt}
	})
	state.Refreshing = refreshing
	if state.Items == nil {
		state.Items = []clusterView{}
	}
	return state
}

func (p *localControlPanel) awsDiscoveryState(records []panelRunRecord) panelAWSInventoryState {
	state, refreshing := p.awsDiscovery.snapshot(func() panelAWSInventoryState {
		return p.discoverAWSInventory(records)
	})
	state.Refreshing = refreshing
	if state.Items == nil {
		state.Items = []awsResourceView{}
	}
	return state
}
