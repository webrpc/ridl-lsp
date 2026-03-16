package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestDocumentHighlightFindsSameFileTypeUsages(t *testing.T) {
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
	path := filepath.Join(dir, "document-highlight-type.ridl")
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

	highlights, err := srv.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "struct "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertDocumentHighlights(t, highlights,
		documentHighlightExpectation{pos: positionAfter(t, content, "struct "), kind: protocol.DocumentHighlightKindWrite},
		documentHighlightExpectation{pos: positionAfter(t, content, "(user: "), kind: protocol.DocumentHighlightKindRead},
		documentHighlightExpectation{pos: positionAfter(t, content, "[]"), kind: protocol.DocumentHighlightKindRead},
	)
}

func TestDocumentHighlightFindsImportedTypeUsagesInCurrentDocument(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - ./types.ridl

service TestService
  - GetUser() => (user: User)
  - ListUsers() => (users: []User)
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

	highlights, err := srv.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
			Position:     positionAfter(t, mainContent, "(user: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertDocumentHighlights(t, highlights,
		documentHighlightExpectation{pos: positionAfter(t, mainContent, "(user: "), kind: protocol.DocumentHighlightKindRead},
		documentHighlightExpectation{pos: positionAfter(t, mainContent, "[]"), kind: protocol.DocumentHighlightKindRead},
	)
}

func TestDocumentHighlightAvoidsSubstringMatchesForErrors(t *testing.T) {
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
	path := filepath.Join(dir, "document-highlight-errors.ridl")
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

	highlights, err := srv.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "error 1 "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertDocumentHighlights(t, highlights,
		documentHighlightExpectation{pos: positionAfter(t, content, "error 1 "), kind: protocol.DocumentHighlightKindWrite},
		documentHighlightExpectation{pos: positionAfter(t, content, "errors "), kind: protocol.DocumentHighlightKindRead},
	)
	assertNoDocumentHighlightAt(t, highlights, positionAfter(t, content, "error 2 "))
}

type documentHighlightExpectation struct {
	pos  protocol.Position
	kind protocol.DocumentHighlightKind
}

func assertDocumentHighlights(t *testing.T, highlights []protocol.DocumentHighlight, want ...documentHighlightExpectation) {
	t.Helper()

	if len(highlights) != len(want) {
		t.Fatalf("expected %d highlights, got %d: %#v", len(want), len(highlights), highlights)
	}

	actual := make(map[string]protocol.DocumentHighlightKind, len(highlights))
	for _, highlight := range highlights {
		actual[documentHighlightKey(highlight.Range.Start)] = highlight.Kind
	}

	for _, expected := range want {
		key := documentHighlightKey(expected.pos)
		kind, ok := actual[key]
		if !ok {
			t.Fatalf("missing highlight %s in %#v", key, highlights)
		}
		if kind != expected.kind {
			t.Fatalf("highlight %s kind = %v, want %v", key, kind, expected.kind)
		}
	}
}

func assertNoDocumentHighlightAt(t *testing.T, highlights []protocol.DocumentHighlight, pos protocol.Position) {
	t.Helper()

	key := documentHighlightKey(pos)
	for _, highlight := range highlights {
		if documentHighlightKey(highlight.Range.Start) == key {
			t.Fatalf("unexpected highlight at %s in %#v", key, highlights)
		}
	}
}

func documentHighlightKey(pos protocol.Position) string {
	return fmt.Sprintf("%d:%d", pos.Line, pos.Character)
}
