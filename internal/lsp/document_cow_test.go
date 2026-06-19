package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

// TestDidChangeDoesNotMutatePriorSnapshot pins the copy-on-write invariant: a
// *Document handed out by the store must never be mutated in place. A concurrent
// reader (handlers run in their own goroutines via jsonrpc2.AsyncHandler) holding
// an earlier snapshot must keep seeing that snapshot's content after DidChange.
func TestDidChangeDoesNotMutatePriorSnapshot(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	path := filepath.Join(dir, "doc.ridl")
	if err := os.WriteFile(path, []byte(validRIDL), 0644); err != nil {
		t.Fatal(err)
	}
	uri := fileURI(path)

	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: protocol.DocumentURI(uri), Text: validRIDL, Version: 1},
	})

	snapshot, ok := srv.docs.Get(uri)
	if !ok {
		t.Fatal("document not in store after DidOpen")
	}
	originalContent := snapshot.Content

	const changed = "webrpc = v1\n\nname = changed\nversion = v0.2.0\n"
	_ = srv.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Version:                2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: changed}},
	})

	if snapshot.Content != originalContent {
		t.Fatalf("prior snapshot was mutated in place: content is now %q, want %q", snapshot.Content, originalContent)
	}
	if snapshot.Version != 1 {
		t.Fatalf("prior snapshot version mutated: got %d, want 1", snapshot.Version)
	}

	updated, ok := srv.docs.Get(uri)
	if !ok {
		t.Fatal("document missing after DidChange")
	}
	if updated.Content != changed {
		t.Fatalf("store did not reflect new content: got %q", updated.Content)
	}
	if updated.Version != 2 {
		t.Fatalf("store version: got %d, want 2", updated.Version)
	}
}
