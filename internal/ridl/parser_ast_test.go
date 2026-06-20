package ridl

import (
	"context"
	"path/filepath"
	"testing"
)

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
}
