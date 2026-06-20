package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

// TestParseDocumentSkipsCanceledContext: a cancelled request must not surface
// ctx.Err() as a "context canceled" diagnostic, nor clear the cached parse
// result. Parse returns the context error once ctx is done, and parseDocument
// must treat that as "stop", not "the document is broken" (regression guard for
// the I6 cancellation plumbing).
func TestParseDocumentSkipsCanceledContext(t *testing.T) {
	srv, _, dir := setupServer(t)

	path := filepath.Join(dir, "doc.ridl")
	if err := os.WriteFile(path, []byte(validRIDL), 0644); err != nil {
		t.Fatal(err)
	}
	uri := fileURI(path)

	_ = srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: protocol.DocumentURI(uri), Text: validRIDL, Version: 1},
	})

	doc, ok := srv.docs.Get(uri)
	if !ok {
		t.Fatal("document missing after DidOpen")
	}
	if doc.Result == nil {
		t.Fatal("expected a cached result for the valid document")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if diags := srv.parseDocument(ctx, doc); diags != nil {
		t.Fatalf("cancelled parse must yield no diagnostics, got %v", diags)
	}

	after, _ := srv.docs.Get(uri)
	if after.Result == nil {
		t.Fatal("cancelled parse must not clear the cached result")
	}
}

// TestParsePathHonorsCanceledContext: the ctx-aware parse used by the diagnostics
// path (e.g. transitive re-import checks) must stop on a cancelled request rather
// than parsing imports off a dead request.
func TestParsePathHonorsCanceledContext(t *testing.T) {
	srv, _, dir := setupServer(t)

	other := filepath.Join(dir, "other.ridl")
	if err := os.WriteFile(other, []byte(validRIDL), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// other.ridl is not open, so there is no cached result to short-circuit on:
	// parsePath must parse, and that parse must bail on the cancelled context.
	if got := srv.parsePath(ctx, other); got != nil {
		t.Fatalf("expected nil from parsePath on cancelled ctx, got %v", got)
	}
}
