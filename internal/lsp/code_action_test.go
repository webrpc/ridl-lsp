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

func TestCodeActionOffersRemoveMissingImportQuickFix(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser(id: uint64) => (user: User)
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

service TestService
  - GetUser(id: uint64) => (user: User)
`

	path := filepath.Join(dir, "code-action-missing-import.ridl")
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

	diagnostics := client.getDiagnostics(uri)
	if len(diagnostics) == 0 {
		t.Fatal("expected diagnostics for missing import")
	}

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context: protocol.CodeActionContext{
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
			Diagnostics: diagnostics,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 quick fix, got %d: %#v", len(actions), actions)
	}

	action := actions[0]
	if action.Title != `Remove unresolved import "types.ridl"` {
		t.Fatalf("unexpected action title %q", action.Title)
	}
	if action.Kind != protocol.QuickFix {
		t.Fatalf("unexpected action kind %q", action.Kind)
	}
	if action.Edit == nil {
		t.Fatal("expected quick fix edit")
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	if len(edits) != 1 {
		t.Fatalf("expected 1 quick fix edit, got %#v", edits)
	}

	got := applyTextEdit(t, content, edits[0])
	if got != want {
		t.Fatalf("unexpected quick fix result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCodeActionRemovesOnlyMissingImportLineFromImportBlock(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	sharedDir := filepath.Join(dir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sharedContent := `webrpc = v1

struct Account
  - id: uint64
`
	sharedPath := filepath.Join(sharedDir, "shared.ridl")
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - missing.ridl
  - shared/shared.ridl

service TestService
  - GetAccount(id: uint64) => (account: Account)
  - GetUser(id: uint64) => (user: User)
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - shared/shared.ridl

service TestService
  - GetAccount(id: uint64) => (account: Account)
  - GetUser(id: uint64) => (user: User)
`

	path := filepath.Join(dir, "code-action-missing-import-block.ridl")
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
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
			Diagnostics: []protocol.Diagnostic{{Source: "ridl"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(actions) != 1 {
		t.Fatalf("expected 1 quick fix, got %d: %#v", len(actions), actions)
	}

	got := applyTextEdit(t, content, actions[0].Edit.Changes[protocol.DocumentURI(uri)][0])
	if got != want {
		t.Fatalf("unexpected quick fix result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCodeActionSkipsMissingImportQuickFixForNonImportDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

oops
`
	path := filepath.Join(dir, "code-action-no-import-fix.ridl")
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

	diagnostics := client.getDiagnostics(uri)
	if len(diagnostics) == 0 {
		t.Fatal("expected diagnostics for invalid document")
	}

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context: protocol.CodeActionContext{
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
			Diagnostics: diagnostics,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("expected no missing-import quick fix, got %#v", actions)
	}
}

func applyTextEdit(t *testing.T, content string, edit protocol.TextEdit) string {
	t.Helper()

	start := offsetAtPosition(t, content, edit.Range.Start)
	end := offsetAtPosition(t, content, edit.Range.End)
	return content[:start] + edit.NewText + content[end:]
}

func offsetAtPosition(t *testing.T, content string, pos protocol.Position) int {
	t.Helper()

	line := uint32(0)
	character := uint32(0)
	for offset, r := range content {
		if line == pos.Line && character == pos.Character {
			return offset
		}

		if r == '\n' {
			line++
			character = 0
			continue
		}

		character++
	}

	if line == pos.Line && character == pos.Character {
		return len(content)
	}

	t.Fatalf("position %+v out of bounds for content", pos)
	return 0
}
