package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestOnTypeFormattingFormatsLiveBufferOnNewline(t *testing.T) {
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

	path := filepath.Join(dir, "on-type-format.ridl")
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

	edits, err := srv.OnTypeFormatting(ctx, &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Position:     protocol.Position{Line: 5, Character: 0},
		Ch:           "\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(edits) != 1 {
		t.Fatalf("expected 1 on-type formatting edit, got %#v", edits)
	}
	if edits[0].Range != fullDocumentRange(content) {
		t.Fatalf("expected full-document range %+v, got %+v", fullDocumentRange(content), edits[0].Range)
	}
	if edits[0].NewText != want {
		t.Fatalf("unexpected on-type formatting output:\nwant:\n%s\ngot:\n%s", want, edits[0].NewText)
	}
}

func TestOnTypeFormattingSkipsUnsupportedTrigger(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0
`
	path := filepath.Join(dir, "on-type-skip.ridl")
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

	edits, err := srv.OnTypeFormatting(ctx, &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Position:     protocol.Position{Line: 2, Character: 0},
		Ch:           ":",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 0 {
		t.Fatalf("expected no on-type formatting edits for unsupported trigger, got %#v", edits)
	}
}

func TestOnTypeFormattingReturnsNoEditsForFormattedDocument(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0

struct User
  - id: uint64
`
	path := filepath.Join(dir, "on-type-noop.ridl")
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

	edits, err := srv.OnTypeFormatting(ctx, &protocol.DocumentOnTypeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Position:     protocol.Position{Line: 5, Character: 0},
		Ch:           "\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 0 {
		t.Fatalf("expected no edits for already formatted document, got %#v", edits)
	}
}
