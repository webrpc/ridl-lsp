package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestSelectionRangeForStructFieldTypeBuildsNestedChain(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct Account
  - id: uint64

struct User
  - friend: []Account
`
	path := filepath.Join(dir, "selection-struct.ridl")
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

	ranges, err := srv.SelectionRange(ctx, &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Positions:    []protocol.Position{positionAtOccurrence(t, content, "Account", 1)},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSelectionRangeTexts(t, content, ranges, 0, "Account", "[]Account", "- friend: []Account", "struct User\n  - friend: []Account")
}

func TestSelectionRangeForMethodArgumentBuildsNestedChain(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service AccountService
  - GetUser(id: uint64) => (user: []User)
`
	path := filepath.Join(dir, "selection-method.ridl")
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

	ranges, err := srv.SelectionRange(ctx, &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Positions:    []protocol.Position{positionAfter(t, content, "[]")},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSelectionRangeTexts(t, content, ranges, 0, "User", "[]User", "user: []User", "GetUser(id: uint64) => (user: []User)", "service AccountService\n  - GetUser(id: uint64) => (user: []User)")
}

func TestSelectionRangeForImportPathBuildsNestedChain(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: string)
`
	path := filepath.Join(dir, "selection-import.ridl")
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

	ranges, err := srv.SelectionRange(ctx, &protocol.SelectionRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Positions:    []protocol.Position{positionAt(t, content, "types.ridl")},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSelectionRangeTexts(t, content, ranges, 0, "types.ridl", "- types.ridl")
}

func assertSelectionRangeTexts(t *testing.T, content string, ranges []protocol.SelectionRange, index int, want ...string) {
	t.Helper()

	if len(ranges) <= index {
		t.Fatalf("missing selection range at index %d in %#v", index, ranges)
	}

	got := selectionRangeTexts(t, content, &ranges[index])
	if len(got) != len(want) {
		t.Fatalf("expected %d selection ranges, got %d: %#v", len(want), len(got), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selection range %d = %q, want %q; full chain: %#v", i, got[i], want[i], got)
		}
	}
}

func selectionRangeTexts(t *testing.T, content string, sel *protocol.SelectionRange) []string {
	t.Helper()

	texts := []string{}
	for current := sel; current != nil; current = current.Parent {
		start := offsetAtPosition(t, content, current.Range.Start)
		end := offsetAtPosition(t, content, current.Range.End)
		texts = append(texts, content[start:end])
	}
	return texts
}
