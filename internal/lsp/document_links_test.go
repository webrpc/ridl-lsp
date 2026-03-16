package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestDocumentLinkReturnsLinkForClosedImportedFile(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl
`
	mainPath := filepath.Join(dir, "main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}

	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte("webrpc = v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(mainPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    mainContent,
			Version: 1,
		},
	})

	links, err := srv.DocumentLink(ctx, &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertDocumentLink(t, links, positionAt(t, mainContent, "types.ridl"), fileURI(typesPath))
}

func TestDocumentLinkResolvesNestedRelativeImport(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sharedDir := filepath.Join(dir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - ../shared/types.ridl
`
	mainPath := filepath.Join(apiDir, "main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}

	typesPath := filepath.Join(sharedDir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte("webrpc = v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(mainPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    mainContent,
			Version: 1,
		},
	})

	links, err := srv.DocumentLink(ctx, &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertDocumentLink(t, links, positionAt(t, mainContent, "../shared/types.ridl"), fileURI(typesPath))
}

func TestDocumentLinkSkipsMissingImportsAndLinksOverlayTargets(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - missing.ridl
  - overlay.ridl
`
	mainPath := filepath.Join(dir, "main-overlay.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}

	overlayPath := filepath.Join(dir, "overlay.ridl")
	overlayURI := fileURI(overlayPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(overlayURI),
			Text:    "webrpc = v1\n",
			Version: 1,
		},
	})

	mainURI := fileURI(mainPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(mainURI),
			Text:    mainContent,
			Version: 1,
		},
	})

	links, err := srv.DocumentLink(ctx, &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(links) != 1 {
		t.Fatalf("expected 1 document link, got %d: %#v", len(links), links)
	}

	assertDocumentLink(t, links, positionAt(t, mainContent, "overlay.ridl"), overlayURI)
}

func TestDocumentLinkResolveReturnsResolvedLink(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl
`
	mainPath := filepath.Join(dir, "main-resolve.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatal(err)
	}

	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte("webrpc = v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mainURI := fileURI(mainPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(mainURI),
			Text:    mainContent,
			Version: 1,
		},
	})

	links, err := srv.DocumentLink(ctx, &protocol.DocumentLinkParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 document link, got %#v", links)
	}

	resolved, err := srv.DocumentLinkResolve(ctx, &links[0])
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil {
		t.Fatal("expected resolved document link")
	}
	if string(resolved.Target) != fileURI(typesPath) {
		t.Fatalf("document link target = %q, want %q", resolved.Target, fileURI(typesPath))
	}
	if resolved.Tooltip != "Open imported RIDL file" {
		t.Fatalf("unexpected document link tooltip %q", resolved.Tooltip)
	}
}

func assertDocumentLink(t *testing.T, links []protocol.DocumentLink, wantPos protocol.Position, wantTarget string) {
	t.Helper()

	for _, link := range links {
		if link.Range.Start != wantPos {
			continue
		}
		if string(link.Target) != wantTarget {
			t.Fatalf("document link target = %q, want %q", link.Target, wantTarget)
		}
		if link.Tooltip == "" {
			t.Fatal("expected document link tooltip")
		}
		return
	}

	t.Fatalf("missing document link at %+v in %#v", wantPos, links)
}
