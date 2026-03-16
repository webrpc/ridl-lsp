package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestReferencesFindSameFileTypeReferences(t *testing.T) {
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
	path := filepath.Join(dir, "references-same-file.ridl")
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

	locations, err := srv.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "struct "),
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertReferenceStarts(t, locations,
		referenceStart{uri: uri, pos: positionAfter(t, content, "struct ")},
		referenceStart{uri: uri, pos: positionAfter(t, content, "(user: ")},
		referenceStart{uri: uri, pos: positionAfter(t, content, "[]")},
	)

	locations, err = srv.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "struct "),
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertReferenceStarts(t, locations,
		referenceStart{uri: uri, pos: positionAfter(t, content, "(user: ")},
		referenceStart{uri: uri, pos: positionAfter(t, content, "[]")},
	)
}

func TestReferencesFindImportedTypeInClosedWorkspaceFile(t *testing.T) {
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
`
	mainPath := filepath.Join(mainDir, "main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	sharedDir := filepath.Join(dir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(sharedDir, "types.ridl")
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

	locations, err := srv.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(typesURI)},
			Position:     positionAfter(t, typesContent, "struct "),
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertReferenceStarts(t, locations,
		referenceStart{uri: typesURI, pos: positionAfter(t, typesContent, "struct ")},
		referenceStart{uri: fileURI(mainPath), pos: positionAfter(t, mainContent, "(user: ")},
	)
}

func TestReferencesFindErrorReferencesWithoutSubstringMatches(t *testing.T) {
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
	path := filepath.Join(dir, "references-errors.ridl")
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

	locations, err := srv.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "error 1 "),
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertReferenceStarts(t, locations,
		referenceStart{uri: uri, pos: positionAfter(t, content, "error 1 ")},
		referenceStart{uri: uri, pos: positionAfter(t, content, "errors ")},
	)
}

type referenceStart struct {
	uri string
	pos protocol.Position
}

func assertReferenceStarts(t *testing.T, locations []protocol.Location, want ...referenceStart) {
	t.Helper()

	if len(locations) != len(want) {
		t.Fatalf("expected %d references, got %d: %#v", len(want), len(locations), locations)
	}

	actual := make(map[string]struct{}, len(locations))
	for _, location := range locations {
		actual[referenceStartKey(string(location.URI), location.Range.Start)] = struct{}{}
	}

	for _, ref := range want {
		key := referenceStartKey(ref.uri, ref.pos)
		if _, ok := actual[key]; !ok {
			t.Fatalf("missing reference %s in %#v", key, locations)
		}
	}
}

func referenceStartKey(uri string, pos protocol.Position) string {
	return fmt.Sprintf("%s:%d:%d", uri, pos.Line, pos.Character)
}
