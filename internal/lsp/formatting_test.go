package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestFormattingFormatsLiveBufferContent(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	diskContent := `webrpc = v1
name = disk
version = v0.1.0
`
	openContent := `webrpc= v1
name= testapp
version =v0.1.0

struct   User
 -  id  :uint64
   +json=id

service   TestService
 - GetUser(  userID:uint64)=> ( user :User)
`
	want := `webrpc = v1
name = testapp
version = v0.1.0

struct User
  - id: uint64
    +json = id

service TestService
  - GetUser(userID: uint64) => (user: User)
`

	path := filepath.Join(dir, "format-live-buffer.ridl")
	if err := os.WriteFile(path, []byte(diskContent), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    openContent,
			Version: 1,
		},
	})

	edits, err := srv.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(edits) != 1 {
		t.Fatalf("expected 1 formatting edit, got %d: %#v", len(edits), edits)
	}
	if edits[0].NewText != want {
		t.Fatalf("unexpected formatted output:\nwant:\n%s\ngot:\n%s", want, edits[0].NewText)
	}
	if edits[0].Range != fullDocumentRange(openContent) {
		t.Fatalf("expected full-document range %+v, got %+v", fullDocumentRange(openContent), edits[0].Range)
	}
}

func TestFormattingReturnsNoEditsForFormattedDocument(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0

struct User
  - id: uint64
    + json = id

service TestService
  - GetUser(userID: uint64) => (user: User)
`
	path := filepath.Join(dir, "format-noop.ridl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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

	edits, err := srv.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 0 {
		t.Fatalf("expected no formatting edits, got %#v", edits)
	}
}

func TestFormattingReturnsErrorForInvalidDocument(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0

oops
`
	path := filepath.Join(dir, "format-invalid.ridl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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

	edits, err := srv.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err == nil {
		t.Fatal("expected formatting error for invalid document")
	}
	if edits != nil {
		t.Fatalf("expected no edits on formatting error, got %#v", edits)
	}
}
