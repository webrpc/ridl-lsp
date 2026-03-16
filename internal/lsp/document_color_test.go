package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestDocumentColorReturnsNoColorsForRIDL(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0
`
	path := filepath.Join(dir, "document-color.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	colors, err := srv.DocumentColor(ctx, &protocol.DocumentColorParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(colors) != 0 {
		t.Fatalf("expected no RIDL document colors, got %#v", colors)
	}
}

func TestColorPresentationReturnsNoPresentationsForRIDL(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0
`
	path := filepath.Join(dir, "color-presentation.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	presentations, err := srv.ColorPresentation(ctx, &protocol.ColorPresentationParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Color:        protocol.Color{Red: 1, Green: 0, Blue: 0, Alpha: 1},
		Range:        protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(presentations) != 0 {
		t.Fatalf("expected no RIDL color presentations, got %#v", presentations)
	}
}
