package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"go.lsp.dev/protocol"
	"go.uber.org/zap"
)

const baseRIDL = `webrpc = v1

name = test
version = v0.0.1

struct User
  - id: uint64
`

// TestNewRequestParseMemoizesPerPath asserts that calling the request-scoped
// parse function twice with the same path returns the identical *ParseResult
// pointer, proving the file is parsed at most once per request.
func TestNewRequestParseMemoizesPerPath(t *testing.T) {
	srv, _, dir := setupServer(t)

	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseRIDL), 0o644); err != nil {
		t.Fatal(err)
	}

	parse := srv.newRequestParse()

	r1 := parse(basePath)
	if r1 == nil {
		t.Fatal("expected non-nil ParseResult, got nil — test would be vacuous")
	}
	if r1.Root == nil {
		t.Fatal("expected ParseResult.Root != nil, got nil — test would be vacuous")
	}

	r2 := parse(basePath)
	if r1 != r2 {
		t.Errorf("parse returned different pointers for the same path — file was parsed more than once")
	}
}

// TestNewRequestParseKeyedByCleanPath asserts that path variants that resolve
// to the same cleaned path (e.g. "dir/./file.ridl" vs "dir/file.ridl") share
// the same memo slot and produce the identical *ParseResult pointer.
func TestNewRequestParseKeyedByCleanPath(t *testing.T) {
	srv, _, dir := setupServer(t)

	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseRIDL), 0o644); err != nil {
		t.Fatal(err)
	}

	parse := srv.newRequestParse()

	// Two string paths that differ only by a redundant "." segment.
	p1 := dir + "/base.ridl"
	p2 := dir + "/./base.ridl" // deliberately un-cleaned via string concat

	r1 := parse(p1)
	if r1 == nil {
		t.Fatal("expected non-nil ParseResult for p1, got nil — test would be vacuous")
	}

	r2 := parse(p2)
	if r1 != r2 {
		t.Errorf("different un-cleaned paths that resolve to the same file returned different ParseResult pointers — clean-path keying is broken")
	}
}

// BenchmarkCodeLensEager measures the cost of a single CodeLens request
// against a workspace of 40 importer files all referencing two symbols in
// a shared base file. Run with -bench BenchmarkCodeLensEager -run x.
func BenchmarkCodeLensEager(b *testing.B) {
	// Replicate setupServer body for *testing.B (setupServer only accepts *testing.T).
	dir := b.TempDir()
	client := newMockClient()
	srv := NewServer(zap.NewNop())
	srv.SetClient(client)
	srv.workspace.SetRoot(dir)

	baseContent := `webrpc = v1

name = base
version = v0.0.1

struct User
  - id: uint64

struct Account
  - userId: uint64
`
	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseContent), 0o644); err != nil {
		b.Fatal(err)
	}

	// Write 40 importer files each referencing User and Account from base.
	for i := 0; i < 40; i++ {
		content := `webrpc = v1

name = f` + strconv.Itoa(i) + `
version = v0.0.1

import
  - base.ridl

struct S` + strconv.Itoa(i) + `
  - u: User
  - a: Account
`
		p := filepath.Join(dir, "f"+strconv.Itoa(i)+".ridl")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	// DidOpen base.ridl so it is the target document.
	baseURI := string(PathToURI(basePath))
	ctx := context.Background()
	if err := srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(baseURI),
			Text:    baseContent,
			Version: 1,
		},
	}); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := srv.CodeLens(ctx, &protocol.CodeLensParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(baseURI)},
		}); err != nil {
			b.Fatal(err)
		}
	}
}
