package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestPrepareRenameReturnsIdentifierRange(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - ListUsers() => (users: []User)
`
	path := filepath.Join(dir, "prepare-rename.ridl")
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

	rng, err := srv.PrepareRename(ctx, &protocol.PrepareRenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "[]"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rng == nil {
		t.Fatal("expected prepare rename range")
	}
	want := protocol.Range{
		Start: positionAfter(t, content, "[]"),
		End: protocol.Position{
			Line:      positionAfter(t, content, "[]").Line,
			Character: positionAfter(t, content, "[]").Character + 4,
		},
	}
	if *rng != want {
		t.Fatalf("expected range %+v, got %+v", want, *rng)
	}
}

func TestRenameTypeAcrossWorkspaceFiles(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - ../shared/types.ridl

service TestService
  - GetUser() => (user: User)
  - ListUsers() => (users: []User)
`
	mainPath := filepath.Join(mainDir, "main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	typesDir := filepath.Join(dir, "shared")
	if err := os.MkdirAll(typesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(typesDir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	typesURI := fileURI(typesPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(typesURI),
			Text:    typesContent,
			Version: 1,
		},
	})

	edit, err := srv.Rename(ctx, &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(typesURI)},
			Position:     positionAfter(t, typesContent, "struct "),
		},
		NewName: "Account",
	})
	if err != nil {
		t.Fatal(err)
	}
	if edit == nil {
		t.Fatal("expected workspace edit")
	}

	assertWorkspaceEdits(t, edit,
		editExpectation{uri: typesURI, pos: positionAfter(t, typesContent, "struct "), newText: "Account"},
		editExpectation{uri: fileURI(mainPath), pos: positionAfter(t, mainContent, "(user: "), newText: "Account"},
		editExpectation{uri: fileURI(mainPath), pos: positionAfter(t, mainContent, "[]"), newText: "Account"},
	)
}

func TestRenameErrorDoesNotTouchSubstringMatches(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

error 1 UserNotFound "user not found" HTTP 404
error 2 UserNotFoundAgain "user not found again" HTTP 404

service TestService
  - GetUser() => (user: string) errors UserNotFound
`
	path := filepath.Join(dir, "rename-error.ridl")
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

	edit, err := srv.Rename(ctx, &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "error 1 "),
		},
		NewName: "MissingUser",
	})
	if err != nil {
		t.Fatal(err)
	}
	if edit == nil {
		t.Fatal("expected workspace edit")
	}

	assertWorkspaceEdits(t, edit,
		editExpectation{uri: uri, pos: positionAfter(t, content, "error 1 "), newText: "MissingUser"},
		editExpectation{uri: uri, pos: positionAfter(t, content, "errors "), newText: "MissingUser"},
	)
	assertNoWorkspaceEditAt(t, edit, uri, positionAfter(t, content, "error 2 "))
}

func TestRenameRejectsBuiltInTypes(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

service TestService
  - GetUser() => (user: string)
`
	path := filepath.Join(dir, "rename-builtin.ridl")
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

	rng, err := srv.PrepareRename(ctx, &protocol.PrepareRenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "(user: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rng != nil {
		t.Fatalf("expected no prepare rename range for built-in type, got %+v", *rng)
	}

	edit, err := srv.Rename(ctx, &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "(user: "),
		},
		NewName: "Text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		t.Fatalf("expected no workspace edit for built-in type rename, got %#v", edit)
	}
}

type editExpectation struct {
	uri     string
	pos     protocol.Position
	newText string
}

func assertWorkspaceEdits(t *testing.T, edit *protocol.WorkspaceEdit, want ...editExpectation) {
	t.Helper()
	if edit == nil {
		t.Fatal("expected workspace edit")
	}

	count := 0
	for _, edits := range edit.Changes {
		count += len(edits)
	}
	if count != len(want) {
		t.Fatalf("expected %d text edits, got %d: %#v", len(want), count, edit.Changes)
	}

	for _, expected := range want {
		edits := edit.Changes[protocol.DocumentURI(expected.uri)]
		found := false
		for _, textEdit := range edits {
			if textEdit.Range.Start == expected.pos && textEdit.NewText == expected.newText {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing edit at %s:%+v in %#v", expected.uri, expected.pos, edit.Changes)
		}
	}
}

func assertNoWorkspaceEditAt(t *testing.T, edit *protocol.WorkspaceEdit, uri string, pos protocol.Position) {
	t.Helper()
	if edit == nil {
		return
	}
	for _, textEdit := range edit.Changes[protocol.DocumentURI(uri)] {
		if textEdit.Range.Start == pos {
			t.Fatalf("unexpected edit at %s:%+v in %#v", uri, pos, edit.Changes)
		}
	}
}
