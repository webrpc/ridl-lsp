package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestReferenceCandidatePathsPrunesHeavyDirs: the workspace scan that backs
// find-references / workspace-symbols / missing-import quick-fixes must not
// descend into .git, node_modules, vendor, or hidden directories. They never
// hold project schemas and can dominate a monorepo scan that runs synchronously
// on the request (audit I7).
func TestReferenceCandidatePathsPrunesHeavyDirs(t *testing.T) {
	srv, _, dir := setupServer(t)

	write := func(rel string) string {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(validRIDL), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	wanted := write("api.ridl")
	write("node_modules/dep/schema.ridl")
	write("vendor/lib/schema.ridl")
	write(".git/hooks/schema.ridl")
	write(".cache/schema.ridl")

	paths := srv.referenceCandidatePaths()

	if !slices.Contains(paths, wanted) {
		t.Fatalf("expected top-level api.ridl in candidates, got %v", paths)
	}
	for _, p := range paths {
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		for _, pruned := range []string{"node_modules/", "vendor/", ".git/", ".cache/"} {
			if len(rel) >= len(pruned) && rel[:len(pruned)] == pruned {
				t.Fatalf("candidate from pruned dir leaked: %q", rel)
			}
		}
	}
}
