package lsp

import (
	"context"
	"errors"
	"testing"

	"go.lsp.dev/protocol"
)

// initializeWithWatchCap runs Initialize+Initialized for a server whose client
// advertises (or does not advertise) DidChangeWatchedFiles dynamic registration.
func initializeWithWatchCap(t *testing.T, srv *Server, dynamic bool) {
	t.Helper()
	ctx := context.Background()

	var workspaceCaps *protocol.WorkspaceClientCapabilities
	if dynamic {
		workspaceCaps = &protocol.WorkspaceClientCapabilities{
			DidChangeWatchedFiles: &protocol.DidChangeWatchedFilesWorkspaceClientCapabilities{
				DynamicRegistration: true,
			},
		}
	}

	_, err := srv.Initialize(ctx, &protocol.InitializeParams{
		Capabilities: protocol.ClientCapabilities{
			Workspace: workspaceCaps,
		},
	})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := srv.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("Initialized: %v", err)
	}
}

func TestWatcherRegistration_CapabilityAdvertised_Success(t *testing.T) {
	srv, client, _ := setupServer(t)

	initializeWithWatchCap(t, srv, true)

	regs := client.getRegistrations()
	if len(regs) != 1 {
		t.Fatalf("want 1 registration, got %d", len(regs))
	}
	if regs[0].Method != "workspace/didChangeWatchedFiles" {
		t.Errorf("want method workspace/didChangeWatchedFiles, got %q", regs[0].Method)
	}
	opts, ok := regs[0].RegisterOptions.(protocol.DidChangeWatchedFilesRegistrationOptions)
	if !ok {
		t.Fatalf("RegisterOptions wrong type: %T", regs[0].RegisterOptions)
	}
	if len(opts.Watchers) != 1 || opts.Watchers[0].GlobPattern != "**/*.ridl" {
		t.Errorf("want watcher **/*.ridl, got %+v", opts.Watchers)
	}
	if !srv.cacheEnabled.Load() {
		t.Error("cacheEnabled must be true after successful registration")
	}
}

func TestWatcherRegistration_CapabilityNotAdvertised(t *testing.T) {
	srv, client, _ := setupServer(t)

	initializeWithWatchCap(t, srv, false)

	if regs := client.getRegistrations(); len(regs) != 0 {
		t.Errorf("want 0 registrations, got %d", len(regs))
	}
	if srv.cacheEnabled.Load() {
		t.Error("cacheEnabled must be false when capability not advertised")
	}
}

func TestWatcherRegistration_CapabilityAdvertised_RegisterFails(t *testing.T) {
	srv, client, _ := setupServer(t)
	client.registerErr = errors.New("test: register denied")

	initializeWithWatchCap(t, srv, true)

	if srv.cacheEnabled.Load() {
		t.Error("cacheEnabled must be false when RegisterCapability returns an error")
	}
}
