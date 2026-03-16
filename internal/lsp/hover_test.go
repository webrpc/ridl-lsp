package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestHoverReturnsMethodSignature(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - GetUser(id: uint64) => (user: User)
`
	path := filepath.Join(dir, "hover-method.ridl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
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

	hover, err := srv.Hover(ctx, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAt(t, content, "GetUser"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hover == nil {
		t.Fatal("expected hover response for method name")
	}
	if !strings.Contains(hover.Contents.Value, "GetUser(id: uint64) => (user: User)") {
		t.Fatalf("expected method signature in hover, got %q", hover.Contents.Value)
	}
}

func TestHoverResolvesImportedType(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser(id: uint64) => (user: User)
`
	mainPath := filepath.Join(dir, "hover-main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	typesContent := `webrpc = v1

struct User
  - id: uint64
  - name: string
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
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

	typesURI := fileURI(typesPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(typesURI),
			Text:    typesContent,
			Version: 1,
		},
	})

	hover, err := srv.Hover(ctx, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
			Position:     positionAtOccurrence(t, mainContent, "User", 1),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hover == nil {
		t.Fatal("expected hover response for imported type")
	}
	if !strings.Contains(hover.Contents.Value, "struct User") {
		t.Fatalf("expected imported type in hover, got %q", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "name: string") {
		t.Fatalf("expected imported type fields in hover, got %q", hover.Contents.Value)
	}
}

func positionAt(t *testing.T, content, needle string) protocol.Position {
	t.Helper()
	return positionAtOccurrence(t, content, needle, 0)
}

func positionAtOccurrence(t *testing.T, content, needle string, occurrence int) protocol.Position {
	t.Helper()

	searchFrom := 0
	index := -1
	for i := 0; i <= occurrence; i++ {
		next := strings.Index(content[searchFrom:], needle)
		if next < 0 {
			t.Fatalf("could not find occurrence %d of %q", occurrence, needle)
		}
		index = searchFrom + next
		searchFrom = index + len(needle)
	}

	line := 0
	character := 0
	for _, r := range content[:index] {
		if r == '\n' {
			line++
			character = 0
			continue
		}
		character++
	}

	return protocol.Position{
		Line:      uint32(line),
		Character: uint32(character),
	}
}
