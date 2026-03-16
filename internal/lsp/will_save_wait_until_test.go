package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestWillSaveWaitUntilReturnsFormattingEdits(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc= v1
name= testapp
version =v0.1.0

struct   User
 -  id  :uint64
`
	want := `webrpc = v1
name = testapp
version = v0.1.0

struct User
  - id: uint64
`

	path := filepath.Join(dir, "will-save-format.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	edits, err := srv.WillSaveWaitUntil(ctx, &protocol.WillSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 will-save edit, got %#v", edits)
	}
	if edits[0].NewText != want {
		t.Fatalf("unexpected will-save formatted output:\nwant:\n%s\ngot:\n%s", want, edits[0].NewText)
	}
}

func TestWillSaveWaitUntilSkipsInvalidDocuments(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0

oops
`
	path := filepath.Join(dir, "will-save-invalid.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	edits, err := srv.WillSaveWaitUntil(ctx, &protocol.WillSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if edits != nil {
		t.Fatalf("expected no will-save edits for invalid document, got %#v", edits)
	}
}
