package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestFoldingRangesIncludeCommentsImportsAndDeclarations(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0

# Service docs
# More docs
service TestService
  - GetUser() => (user: User)
  - ListUsers() => (users: []User)

import
  - types.ridl
  - errors.ridl

struct User
  - id: uint64
  - name: string

enum Kind: uint32
  - ADMIN = 1
  - MEMBER = 2
`
	path := filepath.Join(dir, "folding-main.ridl")
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

	ranges, err := srv.FoldingRanges(ctx, &protocol.FoldingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertHasFoldingRange(t, ranges, 4, 5, protocol.CommentFoldingRange)
	assertHasFoldingRange(t, ranges, 6, 8, "")
	assertHasFoldingRange(t, ranges, 10, 12, protocol.ImportsFoldingRange)
	assertHasFoldingRange(t, ranges, 14, 16, "")
	assertHasFoldingRange(t, ranges, 18, 20, "")
	assertFoldingRangesSorted(t, ranges)
}

func TestFoldingRangesSkipSingleLineBlocks(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0

# One line comment
service TestService
  - Ping()

import

struct Empty
`
	path := filepath.Join(dir, "folding-single-line.ridl")
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

	ranges, err := srv.FoldingRanges(ctx, &protocol.FoldingRangeParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(ranges) != 1 {
		t.Fatalf("expected only service folding range, got %#v", ranges)
	}
	assertHasFoldingRange(t, ranges, 5, 6, "")
}

func assertHasFoldingRange(t *testing.T, ranges []protocol.FoldingRange, startLine, endLine uint32, kind protocol.FoldingRangeKind) {
	t.Helper()
	for _, rng := range ranges {
		if rng.StartLine == startLine && rng.EndLine == endLine && rng.Kind == kind {
			return
		}
	}
	t.Fatalf("missing folding range %d-%d kind=%q in %#v", startLine, endLine, kind, ranges)
}

func assertFoldingRangesSorted(t *testing.T, ranges []protocol.FoldingRange) {
	t.Helper()
	for i := 1; i < len(ranges); i++ {
		prev := ranges[i-1]
		curr := ranges[i]
		if curr.StartLine < prev.StartLine || (curr.StartLine == prev.StartLine && curr.EndLine < prev.EndLine) {
			t.Fatalf("folding ranges out of order: %#v before %#v", prev, curr)
		}
	}
}
