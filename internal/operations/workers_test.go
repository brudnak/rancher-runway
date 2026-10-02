package operations_test

import (
	"context"
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/operations"
	"testing"
	"time"
)

func TestWorkersCancelDrainAndReject(t *testing.T) {
	var workers operations.Workers
	canceled := make(chan struct{})
	finish := make(chan struct{})
	saved := make(chan struct{})
	if !workers.Start(func(ctx context.Context) { <-ctx.Done(); close(canceled); <-finish; close(saved) }) {
		t.Fatal("start rejected")
	}
	workers.Stop()
	<-canceled
	if workers.Start(func(context.Context) { t.Error("work started after stop") }) {
		t.Fatal("accepted after stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := workers.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("did not wait for final write: %v", err)
	}
	close(finish)
	if err := workers.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-saved:
	default:
		t.Fatal("returned before final persistence")
	}
}
