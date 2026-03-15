package lsp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/webrpc/ridl-lsp/internal/workspace"
)

const validRIDL = `webrpc = v1

name = testapp
version = v0.1.0
`

const invalidRIDL = `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64
  - bad field here
`

// mockClient captures PublishDiagnostics calls for assertions.
type mockClient struct {
	protocol.Client

	mu          sync.Mutex
	diagnostics map[string][]protocol.Diagnostic
}

func newMockClient() *mockClient {
	return &mockClient{
		diagnostics: map[string][]protocol.Diagnostic{},
	}
}

func (m *mockClient) PublishDiagnostics(ctx context.Context, params *protocol.PublishDiagnosticsParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnostics[string(params.URI)] = params.Diagnostics
	return nil
}

func (m *mockClient) getDiagnostics(uri string) []protocol.Diagnostic {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.diagnostics[uri]
}

func setupServer(t *testing.T) (*Server, *mockClient, string) {
	t.Helper()

	dir := t.TempDir()
	client := newMockClient()

	srv := NewServer()
	srv.SetClient(client)
	srv.workspace.SetRoot(dir)

	return srv, client, dir
}

func fileURI(path string) string {
	return string(workspace.PathToURI(path))
}

func TestValidDocumentNoDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	path := filepath.Join(dir, "valid.ridl")
	if err := os.WriteFile(path, []byte(validRIDL), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    validRIDL,
			Version: 1,
		},
	})

	diags := client.getDiagnostics(uri)
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for valid document, got %d: %v", len(diags), diags)
	}
}

func TestInvalidDocumentPublishesDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	path := filepath.Join(dir, "invalid.ridl")
	if err := os.WriteFile(path, []byte(invalidRIDL), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    invalidRIDL,
			Version: 1,
		},
	})

	diags := client.getDiagnostics(uri)
	if len(diags) == 0 {
		t.Error("expected diagnostics for invalid document, got 0")
	}
}

func TestInvalidToValidClearsDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	path := filepath.Join(dir, "doc.ridl")
	if err := os.WriteFile(path, []byte(invalidRIDL), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)

	// Open with invalid content.
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    invalidRIDL,
			Version: 1,
		},
	})

	diags := client.getDiagnostics(uri)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for invalid document, got 0")
	}

	// Change to valid content.
	_ = srv.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentURI(uri),
			},
			Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{Text: validRIDL},
		},
	})

	diags = client.getDiagnostics(uri)
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics after fixing document, got %d: %v", len(diags), diags)
	}
}

func TestCloseDocumentClearsDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	path := filepath.Join(dir, "closing.ridl")
	if err := os.WriteFile(path, []byte(invalidRIDL), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)

	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    invalidRIDL,
			Version: 1,
		},
	})

	diags := client.getDiagnostics(uri)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for invalid document")
	}

	// Close the document.
	_ = srv.DidClose(ctx, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: protocol.DocumentURI(uri),
		},
	})

	diags = client.getDiagnostics(uri)
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics after closing document, got %d", len(diags))
	}
}

func TestImportedFileChangeRefreshesDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	// Create a main file that imports "types.ridl" and uses a type from it.
	// When types.ridl is missing, the imported User type is unresolved,
	// causing a diagnostic. When types.ridl is available via overlay, it resolves.
	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser(id: uint64) => (user: User)
`
	mainPath := filepath.Join(dir, "main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Open the main file — types.ridl doesn't exist, so User type is unresolved.
	mainURI := fileURI(mainPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(mainURI),
			Text:    mainContent,
			Version: 1,
		},
	})

	diags := client.getDiagnostics(mainURI)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics when imported file is missing")
	}

	// Now "open" the imported file in the editor (in-memory overlay).
	typesContent := `webrpc = v1

struct User
  - id: uint64
  - name: string
`
	typesPath := filepath.Join(dir, "types.ridl")
	typesURI := fileURI(typesPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(typesURI),
			Text:    typesContent,
			Version: 1,
		},
	})

	// The main file should now parse cleanly because the overlay provides types.ridl.
	diags = client.getDiagnostics(mainURI)
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics after opening imported file, got %d: %v", len(diags), diags)
	}
}
