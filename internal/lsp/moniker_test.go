package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestMonikerReturnsExportedTypeMoniker(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64
`
	path := filepath.Join(dir, "moniker-export.ridl")
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

	monikers, err := srv.Moniker(ctx, &protocol.MonikerParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAt(t, content, "User\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(monikers) != 1 {
		t.Fatalf("expected 1 moniker, got %#v", monikers)
	}

	got := monikers[0]
	if got.Scheme != monikerScheme {
		t.Fatalf("unexpected moniker scheme %q", got.Scheme)
	}
	if got.Identifier != "type:moniker-export.ridl#User" {
		t.Fatalf("unexpected moniker identifier %q", got.Identifier)
	}
	if got.Unique != protocol.UniquenessLevelProject {
		t.Fatalf("unexpected moniker uniqueness %q", got.Unique)
	}
	if got.Kind != protocol.MonikerKindExport {
		t.Fatalf("unexpected moniker kind %q", got.Kind)
	}
}

func TestMonikerReturnsImportedTypeMoniker(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`
	mainPath := filepath.Join(dir, "moniker-import.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
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

	monikers, err := srv.Moniker(ctx, &protocol.MonikerParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
			Position:     positionAt(t, mainContent, "User)"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(monikers) != 1 {
		t.Fatalf("expected 1 moniker, got %#v", monikers)
	}

	got := monikers[0]
	if got.Identifier != "type:types.ridl#User" {
		t.Fatalf("unexpected imported moniker identifier %q", got.Identifier)
	}
	if got.Kind != protocol.MonikerKindImport {
		t.Fatalf("unexpected imported moniker kind %q", got.Kind)
	}
}
