package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestCodeLensReturnsDeclarationLenses(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

error 1 UserNotFound "user not found" HTTP 404

service TestService
  - GetUser() => (user: User) errors UserNotFound
`
	path := filepath.Join(dir, "code-lens.ridl")
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

	lenses, err := srv.CodeLens(ctx, &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(lenses) != 2 {
		t.Fatalf("expected 2 declaration code lenses, got %#v", lenses)
	}

	userLens := findCodeLensAtPosition(lenses, positionAt(t, content, "User\n"))
	if userLens == nil {
		t.Fatalf("missing code lens for User declaration in %#v", lenses)
	}
	if userLens.Command == nil {
		t.Fatalf("expected eager resolved code lens command, got nil")
	}
	if userLens.Command.Command != showReferencesCommand {
		t.Fatalf("unexpected code lens command %q", userLens.Command.Command)
	}
	if userLens.Data != nil {
		t.Fatalf("expected Data cleared on resolved lens, got %#v", userLens.Data)
	}

	notFoundLens := findCodeLensAtPosition(lenses, positionAt(t, content, "UserNotFound"))
	if notFoundLens == nil {
		t.Fatalf("missing code lens for UserNotFound declaration in %#v", lenses)
	}
}

func TestCodeLensBuildsShowReferencesCommand(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - GetUser() => (user: User)
  - ListUsers() => (users: []User)
`
	path := filepath.Join(dir, "code-lens-resolve.ridl")
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

	lenses, err := srv.CodeLens(ctx, &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	lens := findCodeLensAtPosition(lenses, positionAt(t, content, "User\n"))
	if lens == nil {
		t.Fatalf("missing code lens for User declaration in %#v", lenses)
	}
	if lens.Command == nil {
		t.Fatalf("expected resolved code lens command, got nil")
	}
	if lens.Command.Command != showReferencesCommand {
		t.Fatalf("unexpected code lens command %q", lens.Command.Command)
	}
	if lens.Command.Title != "2 references" {
		t.Fatalf("unexpected code lens title %q", lens.Command.Title)
	}
	if len(lens.Command.Arguments) != 3 {
		t.Fatalf("expected show-references arguments, got %#v", lens.Command.Arguments)
	}

	locations, ok := lens.Command.Arguments[2].([]protocol.Location)
	if !ok {
		t.Fatalf("expected reference locations in code lens args, got %#v", lens.Command.Arguments[2])
	}
	if len(locations) != 2 {
		t.Fatalf("expected 2 reference locations, got %#v", locations)
	}
}

func TestCodeLensResolveIsPassthrough(t *testing.T) {
	srv, _, _ := setupServer(t)
	ctx := context.Background()

	in := &protocol.CodeLens{
		Range:   protocol.Range{Start: protocol.Position{Line: 4}},
		Command: &protocol.Command{Title: "1 reference", Command: showReferencesCommand},
	}
	out, err := srv.CodeLensResolve(ctx, in)
	if err != nil {
		t.Fatalf("CodeLensResolve error: %v", err)
	}
	if out != in {
		t.Fatal("expected CodeLensResolve to return the same lens unchanged")
	}
}

func findCodeLensAtPosition(lenses []protocol.CodeLens, pos protocol.Position) *protocol.CodeLens {
	for i := range lenses {
		lens := &lenses[i]
		if lens.Range.Start == pos {
			return lens
		}
	}
	return nil
}
