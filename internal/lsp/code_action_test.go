package lsp

import (
	"context"
	"os"
	"path/filepath"
	"sort"
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

func TestCodeActionOffersBulkRemoveAllUnresolvedImports(t *testing.T) {
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
  - missing-a.ridl
  - shared/shared.ridl
  - missing-b.ridl

service TestService
  - GetAccount(id: uint64) => (account: Account)
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - shared/shared.ridl

service TestService
  - GetAccount(id: uint64) => (account: Account)
`

	path := filepath.Join(dir, "code-action-remove-all-missing-imports.ridl")
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

	if len(actions) != 3 {
		t.Fatalf("expected 3 quick fixes, got %d: %#v", len(actions), actions)
	}

	var bulkAction *protocol.CodeAction
	for i := range actions {
		if actions[i].Title == "Remove all unresolved imports" {
			bulkAction = &actions[i]
			break
		}
	}
	if bulkAction == nil {
		t.Fatalf("missing bulk unresolved-import action in %#v", actions)
	}
	if bulkAction.Edit == nil {
		t.Fatal("expected bulk action edit")
	}

	edits := bulkAction.Edit.Changes[protocol.DocumentURI(uri)]
	if len(edits) != 2 {
		t.Fatalf("expected 2 bulk edits, got %#v", edits)
	}

	got := applyTextEdits(t, content, edits)
	if got != want {
		t.Fatalf("unexpected bulk quick fix result:\nwant:\n%s\ngot:\n%s", want, got)
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

func TestCodeActionOffersAddMissingImportQuickFix(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

service TestService
  - GetUser() => (user: User)
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`

	path := filepath.Join(dir, "code-action-add-import.ridl")
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
		t.Fatal("expected diagnostics for unresolved type")
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

	action := findCodeActionByTitle(actions, `Import "types.ridl" for "User"`)
	if action == nil {
		t.Fatalf("missing add-import quick fix in %#v", actions)
	}
	if action.Edit == nil {
		t.Fatal("expected add-import edit")
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	if len(edits) != 1 {
		t.Fatalf("expected 1 add-import edit, got %#v", edits)
	}

	got := applyTextEdit(t, content, edits[0])
	if got != want {
		t.Fatalf("unexpected add-import quick fix result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCodeActionAppendsMissingImportToExistingBlock(t *testing.T) {
	srv, client, dir := setupServer(t)
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

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - shared/shared.ridl

service TestService
  - GetAccount() => (account: Account)
  - GetUser() => (user: User)
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - shared/shared.ridl
  - types.ridl

service TestService
  - GetAccount() => (account: Account)
  - GetUser() => (user: User)
`

	path := filepath.Join(dir, "code-action-append-import.ridl")
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
		t.Fatal("expected diagnostics for unresolved type")
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

	action := findCodeActionByTitle(actions, `Import "types.ridl" for "User"`)
	if action == nil {
		t.Fatalf("missing append-import quick fix in %#v", actions)
	}
	if action.Edit == nil {
		t.Fatal("expected append-import edit")
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	if len(edits) != 1 {
		t.Fatalf("expected 1 append-import edit, got %#v", edits)
	}

	got := applyTextEdit(t, content, edits[0])
	if got != want {
		t.Fatalf("unexpected append-import quick fix result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCodeActionSkipsAmbiguousAddMissingImportQuickFix(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	firstContent := `webrpc = v1

struct User
  - id: uint64
`
	firstPath := filepath.Join(dir, "types-a.ridl")
	if err := os.WriteFile(firstPath, []byte(firstContent), 0o644); err != nil {
		t.Fatal(err)
	}

	secondContent := `webrpc = v1

struct User
  - name: string
`
	secondPath := filepath.Join(dir, "types-b.ridl")
	if err := os.WriteFile(secondPath, []byte(secondContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "code-action-ambiguous-import.ridl")
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
		t.Fatal("expected diagnostics for unresolved type")
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

	if action := findCodeActionByTitle(actions, `Import "types-a.ridl" for "User"`); action != nil {
		t.Fatalf("unexpected ambiguous add-import quick fix %#v", action)
	}
	if action := findCodeActionByTitle(actions, `Import "types-b.ridl" for "User"`); action != nil {
		t.Fatalf("unexpected ambiguous add-import quick fix %#v", action)
	}
}

func TestCodeActionPrefersDefinerOverReExporter(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	// organization.ridl defines OrgID
	orgContent := `webrpc = v1

struct OrgID
  - value: string
`
	orgPath := filepath.Join(dir, "organization.ridl")
	if err := os.WriteFile(orgPath, []byte(orgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// user.ridl imports organization.ridl (re-exports OrgID)
	userContent := `webrpc = v1

import
  - organization.ridl

struct User
  - id: uint64
  - orgID: OrgID
`
	userPath := filepath.Join(dir, "user.ridl")
	if err := os.WriteFile(userPath, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// project.ridl uses OrgID without importing anything
	content := `webrpc = v1

name = testapp
version = v0.1.0

struct Project
  - id: uint64
  - orgID: OrgID
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - organization.ridl

struct Project
  - id: uint64
  - orgID: OrgID
`

	path := filepath.Join(dir, "project.ridl")
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
		t.Fatal("expected diagnostics for unresolved type")
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

	// Should suggest organization.ridl (definer), NOT user.ridl (re-exporter)
	action := findCodeActionByTitle(actions, `Import "organization.ridl" for "OrgID"`)
	if action == nil {
		t.Fatalf("missing auto-import for OrgID from organization.ridl in %#v", actions)
	}

	if wrongAction := findCodeActionByTitle(actions, `Import "user.ridl" for "OrgID"`); wrongAction != nil {
		t.Fatalf("should not suggest re-exporter user.ridl")
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	got := applyTextEdit(t, content, edits[0])
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func findCodeActionByTitle(actions []protocol.CodeAction, title string) *protocol.CodeAction {
	for i := range actions {
		if actions[i].Title == title {
			return &actions[i]
		}
	}
	return nil
}

func applyTextEdit(t *testing.T, content string, edit protocol.TextEdit) string {
	t.Helper()

	start := offsetAtPosition(t, content, edit.Range.Start)
	end := offsetAtPosition(t, content, edit.Range.End)
	return content[:start] + edit.NewText + content[end:]
}

func applyTextEdits(t *testing.T, content string, edits []protocol.TextEdit) string {
	t.Helper()

	sorted := append([]protocol.TextEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Range.Start.Line != sorted[j].Range.Start.Line {
			return sorted[i].Range.Start.Line > sorted[j].Range.Start.Line
		}
		return sorted[i].Range.Start.Character > sorted[j].Range.Start.Character
	})

	result := content
	for _, edit := range sorted {
		result = applyTextEdit(t, result, edit)
	}
	return result
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
