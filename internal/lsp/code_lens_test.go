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
	if userLens.Command != nil {
		t.Fatalf("expected unresolved code lens command, got %#v", userLens.Command)
	}

	notFoundLens := findCodeLensAtPosition(lenses, positionAt(t, content, "UserNotFound"))
	if notFoundLens == nil {
		t.Fatalf("missing code lens for UserNotFound declaration in %#v", lenses)
	}
}

func TestCodeLensResolveBuildsShowReferencesCommand(t *testing.T) {
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

	resolved, err := srv.CodeLensResolve(ctx, lens)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.Command == nil {
		t.Fatalf("expected resolved code lens command, got %#v", resolved)
	}
	if resolved.Command.Command != showReferencesCommand {
		t.Fatalf("unexpected code lens command %q", resolved.Command.Command)
	}
	if resolved.Command.Title != "2 references" {
		t.Fatalf("unexpected code lens title %q", resolved.Command.Title)
	}
	if len(resolved.Command.Arguments) != 3 {
		t.Fatalf("expected show-references arguments, got %#v", resolved.Command.Arguments)
	}

	locations, ok := resolved.Command.Arguments[2].([]protocol.Location)
	if !ok {
		t.Fatalf("expected reference locations in code lens args, got %#v", resolved.Command.Arguments[2])
	}
	if len(locations) != 2 {
		t.Fatalf("expected 2 reference locations, got %#v", locations)
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
