package documents

import (
	"testing"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

// TestSetResultDropsStaleVersion is the whole reason SetResult takes a version:
// a parse of older content (a slower request) must not clobber the result of a
// newer change that already superseded it.
func TestSetResultDropsStaleVersion(t *testing.T) {
	s := NewStore()
	s.Set(&Document{URI: "u", Version: 2})

	s.SetResult("u", 1, &ridl.ParseResult{}) // parsed from superseded content
	if doc, _ := s.Get("u"); doc.Result != nil {
		t.Fatal("stale-version result must be dropped")
	}

	current := &ridl.ParseResult{}
	s.SetResult("u", 2, current)
	if doc, _ := s.Get("u"); doc.Result != current {
		t.Fatal("matching-version result must be applied")
	}
}

// TestSetResultMissingDocIsNoop guards the gone-document branch: a result
// arriving after the document closed must neither panic nor resurrect it.
func TestSetResultMissingDocIsNoop(t *testing.T) {
	s := NewStore()
	s.SetResult("missing", 1, &ridl.ParseResult{})
	if _, ok := s.Get("missing"); ok {
		t.Fatal("SetResult must not create a document")
	}
}
