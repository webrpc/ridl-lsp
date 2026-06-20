package ridl

import (
	"context"
	"path/filepath"
	"testing"
)

// multiDeclRIDL is a representative file that contains structs, an enum, and an
// error — enough surface area to catch Root divergence between Parse and ParseAST.
const multiDeclRIDL = `webrpc = v1

name = multidecl
version = v0.0.1

struct User
  - id: uint64
  - name: string

struct Account
  - userId: uint64

enum Status: uint32
  - Active = 1
  - Inactive = 2

error 1 NotFound "not found" HTTP 404
`

// TestParseASTRootEquivalentToParse asserts that ParseAST and Parse produce
// structurally identical Root nodes for a valid multi-declaration file: same
// struct/enum/error counts and matching name strings. Schema divergence is
// expected and intentional — only Root is compared.
func TestParseASTRootEquivalentToParse(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "multi.ridl")
	writeTestFile(t, p, multiDeclRIDL)

	ctx := context.Background()
	full, err := NewParser().Parse(ctx, dir, p, nil)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if full.Root == nil {
		t.Fatal("Parse returned nil Root")
	}

	ast, err := NewParser().ParseAST(ctx, dir, p, nil)
	if err != nil {
		t.Fatalf("ParseAST error: %v", err)
	}
	if ast.Root == nil {
		t.Fatal("ParseAST returned nil Root")
	}

	if got, want := len(ast.Root.Structs()), len(full.Root.Structs()); got != want {
		t.Errorf("Structs count: ParseAST=%d, Parse=%d", got, want)
	}
	if got, want := len(ast.Root.Enums()), len(full.Root.Enums()); got != want {
		t.Errorf("Enums count: ParseAST=%d, Parse=%d", got, want)
	}
	if got, want := len(ast.Root.Errors()), len(full.Root.Errors()); got != want {
		t.Errorf("Errors count: ParseAST=%d, Parse=%d", got, want)
	}

	for i, s := range full.Root.Structs() {
		if i >= len(ast.Root.Structs()) {
			break
		}
		if got, want := ast.Root.Structs()[i].Name().String(), s.Name().String(); got != want {
			t.Errorf("Struct[%d] name: ParseAST=%q, Parse=%q", i, got, want)
		}
	}
	for i, e := range full.Root.Enums() {
		if i >= len(ast.Root.Enums()) {
			break
		}
		if got, want := ast.Root.Enums()[i].Name().String(), e.Name().String(); got != want {
			t.Errorf("Enum[%d] name: ParseAST=%q, Parse=%q", i, got, want)
		}
	}
	for i, e := range full.Root.Errors() {
		if i >= len(ast.Root.Errors()) {
			break
		}
		if got, want := ast.Root.Errors()[i].Name().String(), e.Name().String(); got != want {
			t.Errorf("Error[%d] name: ParseAST=%q, Parse=%q", i, got, want)
		}
	}
}

func TestParseASTReturnsRootAndEmptySchema(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.ridl")
	writeTestFile(t, p, "webrpc = v1\n\nname = test\nversion = v0.0.1\n\nstruct User\n  - id: uint64\n")

	result, err := NewParser().ParseAST(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatalf("ParseAST error: %v", err)
	}
	if result == nil || result.Root == nil {
		t.Fatal("expected non-nil Root")
	}
	if result.Schema == nil {
		t.Fatal("expected non-nil empty Schema so semanticDocument.valid() passes")
	}
	if len(result.Root.Structs()) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(result.Root.Structs()))
	}
}

func TestParseASTSkipsImportResolution(t *testing.T) {
	dir := t.TempDir()
	// Imports a file that does NOT exist on disk. Full Parse would chase it;
	// ParseAST must not, and must still return this file's Root.
	p := filepath.Join(dir, "a.ridl")
	writeTestFile(t, p, "webrpc = v1\n\nname = test\nversion = v0.0.1\n\nimport\n  - missing.ridl\n\nstruct User\n  - id: uint64\n")

	result, err := NewParser().ParseAST(context.Background(), dir, p, nil)
	if err != nil {
		t.Fatalf("ParseAST error: %v", err)
	}
	if result == nil || result.Root == nil || len(result.Root.Structs()) != 1 {
		t.Fatal("expected this file's Root regardless of missing import")
	}
	if result.Schema == nil {
		t.Fatal("expected non-nil Schema")
	}
}
