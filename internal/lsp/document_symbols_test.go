package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestDocumentSymbolsReturnTopLevelOutlineInSourceOrder(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

service AccountService
  - Ping(id: uint64) => (ok: bool)

struct User
  - id: uint64

enum Kind: uint32
  - ADMIN = 1

error 100 UserNotFound "user not found" HTTP 404
`
	path := filepath.Join(dir, "document-symbols-order.ridl")
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

	got, err := srv.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	symbols := requireDocumentSymbols(t, got)
	assertSymbolNames(t, symbols, "AccountService", "User", "Kind", "UserNotFound")
	assertSymbolKind(t, symbols[0], protocol.SymbolKindInterface)
	assertSymbolKind(t, symbols[1], protocol.SymbolKindStruct)
	assertSymbolKind(t, symbols[2], protocol.SymbolKindEnum)
	assertSymbolKind(t, symbols[3], protocol.SymbolKindObject)
}

func TestDocumentSymbolsIncludeNestedRIDLDeclarations(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

enum Kind: uint32
  - ADMIN = 1

service AccountService
  - Ping(id: uint64) => (ok: bool)
`
	path := filepath.Join(dir, "document-symbols-children.ridl")
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

	got, err := srv.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	symbols := requireDocumentSymbols(t, got)

	structSymbol := findDocumentSymbol(t, symbols, "User")
	if len(structSymbol.Children) != 1 {
		t.Fatalf("expected struct children, got %#v", structSymbol.Children)
	}
	assertSymbolKind(t, structSymbol.Children[0], protocol.SymbolKindField)
	if structSymbol.Children[0].Name != "id" || structSymbol.Children[0].Detail != "uint64" {
		t.Fatalf("unexpected struct field symbol: %#v", structSymbol.Children[0])
	}

	enumSymbol := findDocumentSymbol(t, symbols, "Kind")
	if len(enumSymbol.Children) != 1 {
		t.Fatalf("expected enum children, got %#v", enumSymbol.Children)
	}
	assertSymbolKind(t, enumSymbol.Children[0], protocol.SymbolKindEnumMember)
	if enumSymbol.Children[0].Name != "ADMIN" || enumSymbol.Children[0].Detail != "= 1" {
		t.Fatalf("unexpected enum member symbol: %#v", enumSymbol.Children[0])
	}

	serviceSymbol := findDocumentSymbol(t, symbols, "AccountService")
	if len(serviceSymbol.Children) != 1 {
		t.Fatalf("expected service children, got %#v", serviceSymbol.Children)
	}
	assertSymbolKind(t, serviceSymbol.Children[0], protocol.SymbolKindMethod)
	if serviceSymbol.Children[0].Name != "Ping" {
		t.Fatalf("unexpected method symbol name: %#v", serviceSymbol.Children[0])
	}
	if serviceSymbol.Children[0].Detail != "Ping(id: uint64) => (ok: bool)" {
		t.Fatalf("unexpected method symbol detail: %#v", serviceSymbol.Children[0])
	}
}

func requireDocumentSymbols(t *testing.T, raw []any) []protocol.DocumentSymbol {
	t.Helper()

	symbols := make([]protocol.DocumentSymbol, 0, len(raw))
	for _, item := range raw {
		symbol, ok := item.(protocol.DocumentSymbol)
		if !ok {
			t.Fatalf("expected protocol.DocumentSymbol, got %T", item)
		}
		symbols = append(symbols, symbol)
	}
	return symbols
}

func assertSymbolNames(t *testing.T, symbols []protocol.DocumentSymbol, want ...string) {
	t.Helper()
	if len(symbols) != len(want) {
		t.Fatalf("expected %d symbols, got %d: %#v", len(want), len(symbols), symbols)
	}
	for i, name := range want {
		if symbols[i].Name != name {
			t.Fatalf("expected symbol %d to be %q, got %q", i, name, symbols[i].Name)
		}
	}
}

func assertSymbolKind(t *testing.T, symbol protocol.DocumentSymbol, want protocol.SymbolKind) {
	t.Helper()
	if symbol.Kind != want {
		t.Fatalf("expected symbol %q kind %s, got %s", symbol.Name, want, symbol.Kind)
	}
}

func findDocumentSymbol(t *testing.T, symbols []protocol.DocumentSymbol, name string) protocol.DocumentSymbol {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.Name == name {
			return symbol
		}
	}
	t.Fatalf("could not find symbol %q in %#v", name, symbols)
	return protocol.DocumentSymbol{}
}
