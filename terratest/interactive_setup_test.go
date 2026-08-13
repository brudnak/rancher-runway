package test

import (
	"errors"
	"testing"
)

func TestInteractiveResolutionFailureReturnsToEditorWithoutEndingSession(t *testing.T) {
	resultCh := make(chan interactiveResult, 1)
	server := &interactiveServer{
		phase:     phaseResolving,
		submitted: true,
		resultCh:  resultCh,
	}

	server.returnResolutionFailureToEditor(errors.New("preferred registry image pair is unavailable"))

	server.mu.Lock()
	phase := server.phase
	submitted := server.submitted
	resolveErr := server.resolveErr
	server.mu.Unlock()

	if phase != phaseEditor {
		t.Fatalf("phase = %q, want %q", phase, phaseEditor)
	}
	if submitted {
		t.Fatal("submitted remained true after resolution failure")
	}
	if resolveErr != "preferred registry image pair is unavailable" {
		t.Fatalf("resolveErr = %q", resolveErr)
	}

	select {
	case result := <-resultCh:
		t.Fatalf("resolution failure ended the interactive session: %#v", result)
	default:
	}
}
