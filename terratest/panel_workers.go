package test

import (
	"context"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/operations"
)

type panelWorkers = operations.Workers

func (p *localControlPanel) stopWorkers(ctx context.Context) error {
	p.workers.Stop()
	p.stopCacheLab()
	p.stopTestLab()
	p.stopDailyReadiness()
	return p.workers.Wait(ctx)
}

func (p *localControlPanel) startPanelWorker(task func()) error {
	if !p.workers.Start(func(context.Context) { task() }) {
		return fmt.Errorf("Runway is shutting down")
	}
	return nil
}
