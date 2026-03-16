package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestLinkedEditingRangeReturnsSameFileTypeRanges(t *testing.T) {
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
	path := filepath.Join(dir, "linked-editing-type.ridl")
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

	ranges, err := srv.LinkedEditingRange(ctx, &protocol.LinkedEditingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "struct "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ranges == nil {
		t.Fatal("expected linked editing ranges")
	}
	if ranges.WordPattern != ridlIdentifierWordPattern {
		t.Fatalf("unexpected word pattern %q", ranges.WordPattern)
	}

	assertLinkedEditingStarts(t, ranges.Ranges,
		positionAfter(t, content, "struct "),
		positionAfter(t, content, "(user: "),
		positionAfter(t, content, "[]"),
	)
}

func TestLinkedEditingRangeReturnsSameFileErrorRanges(t *testing.T) {
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
	path := filepath.Join(dir, "linked-editing-error.ridl")
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

	ranges, err := srv.LinkedEditingRange(ctx, &protocol.LinkedEditingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "error 1 "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ranges == nil {
		t.Fatal("expected linked editing ranges")
	}

	assertLinkedEditingStarts(t, ranges.Ranges,
		positionAfter(t, content, "error 1 "),
		positionAfter(t, content, "errors "),
	)
	assertNoLinkedEditingStartAt(t, ranges.Ranges, positionAfter(t, content, "error 2 "))
}

func TestLinkedEditingRangeSkipsImportedDefinitions(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - ./types.ridl

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

	ranges, err := srv.LinkedEditingRange(ctx, &protocol.LinkedEditingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
			Position:     positionAfter(t, mainContent, "(user: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ranges != nil {
		t.Fatalf("expected no linked editing ranges for imported definition, got %#v", ranges)
	}
}

func assertLinkedEditingStarts(t *testing.T, ranges []protocol.Range, want ...protocol.Position) {
	t.Helper()

	if len(ranges) != len(want) {
		t.Fatalf("expected %d linked editing ranges, got %d: %#v", len(want), len(ranges), ranges)
	}

	actual := make(map[string]struct{}, len(ranges))
	for _, rng := range ranges {
		actual[referenceStartKey("", rng.Start)] = struct{}{}
	}

	for _, pos := range want {
		key := referenceStartKey("", pos)
		if _, ok := actual[key]; !ok {
			t.Fatalf("missing linked editing range at %s in %#v", key, ranges)
		}
	}
}

func assertNoLinkedEditingStartAt(t *testing.T, ranges []protocol.Range, pos protocol.Position) {
	t.Helper()

	key := referenceStartKey("", pos)
	for _, rng := range ranges {
		if referenceStartKey("", rng.Start) == key {
			t.Fatalf("unexpected linked editing range at %s in %#v", key, ranges)
		}
	}
}
