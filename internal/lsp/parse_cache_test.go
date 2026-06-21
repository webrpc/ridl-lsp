package lsp

import (
	"testing"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

func TestParseCacheGetPutGenSemantics(t *testing.T) {
	c := newParseCache()
	r := &ridl.ParseResult{}

	// Miss on empty.
	if _, ok := c.get("a.ridl", 1); ok {
		t.Fatal("expected miss on empty cache")
	}
	// Put + hit at same gen.
	c.put("a.ridl", 1, r)
	if got, ok := c.get("a.ridl", 1); !ok || got != r {
		t.Fatal("expected hit at gen 1")
	}
	// Gen mismatch is a miss (whole-gen invalidation).
	if _, ok := c.get("a.ridl", 2); ok {
		t.Fatal("expected miss at newer gen")
	}
	// Roll forward: put at gen 2 drops gen-1 entries.
	c.put("b.ridl", 2, r)
	if _, ok := c.get("a.ridl", 2); ok {
		t.Fatal("expected gen-1 entry dropped after roll-forward")
	}
	if got, ok := c.get("b.ridl", 2); !ok || got != r {
		t.Fatal("expected hit for gen-2 entry")
	}
	// Stale put (older gen) is ignored.
	c.put("c.ridl", 1, r)
	if _, ok := c.get("c.ridl", 1); ok {
		t.Fatal("expected stale put to be ignored")
	}
	// Nil result is never stored.
	c.put("d.ridl", 2, nil)
	if _, ok := c.get("d.ridl", 2); ok {
		t.Fatal("expected nil result not stored")
	}
}
