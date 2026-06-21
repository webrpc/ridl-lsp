package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.lsp.dev/protocol"
)

// TestCacheConcurrentAccess exercises the parse cache under concurrent handler
// calls and concurrent DidChange mutations. The test asserts no data race by
// passing under `go test -race`.
func TestCacheConcurrentAccess(t *testing.T) {
	srv, _, dir := setupServer(t)
	srv.cacheEnabled.Store(true)

	// Build a small workspace: base.ridl defines a struct/error; two importers
	// reference it. All three are written to disk so parsePath can read them.
	baseContent := `webrpc = v1

name = racetest
version = v0.0.1

struct Point
  - x: int32
  - y: int32

error 100 BadInput "bad input" HTTP 400
`
	importer1Content := `webrpc = v1

name = importer1
version = v0.0.1

import
  - path = base.ridl
`
	importer2Content := `webrpc = v1

name = importer2
version = v0.0.1

import
  - path = base.ridl
`

	basePath := filepath.Join(dir, "base.ridl")
	imp1Path := filepath.Join(dir, "importer1.ridl")
	imp2Path := filepath.Join(dir, "importer2.ridl")
	for path, content := range map[string]string{
		basePath: baseContent,
		imp1Path: importer1Content,
		imp2Path: importer2Content,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Open base.ridl so DidChange has a registered document to churn.
	baseURI := protocol.DocumentURI(fileURI(basePath))
	ctx := context.Background()
	if err := srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     baseURI,
			Text:    baseContent,
			Version: 1,
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Put the cursor on the Point type definition so References runs a real
	// cross-file search, walking the CLOSED importer files through
	// parsePathForNavigation -> parsePath -> the session parse cache. A header
	// position resolves no target and would never reach the cache.
	pointPos := positionAt(t, baseContent, "Point")

	// Precondition: a closed-file parse must populate the session cache, else the
	// concurrent loop below would not exercise the cache path it claims to test.
	if got := srv.parsePath(ctx, imp1Path); got == nil || got.Root == nil {
		t.Fatal("precondition: closed importer parse must succeed")
	}
	if _, ok := srv.parseCache.get(imp1Path, srv.gen.Load()); !ok {
		t.Fatal("precondition: session parse cache was not populated by a closed-file parse")
	}

	const (
		numReaders  = 16
		numChangers = 2
		iters       = 50
	)

	var wg sync.WaitGroup

	// Reader goroutines call a mix of read-path handlers.
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				pos := pointPos
				switch id % 4 {
				case 0:
					_, _ = srv.References(ctx, &protocol.ReferenceParams{
						TextDocumentPositionParams: protocol.TextDocumentPositionParams{
							TextDocument: protocol.TextDocumentIdentifier{URI: baseURI},
							Position:     pos,
						},
					})
				case 1:
					_, _ = srv.Definition(ctx, &protocol.DefinitionParams{
						TextDocumentPositionParams: protocol.TextDocumentPositionParams{
							TextDocument: protocol.TextDocumentIdentifier{URI: baseURI},
							Position:     pos,
						},
					})
				case 2:
					_, _ = srv.Hover(ctx, &protocol.HoverParams{
						TextDocumentPositionParams: protocol.TextDocumentPositionParams{
							TextDocument: protocol.TextDocumentIdentifier{URI: baseURI},
							Position:     pos,
						},
					})
				case 3:
					_, _ = srv.CodeLens(ctx, &protocol.CodeLensParams{
						TextDocument: protocol.TextDocumentIdentifier{URI: baseURI},
					})
				}
			}
		}(i)
	}

	// Changer goroutines fire DidChange to churn the generation counter.
	for i := 0; i < numChangers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				ver := int32(2 + id*iters + j)
				_ = srv.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
					TextDocument: protocol.VersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: baseURI},
						Version:                ver,
					},
					ContentChanges: []protocol.TextDocumentContentChangeEvent{
						{Text: fmt.Sprintf("%s\n# churn %d\n", baseContent, j)},
					},
				})
			}
		}(i)
	}

	wg.Wait()
	// No assertion needed: a data race would be caught by the -race detector.
}
