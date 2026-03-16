package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestCodeActionOffersFormatDocumentSourceAction(t *testing.T) {
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

	path := filepath.Join(dir, "code-action-format.ridl")
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

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context:      protocol.CodeActionContext{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 code action, got %d: %#v", len(actions), actions)
	}

	action := actions[0]
	if action.Title != "Format document" {
		t.Fatalf("unexpected code action title %q", action.Title)
	}
	if action.Kind != protocol.Source {
		t.Fatalf("unexpected code action kind %q", action.Kind)
	}
	if action.Edit == nil {
		t.Fatal("expected code action edit")
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %#v", edits)
	}
	if edits[0].Range != fullDocumentRange(content) {
		t.Fatalf("expected full-document range %+v, got %+v", fullDocumentRange(content), edits[0].Range)
	}
	if edits[0].NewText != want {
		t.Fatalf("unexpected formatted output:\nwant:\n%s\ngot:\n%s", want, edits[0].NewText)
	}
}

func TestCodeActionSkipsAlreadyFormattedDocument(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0
`
	path := filepath.Join(dir, "code-action-noop.ridl")
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

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context:      protocol.CodeActionContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("expected no code actions, got %#v", actions)
	}
}

func TestCodeActionRespectsRequestedKinds(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc= v1
name= testapp
version =v0.1.0
`
	path := filepath.Join(dir, "code-action-kind-filter.ridl")
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

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context: protocol.CodeActionContext{
			Only: []protocol.CodeActionKind{protocol.QuickFix},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("expected no code actions for quickfix-only request, got %#v", actions)
	}
}
