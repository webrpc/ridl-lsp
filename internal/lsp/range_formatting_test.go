package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestRangeFormattingFormatsFullDocumentRange(t *testing.T) {
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

	path := filepath.Join(dir, "range-format-full.ridl")
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

	edits, err := srv.RangeFormatting(ctx, &protocol.DocumentRangeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 range-formatting edit, got %#v", edits)
	}
	if edits[0].NewText != want {
		t.Fatalf("unexpected range formatting output:\nwant:\n%s\ngot:\n%s", want, edits[0].NewText)
	}
}

func TestRangeFormattingSkipsPartialRanges(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc= v1
name= testapp
version =v0.1.0

struct   User
 -  id  :uint64
`
	path := filepath.Join(dir, "range-format-partial.ridl")
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

	edits, err := srv.RangeFormatting(ctx, &protocol.DocumentRangeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range: protocol.Range{
			Start: protocol.Position{Line: 4, Character: 0},
			End:   protocol.Position{Line: 5, Character: 12},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 0 {
		t.Fatalf("expected no partial range-formatting edits, got %#v", edits)
	}
}
