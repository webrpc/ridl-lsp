package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const baseRIDLContent = `webrpc = v1

name = cachetest
version = v0.1.0

struct User
  - id: uint64
`

func TestParsePathCachesClosedFile(t *testing.T) {
	srv, _, dir := setupServer(t)
	srv.cacheEnabled.Store(true)

	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseRIDLContent), 0o644); err != nil {
		t.Fatal(err)
	}

	r1 := srv.parsePath(context.Background(), basePath)
	if r1 == nil {
		t.Fatal("expected non-nil result from first parse")
	}
	if r1.Root == nil {
		t.Fatal("expected non-nil Root from first parse")
	}

	r2 := srv.parsePath(context.Background(), basePath)
	if r2 == nil {
		t.Fatal("expected non-nil result from second parse")
	}
	// pointer identity proves cache hit
	if r1 != r2 {
		t.Fatal("expected cache hit: r1 and r2 should be the same pointer")
	}
}

func TestParsePathReparsesAfterGenBump(t *testing.T) {
	srv, _, dir := setupServer(t)
	srv.cacheEnabled.Store(true)

	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseRIDLContent), 0o644); err != nil {
		t.Fatal(err)
	}

	r1 := srv.parsePath(context.Background(), basePath)
	if r1 == nil {
		t.Fatal("expected non-nil result from first parse")
	}

	// r2 must hit the cache — same pointer proves the entry was populated.
	r2 := srv.parsePath(context.Background(), basePath)
	if r2 != r1 {
		t.Fatal("expected cache hit before gen bump: r2 should be the same pointer as r1")
	}

	srv.gen.Add(1)

	r3 := srv.parsePath(context.Background(), basePath)
	// gen bump must drop the cache → fresh non-nil pointer
	if r3 == nil {
		t.Fatal("expected non-nil result after gen bump")
	}
	if r3 == r1 {
		t.Fatal("expected fresh parse after gen bump: r3 should differ from r1")
	}
}

func TestParsePathDoesNotCacheCanceledParse(t *testing.T) {
	srv, _, dir := setupServer(t)
	srv.cacheEnabled.Store(true)

	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseRIDLContent), 0o644); err != nil {
		t.Fatal(err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	got := srv.parsePath(canceledCtx, basePath)
	if got != nil {
		t.Fatal("expected nil result for cancelled context")
	}

	// cache must NOT be poisoned by the cancelled attempt
	r := srv.parsePath(context.Background(), basePath)
	if r == nil {
		t.Fatal("expected non-nil result after cancelled attempt: cache should not be poisoned")
	}
}

func TestParsePathBypassesCacheWhenDisabled(t *testing.T) {
	srv, _, dir := setupServer(t)
	// cacheEnabled is false by default — do not enable it

	basePath := filepath.Join(dir, "base.ridl")
	if err := os.WriteFile(basePath, []byte(baseRIDLContent), 0o644); err != nil {
		t.Fatal(err)
	}

	r := srv.parsePath(context.Background(), basePath)
	if r == nil {
		t.Fatal("expected non-nil result when cache disabled")
	}
	if r.Root == nil {
		t.Fatal("expected non-nil Root when cache disabled")
	}
}
