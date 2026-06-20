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

// TestCodeLensImportedTypeRefCount asserts that the CodeLens on a type
// declaration counts references from other files that import and use it.
// base.ridl defines struct Base; user.ridl imports it and uses Base in a
// field. The lens on Base must show "1 reference" (the field in user.ridl).
// includeDeclaration is false in CodeLens, so the declaration itself does not
// add to the count.
func TestCodeLensImportedTypeRefCount(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	baseContent := `webrpc = v1

name = base
version = v0.0.1

struct Base
  - id: uint64
`
	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseContent), 0o644); err != nil {
		t.Fatal(err)
	}

	userContent := `webrpc = v1

name = user
version = v0.0.1

import
  - base.ridl

struct User
  - base: Base
`
	userPath := filepath.Join(dir, "user.ridl")
	if err := os.WriteFile(userPath, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open base.ridl — CodeLens is requested on it; user.ridl is picked up via
	// the workspace walk inside referenceCandidatePaths.
	baseURI := fileURI(basePath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(baseURI),
			Text:    baseContent,
			Version: 1,
		},
	})

	lenses, err := srv.CodeLens(ctx, &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(baseURI)},
	})
	if err != nil {
		t.Fatal(err)
	}

	baseLens := findCodeLensAtPosition(lenses, positionAt(t, baseContent, "Base\n"))
	if baseLens == nil {
		t.Fatalf("missing code lens for Base declaration in %#v", lenses)
	}
	if baseLens.Command == nil {
		t.Fatal("expected resolved code lens command, got nil")
	}
	if baseLens.Command.Title != "1 reference" {
		t.Fatalf("expected \"1 reference\" for Base (used once in user.ridl field), got %q", baseLens.Command.Title)
	}
}

// TestCodeLensErrorRefCount asserts that the CodeLens on an error declaration
// counts the single method that lists it in its errors clause.
func TestCodeLensErrorRefCount(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = errtest
version = v0.0.1

error 1 Foo "msg" HTTP 404

service FooService
  - M() => (ok: bool) errors Foo
`
	path := filepath.Join(dir, "errref.ridl")
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

	fooLens := findCodeLensAtPosition(lenses, positionAt(t, content, "Foo "))
	if fooLens == nil {
		t.Fatalf("missing code lens for Foo error declaration in %#v", lenses)
	}
	if fooLens.Command == nil {
		t.Fatal("expected resolved code lens command, got nil")
	}
	if fooLens.Command.Title != "1 reference" {
		t.Fatalf("expected \"1 reference\" for Foo error (used in M() errors clause), got %q", fooLens.Command.Title)
	}
}

// TestCodeLensReturnsErrorOnCanceledContext verifies that CodeLens propagates
// ctx.Err() rather than returning a (misleading) nil-error empty/partial result
// when the request context is already cancelled.
//
// DidOpen uses a live context so s.docs.Get succeeds and the test exercises the
// ctx check inside CodeLens itself — if DidOpen also used the cancelled ctx the
// doc would never be registered and the early-exit `!ok` branch would mask the
// real assertion.
func TestCodeLensReturnsErrorOnCanceledContext(t *testing.T) {
	srv, _, dir := setupServer(t)

	content := `webrpc = v1

name = canceltest
version = v0.0.1

struct Foo
  - id: uint64
`
	path := filepath.Join(dir, "cancel.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	// Register the document with a live context so s.docs.Get succeeds.
	_ = srv.DidOpen(context.Background(), &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the CodeLens call

	lenses, err := srv.CodeLens(ctx, &protocol.CodeLensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err == nil {
		t.Fatalf("expected non-nil error for cancelled context, got nil (lenses: %#v)", lenses)
	}
	if lenses != nil {
		t.Fatalf("expected nil lenses for cancelled context, got %#v", lenses)
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
