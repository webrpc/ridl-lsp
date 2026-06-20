package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCandidatePathCacheGenSemantics unit-tests get/put semantics directly on
// candidatePathCache, mirroring the parseCache test.
func TestCandidatePathCacheGenSemantics(t *testing.T) {
	c := newCandidatePathCache()
	paths := []string{"a.ridl", "b.ridl"}

	// Miss on empty cache.
	if _, ok := c.get(1); ok {
		t.Fatal("expected miss on empty cache")
	}

	// Put + hit at same gen.
	c.put(1, paths)
	got, ok := c.get(1)
	if !ok {
		t.Fatal("expected hit at gen 1")
	}
	if len(got) != len(paths) {
		t.Fatalf("expected %d paths, got %d", len(paths), len(got))
	}

	// Gen mismatch is a miss.
	if _, ok := c.get(2); ok {
		t.Fatal("expected miss at newer gen")
	}

	// Roll forward: put at gen 2 replaces gen 1.
	newPaths := []string{"c.ridl"}
	c.put(2, newPaths)
	if _, ok := c.get(1); ok {
		t.Fatal("expected gen-1 entry dropped after roll-forward")
	}
	if got2, ok := c.get(2); !ok || len(got2) != 1 {
		t.Fatal("expected gen-2 entry present")
	}

	// Stale put (older gen) is ignored.
	c.put(1, paths)
	if _, ok := c.get(1); ok {
		t.Fatal("expected stale put at gen 1 to be ignored")
	}
	// Gen-2 entry must be unchanged after stale put.
	if _, ok := c.get(2); !ok {
		t.Fatal("expected gen-2 entry to survive stale put")
	}

	// Empty-but-valid slice at gen 3 is distinguishable from "not cached".
	c.put(3, []string{})
	emptyPaths, ok := c.get(3)
	if !ok {
		t.Fatal("expected hit for empty-but-valid entry at gen 3")
	}
	if emptyPaths == nil {
		t.Fatal("expected non-nil (but empty) slice for empty-but-valid entry")
	}
}

const candidateRIDL = `webrpc = v1

name = candidatetest
version = v0.1.0

struct Foo
  - id: uint64
`

// TestCandidatePathsCachedAtStableGen verifies that:
//  1. A second call at the same gen returns the cached list (new on-disk file
//     created between the two calls is NOT visible).
//  2. After srv.gen.Add(1) a fresh walk occurs and the new file IS present.
func TestCandidatePathsCachedAtStableGen(t *testing.T) {
	srv, _, dir := setupServer(t)
	srv.cacheEnabled.Store(true)

	// Seed one open document so the workspace is non-empty.
	seedPath := filepath.Join(dir, "seed.ridl")
	if err := os.WriteFile(seedPath, []byte(candidateRIDL), 0o644); err != nil {
		t.Fatal(err)
	}

	// First call: populates the candidate-path cache.
	r1 := srv.referenceCandidatePaths()

	// Write a NEW .ridl file directly to disk — no DidOpen, no watcher event,
	// so the gen does not advance.
	newPath := filepath.Join(dir, "new_file.ridl")
	if err := os.WriteFile(newPath, []byte(candidateRIDL), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second call at same gen: must return cached list, new file must NOT appear.
	r2 := srv.referenceCandidatePaths()
	for _, p := range r2 {
		if p == newPath {
			t.Fatal("new_file.ridl must not appear in cached result at same gen")
		}
	}

	// Ensure the lists agree in length (same gen → same cached object).
	if len(r2) != len(r1) {
		t.Fatalf("expected same length on cache hit: r1=%d r2=%d", len(r1), len(r2))
	}

	// Bump the gen — next call must re-walk and include the new file.
	srv.gen.Add(1)
	r3 := srv.referenceCandidatePaths()

	found := false
	for _, p := range r3 {
		if p == newPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("new_file.ridl must appear after gen bump; got: %v", r3)
	}
}
