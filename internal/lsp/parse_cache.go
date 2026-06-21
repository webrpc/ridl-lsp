package lsp

import (
	"sync"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

// parseCache holds parse results for CLOSED workspace files belonging to a single
// workspace generation. Any generation change drops the whole map, because a
// closed file's parse depends on the overlay+disk state of its entire import graph.
// Cached *ridl.ParseResult values (Root and Schema) are READ-ONLY.
type parseCache struct {
	mu      sync.Mutex
	gen     uint64
	entries map[string]*ridl.ParseResult
}

// candidatePathCache holds the last WalkDir result for a single workspace
// generation. The cached slice is READ-ONLY — callers must not mutate it.
// A valid=true entry at gen=0 is distinguishable from "never cached".
type candidatePathCache struct {
	mu    sync.Mutex
	gen   uint64
	paths []string
	valid bool
}

func newCandidatePathCache() *candidatePathCache {
	return &candidatePathCache{}
}

// get returns the cached path list if it was stored at exactly gen, otherwise false.
func (c *candidatePathCache) get(gen uint64) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.valid || c.gen != gen {
		return nil, false
	}
	return c.paths, true
}

// put stores paths at gen. Stale puts (gen < c.gen) are silently dropped so a
// concurrent walk that finishes late never overwrites a newer cache entry.
func (c *candidatePathCache) put(gen uint64, paths []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen < c.gen {
		return
	}
	c.gen = gen
	c.paths = paths
	c.valid = true
}

func newParseCache() *parseCache {
	return &parseCache{entries: map[string]*ridl.ParseResult{}}
}

func (c *parseCache) get(path string, gen uint64) (*ridl.ParseResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return nil, false
	}
	result, ok := c.entries[path]
	return result, ok
}

func (c *parseCache) put(path string, gen uint64, result *ridl.ParseResult) {
	if result == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen < c.gen {
		return
	}
	if gen > c.gen {
		c.entries = map[string]*ridl.ParseResult{}
		c.gen = gen
	}
	c.entries[path] = result
}
