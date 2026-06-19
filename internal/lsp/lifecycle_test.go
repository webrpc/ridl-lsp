package lsp

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// TestShutdownTracksReceivedState: the exit code main picks depends on whether
// shutdown arrived before exit, so the flag must flip on Shutdown and start false.
func TestShutdownTracksReceivedState(t *testing.T) {
	srv := NewServer(zap.NewNop())

	if srv.ShutdownReceived() {
		t.Fatal("ShutdownReceived must be false before Shutdown")
	}

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	if !srv.ShutdownReceived() {
		t.Fatal("ShutdownReceived must be true after Shutdown")
	}
}
