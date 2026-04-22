package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.lsp.dev/protocol"
	"go.uber.org/zap"

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

	mu                      sync.Mutex
	appliedEdit             *protocol.ApplyWorkspaceEditParams
	diagnostics             map[string][]protocol.Diagnostic
	semanticTokensRefreshes int
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

func (m *mockClient) SemanticTokensRefresh(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.semanticTokensRefreshes++
	return nil
}

func (m *mockClient) ApplyEdit(ctx context.Context, params *protocol.ApplyWorkspaceEditParams) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if params == nil {
		m.appliedEdit = nil
		return false, nil
	}
	cloned := *params
	m.appliedEdit = &cloned
	return true, nil
}

func (m *mockClient) semanticTokensRefreshCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.semanticTokensRefreshes
}

func (m *mockClient) lastAppliedEdit() *protocol.ApplyWorkspaceEditParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appliedEdit
}

func setupServer(t *testing.T) (*Server, *mockClient, string) {
	t.Helper()

	dir := t.TempDir()
	client := newMockClient()

	srv := NewServer(zap.NewNop())
	srv.SetClient(client)
	srv.workspace.SetRoot(dir)

	return srv, client, dir
}

func setupServerWithoutRoot(t *testing.T) (*Server, *mockClient) {
	t.Helper()

	client := newMockClient()

	srv := NewServer(zap.NewNop())
	srv.SetClient(client)

	return srv, client
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

func TestOpenUnsavedDocumentWithoutWorkspaceRootUsesOverlay(t *testing.T) {
	srv, client := setupServerWithoutRoot(t)
	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "unsaved.ridl")
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
		t.Fatalf("expected 0 diagnostics for unsaved document, got %d: %v", len(diags), diags)
	}

	doc, ok := srv.docs.Get(uri)
	if !ok {
		t.Fatal("expected opened document to be tracked")
	}
	if doc.Result == nil {
		t.Fatal("expected parse result for unsaved document")
	}
}

func TestDocumentOutsideWorkspaceRootUsesLiveBuffer(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	outsideDir := t.TempDir()
	path := filepath.Join(outsideDir, "outside.ridl")
	if err := os.WriteFile(path, []byte(validRIDL), 0644); err != nil {
		t.Fatal(err)
	}

	if filepath.Dir(path) == dir {
		t.Fatal("expected test document to be outside workspace root")
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
		t.Fatal("expected diagnostics from live buffer for document outside workspace root")
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

func TestValidToInvalidClearsCachedParseResult(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	path := filepath.Join(dir, "cached-result.ridl")
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

	doc, ok := srv.docs.Get(uri)
	if !ok {
		t.Fatal("expected opened document to be tracked")
	}
	if doc.Result == nil {
		t.Fatal("expected parse result for valid document")
	}

	_ = srv.DidChange(ctx, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: protocol.DocumentURI(uri),
			},
			Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{Text: invalidRIDL},
		},
	})

	diags := client.getDiagnostics(uri)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics after making document invalid")
	}

	doc, ok = srv.docs.Get(uri)
	if !ok {
		t.Fatal("expected changed document to remain tracked")
	}
	if doc.Result != nil {
		t.Fatal("expected cached parse result to be cleared after parse failure")
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

func TestSaveRefreshesDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser(id: uint64) => (user: User)
`
	mainPath := filepath.Join(dir, "main-save.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
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

	diags := client.getDiagnostics(mainURI)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics when imported file is missing")
	}

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	_ = srv.DidSave(ctx, &protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: protocol.DocumentURI(mainURI),
		},
	})

	diags = client.getDiagnostics(mainURI)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics after save-triggered refresh, got %d: %v", len(diags), diags)
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

func TestImportedFileCloseRefreshesDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser(id: uint64) => (user: User)
`
	mainPath := filepath.Join(dir, "main-close-import.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
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

	diags := client.getDiagnostics(mainURI)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics when imported file is missing")
	}

	typesContent := `webrpc = v1

struct User
  - id: uint64
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

	diags = client.getDiagnostics(mainURI)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics after opening imported file, got %d: %v", len(diags), diags)
	}

	_ = srv.DidClose(ctx, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(typesURI)},
	})

	diags = client.getDiagnostics(mainURI)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics after closing overlay-only imported file")
	}
}

func TestNarrowImportDiagnostic(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64

struct Account
  - id: uint64

struct Org
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Message, "can be narrowed to: User") {
		t.Fatalf("expected narrowing diagnostic, got %q", diags[0].Message)
	}
	if diags[0].Severity != protocol.DiagnosticSeverityWarning {
		t.Fatalf("expected warning severity, got %v", diags[0].Severity)
	}
}

func TestNarrowImportExcludesTransitiveTypes(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	// common.ridl defines Page.
	commonContent := `webrpc = v1

struct Page
  - number: uint32
`
	commonPath := filepath.Join(dir, "common.ridl")
	if err := os.WriteFile(commonPath, []byte(commonContent), 0644); err != nil {
		t.Fatal(err)
	}

	// types.ridl defines User and Account, and imports common.ridl.
	// Through the resolved schema, types.ridl "re-exports" Page.
	typesContent := `webrpc = v1

import
  - common.ridl

struct User
  - id: uint64

struct Account
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	// main.ridl uses User (from types.ridl) and Page (from common.ridl).
	// The narrowing diagnostic for types.ridl should suggest only "User",
	// not "Page, User" — Page is not locally defined in types.ridl.
	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - common.ridl
  - types.ridl

service TestService
  - ListUsers(page: Page) => (users: []User)
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)

	// types.ridl locally defines User and Account. Only User is used,
	// so we expect a narrowing diagnostic mentioning only "User".
	var typesNarrow string
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning && strings.Contains(d.Message, "types.ridl") && strings.Contains(d.Message, "can be narrowed") {
			typesNarrow = d.Message
		}
	}
	if typesNarrow == "" {
		t.Fatalf("expected narrowing diagnostic for types.ridl, got: %v", diags)
	}
	if strings.Contains(typesNarrow, "Page") {
		t.Errorf("narrowing diagnostic for types.ridl should not include transitive type Page: %q", typesNarrow)
	}
	if !strings.Contains(typesNarrow, "User") {
		t.Errorf("narrowing diagnostic for types.ridl should include locally defined User: %q", typesNarrow)
	}
}

func TestNoNarrowDiagnosticWhenAllTypesUsed(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics when all types used, got %d: %v", len(diags), diags)
	}
}

func TestUnusedImportDiagnostic(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Message, "is unused") {
		t.Fatalf("expected unused import diagnostic, got %q", diags[0].Message)
	}
	if diags[0].Severity != protocol.DiagnosticSeverityWarning {
		t.Fatalf("expected warning severity, got %v", diags[0].Severity)
	}
}

func TestImportWithServiceNotFlaggedAsUnused(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	// A service-bearing file — structs are request/response DTOs not referenced
	// by the importer, but the service itself contributes to codegen output.
	serviceContent := `webrpc = v1

struct GetUserRequest
  - id: uint64

struct GetUserResponse
  - name: string

service Users
  - GetUser(req: GetUserRequest) => (resp: GetUserResponse)
`
	servicePath := filepath.Join(dir, "users.ridl")
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Aggregator pattern: main.ridl only imports; it doesn't reference
	// any type from users.ridl directly.
	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - users.ridl
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning {
			t.Fatalf("unexpected warning on aggregator importing service file: %q", d.Message)
		}
	}
}

func TestImportWithErrorsOnlyNotFlaggedAsUnused(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	errorsContent := `webrpc = v1

error 100 NotFound "not found" HTTP 404
error 101 Forbidden "forbidden" HTTP 403
`
	errorsPath := filepath.Join(dir, "errors.ridl")
	if err := os.WriteFile(errorsPath, []byte(errorsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Aggregator imports the errors file but doesn't reference NotFound/Forbidden
	// here. Errors are aggregated into codegen output regardless.
	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - errors.ridl
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning {
			t.Fatalf("unexpected warning on import of errors-only file: %q", d.Message)
		}
	}
}

func TestImportWithServiceNoNarrowingSuggestion(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	// Service file also exposes two auxiliary types, only one of which the
	// importer references. Narrowing the import would drop the service from
	// codegen — suggestion must not fire.
	serviceContent := `webrpc = v1

struct AuditEntry
  - when: uint64

struct AuditSource
  - kind: string

service Users
  - GetUser() => (id: uint64)
`
	servicePath := filepath.Join(dir, "users.ridl")
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - users.ridl

struct Audit
  - entry: AuditEntry
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning && strings.Contains(d.Message, "can be narrowed") {
			t.Fatalf("unexpected narrowing suggestion on service-bearing file: %q", d.Message)
		}
	}
}

func TestTransitiveReImportDiagnostic(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	orgContent := `webrpc = v1

struct OrgID
  - value: string
`
	orgPath := filepath.Join(dir, "organization.ridl")
	if err := os.WriteFile(orgPath, []byte(orgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	userContent := `webrpc = v1

import
  - organization.ridl

struct User
  - id: uint64
  - orgID: OrgID
`
	userPath := filepath.Join(dir, "user.ridl")
	if err := os.WriteFile(userPath, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import user.ridl
  - OrgID

struct Project
  - orgID: OrgID
`
	path := filepath.Join(dir, "transitive.ridl")
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

	diags := client.getDiagnostics(uri)
	var reImportDiag *protocol.Diagnostic
	for i := range diags {
		if diags[i].Severity == protocol.DiagnosticSeverityWarning &&
			strings.Contains(diags[i].Message, "defined in") {
			reImportDiag = &diags[i]
			break
		}
	}

	if reImportDiag == nil {
		t.Fatalf("expected transitive re-import warning, got: %#v", diags)
	}

	if !strings.Contains(reImportDiag.Message, "organization.ridl") {
		t.Errorf("expected diagnostic to mention organization.ridl, got: %s", reImportDiag.Message)
	}
}

func TestNoTransitiveDiagnosticForCorrectSelectiveImport(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import types.ridl
  - User

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "correct-selective.ridl")
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

	diags := client.getDiagnostics(uri)
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning {
			t.Fatalf("unexpected warning: %s", d.Message)
		}
	}
}

func TestWatchedImportedFileChangesRefreshDiagnostics(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - shared/types.ridl

service TestService
  - GetUser(id: uint64) => (user: User)
`
	mainPath := filepath.Join(dir, "watched-main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	sharedDir := filepath.Join(dir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	typesPath := filepath.Join(sharedDir, "types.ridl")

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
		t.Fatal("expected diagnostics when watched imported file is missing")
	}

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := srv.DidChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{
		Changes: []*protocol.FileEvent{
			{
				URI:  protocol.DocumentURI(fileURI(typesPath)),
				Type: protocol.FileChangeTypeCreated,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	diags = client.getDiagnostics(mainURI)
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics after watched imported file creation, got %d: %v", len(diags), diags)
	}

	if err := os.Remove(typesPath); err != nil {
		t.Fatal(err)
	}

	if err := srv.DidChangeWatchedFiles(ctx, &protocol.DidChangeWatchedFilesParams{
		Changes: []*protocol.FileEvent{
			{
				URI:  protocol.DocumentURI(fileURI(typesPath)),
				Type: protocol.FileChangeTypeDeleted,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	diags = client.getDiagnostics(mainURI)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics after watched imported file deletion")
	}
}
