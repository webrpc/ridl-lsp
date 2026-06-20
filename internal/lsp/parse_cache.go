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
