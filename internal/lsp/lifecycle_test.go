package lsp

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// TestExitCodeFollowsShutdown pins the LSP contract: the process exits 0 only if
// shutdown arrived before exit, otherwise 1. The exit func is injected so the
// decision is observable without terminating the test binary.
func TestExitCodeFollowsShutdown(t *testing.T) {
	t.Run("exit without shutdown is non-zero", func(t *testing.T) {
		srv := NewServer(zap.NewNop())
		got, called := -1, false
		srv.exitProcess = func(code int) { got, called = code, true }

		_ = srv.Exit(context.Background())

		if !called {
			t.Fatal("Exit must terminate the process")
		}
		if got != 1 {
			t.Fatalf("exit code without prior shutdown: got %d, want 1", got)
		}
	})

	t.Run("shutdown then exit is zero", func(t *testing.T) {
		srv := NewServer(zap.NewNop())
		got := -1
		srv.exitProcess = func(code int) { got = code }

		_ = srv.Shutdown(context.Background())
		_ = srv.Exit(context.Background())

		if got != 0 {
			t.Fatalf("exit code after shutdown: got %d, want 0", got)
		}
	})
}
