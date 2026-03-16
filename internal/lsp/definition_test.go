package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestDefinitionResolvesSameFileType(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "definition-type.ridl")
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

	locations, err := srv.Definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAtOccurrence(t, content, "User", 2),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSingleDefinition(t, locations, uri, positionAt(t, content, "User"))
}

func TestDefinitionResolvesSameFileMethodError(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

error 1 UserNotFound "user not found" HTTP 404

struct User
  - id: uint64

service TestService
  - GetUser(id: uint64) => (user: User) errors UserNotFound
`
	path := filepath.Join(dir, "definition-error.ridl")
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

	locations, err := srv.Definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAtOccurrence(t, content, "UserNotFound", 1),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSingleDefinition(t, locations, uri, positionAt(t, content, "UserNotFound"))
}

func TestDefinitionResolvesImportedType(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser(id: uint64) => (user: User)
`
	mainPath := filepath.Join(dir, "definition-main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	typesContent := `webrpc = v1

struct User
  - id: uint64
  - name: string
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
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

	locations, err := srv.Definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
			Position:     positionAtOccurrence(t, mainContent, "User", 1),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSingleDefinition(t, locations, typesURI, positionAt(t, typesContent, "User"))
}

func TestDefinitionResolvesCompositeTypeReference(t *testing.T) {
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
	path := filepath.Join(dir, "definition-composite.ridl")
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

	locations, err := srv.Definition(ctx, &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAtOccurrence(t, content, "User", 2),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSingleDefinition(t, locations, uri, positionAt(t, content, "User"))
}

func assertSingleDefinition(t *testing.T, locations []protocol.Location, wantURI string, wantPos protocol.Position) {
	t.Helper()

	if len(locations) != 1 {
		t.Fatalf("expected 1 definition location, got %d: %#v", len(locations), locations)
	}

	location := locations[0]
	if string(location.URI) != wantURI {
		t.Fatalf("expected definition URI %q, got %q", wantURI, location.URI)
	}
	if location.Range.Start != wantPos {
		t.Fatalf("expected definition start %+v, got %+v", wantPos, location.Range.Start)
	}
}
