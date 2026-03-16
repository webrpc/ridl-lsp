package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestWorkspaceSymbolsIncludeOpenAndClosedWorkspaceFiles(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	openContent := `webrpc = v1

name = openapp
version = v0.1.0

struct User
  - id: uint64
`
	openPath := filepath.Join(dir, "open.ridl")
	if err := os.WriteFile(openPath, []byte(openContent), 0o644); err != nil {
		t.Fatal(err)
	}

	closedContent := `webrpc = v1

name = closedapp
version = v0.1.0

struct UserProfile
  - id: uint64
`
	closedPath := filepath.Join(dir, "closed.ridl")
	if err := os.WriteFile(closedPath, []byte(closedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	openURI := fileURI(openPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(openURI),
			Text:    openContent,
			Version: 1,
		},
	})

	symbols, err := srv.Symbols(ctx, &protocol.WorkspaceSymbolParams{Query: "user"})
	if err != nil {
		t.Fatal(err)
	}

	assertWorkspaceSymbol(t, symbols, "User", protocol.SymbolKindStruct, fileURI(openPath), "")
	assertWorkspaceSymbol(t, symbols, "UserProfile", protocol.SymbolKindStruct, fileURI(closedPath), "")
}

func TestWorkspaceSymbolsIncludeServiceMethodsWithContainerNames(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

service AccountService
  - GetUser() => (user: string)
  - ListUsers() => (users: []string)
`
	path := filepath.Join(dir, "service.ridl")
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

	symbols, err := srv.Symbols(ctx, &protocol.WorkspaceSymbolParams{Query: "getuser"})
	if err != nil {
		t.Fatal(err)
	}

	if len(symbols) != 1 {
		t.Fatalf("expected 1 workspace symbol, got %d: %#v", len(symbols), symbols)
	}

	symbol := symbols[0]
	if symbol.Name != "GetUser" {
		t.Fatalf("unexpected symbol name %q", symbol.Name)
	}
	if symbol.Kind != protocol.SymbolKindMethod {
		t.Fatalf("unexpected symbol kind %v", symbol.Kind)
	}
	if symbol.ContainerName != "AccountService" {
		t.Fatalf("unexpected container name %q", symbol.ContainerName)
	}
	if string(symbol.Location.URI) != uri {
		t.Fatalf("unexpected symbol URI %q", symbol.Location.URI)
	}
}

func TestWorkspaceSymbolsReturnAllDeclarationsForEmptyQuery(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

enum Kind: string
  - ADMIN = "admin"

error 1 UserNotFound "user not found" HTTP 404

service AccountService
  - GetUser() => (user: string)

struct User
  - id: uint64
`
	path := filepath.Join(dir, "all-symbols.ridl")
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

	symbols, err := srv.Symbols(ctx, &protocol.WorkspaceSymbolParams{})
	if err != nil {
		t.Fatal(err)
	}

	assertWorkspaceSymbol(t, symbols, "Kind", protocol.SymbolKindEnum, uri, "")
	assertWorkspaceSymbol(t, symbols, "UserNotFound", protocol.SymbolKindObject, uri, "")
	assertWorkspaceSymbol(t, symbols, "AccountService", protocol.SymbolKindInterface, uri, "")
	assertWorkspaceSymbol(t, symbols, "GetUser", protocol.SymbolKindMethod, uri, "AccountService")
	assertWorkspaceSymbol(t, symbols, "User", protocol.SymbolKindStruct, uri, "")
}

func assertWorkspaceSymbol(t *testing.T, symbols []protocol.SymbolInformation, name string, kind protocol.SymbolKind, uri string, containerName string) {
	t.Helper()

	for _, symbol := range symbols {
		if symbol.Name != name {
			continue
		}
		if symbol.Kind != kind {
			continue
		}
		if string(symbol.Location.URI) != uri {
			continue
		}
		if symbol.ContainerName != containerName {
			continue
		}
		return
	}

	t.Fatalf("missing workspace symbol %q kind=%v uri=%q container=%q in %#v", name, kind, uri, containerName, symbols)
}
