package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestTypeDefinitionResolvesStructFieldType(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

struct Account
  - owner: User
`
	path := filepath.Join(dir, "type-definition-field.ridl")
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

	locations, err := srv.TypeDefinition(ctx, &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAt(t, content, "owner"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSingleDefinition(t, locations, uri, positionAt(t, content, "User"))
}

func TestTypeDefinitionResolvesMethodOutputType(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`
	mainPath := filepath.Join(dir, "main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
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

	mainURI := fileURI(mainPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(mainURI),
			Text:    mainContent,
			Version: 1,
		},
	})

	typesURI := fileURI(typesPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(typesURI),
			Text:    typesContent,
			Version: 1,
		},
	})

	locations, err := srv.TypeDefinition(ctx, &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
			Position:     positionAt(t, mainContent, "user"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSingleDefinition(t, locations, typesURI, positionAt(t, typesContent, "User"))
}

func TestTypeDefinitionResolvesCompositeTypeReference(t *testing.T) {
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
	path := filepath.Join(dir, "type-definition-composite.ridl")
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

	locations, err := srv.TypeDefinition(ctx, &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "[]"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSingleDefinition(t, locations, uri, positionAt(t, content, "User"))
}

func TestTypeDefinitionSkipsDeclarations(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64
`
	path := filepath.Join(dir, "type-definition-skip.ridl")
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

	locations, err := srv.TypeDefinition(ctx, &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAt(t, content, "User"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(locations) != 0 {
		t.Fatalf("expected no type-definition locations for declaration, got %#v", locations)
	}
}
