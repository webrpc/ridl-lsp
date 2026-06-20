package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestGenerationBumps(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()
	path := filepath.Join(dir, "a.ridl")
	content := "webrpc = v1\n\nname = t\nversion = v0.0.1\n\nstruct User\n  - id: uint64\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := fileURI(path)

	g0 := srv.gen.Load()
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: protocol.DocumentURI(uri), Text: content, Version: 1}})
	g1 := srv.gen.Load()
	if g1 <= g0 {
		t.Fatalf("DidOpen must bump gen: %d -> %d", g0, g1)
	}

	_ = srv.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)}, Version: 2},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: content + "\nstruct B\n  - u: User\n"}},
	})
	g2 := srv.gen.Load()
	if g2 <= g1 {
		t.Fatalf("DidChange must bump gen")
	}

	_ = srv.DidSave(ctx, &protocol.DidSaveTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)}})
	if srv.gen.Load() != g2 {
		t.Fatal("DidSave must NOT bump gen")
	}

	_ = srv.DidChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{Changes: []*protocol.FileEvent{{URI: protocol.DocumentURI(uri), Type: protocol.FileChangeTypeChanged}}})
	g3 := srv.gen.Load()
	if g3 <= g2 {
		t.Fatalf(".ridl DidChangeWatchedFiles must bump gen")
	}

	_ = srv.DidClose(ctx, &protocol.DidCloseTextDocumentParams{TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)}})
	if srv.gen.Load() <= g3 {
		t.Fatal("DidClose must bump gen")
	}
}
