package lsp

import (
	"context"
	"os"
	"sync"
	"sync/atomic"

	"go.lsp.dev/protocol"
	"go.uber.org/zap"

	"github.com/webrpc/ridl-lsp/internal/documents"
	ridlparser "github.com/webrpc/ridl-lsp/internal/ridl"
	"github.com/webrpc/ridl-lsp/internal/workspace"
)

type Server struct {
	docs      *documents.Store
	workspace *workspace.Manager
	parser    *ridlparser.Parser
	client    protocol.Client
	logger    *zap.Logger

	parseCache *parseCache

	// workspaceMu guards docs mutations and gen bumps so the two are always
	// seen together by any reader that loads gen as a cache key.
	workspaceMu sync.RWMutex
	gen         atomic.Uint64

	// cacheEnabled is set true only after the client confirms watcher registration,
	// ensuring the parse cache is only used when invalidation events are guaranteed.
	cacheEnabled atomic.Bool
	// watchSupported is captured from InitializeParams and stays immutable after Initialize returns.
	watchSupported bool

	shutdown atomic.Bool
	// exitProcess is os.Exit in production; injectable so the exit-code contract
	// can be tested without terminating the test binary.
	exitProcess func(int)
}

func NewServer(logger *zap.Logger) *Server {
	return &Server{
		docs:        documents.NewStore(),
		workspace:   workspace.NewManager(),
		parser:      ridlparser.NewParser(),
		logger:      logger,
		exitProcess: os.Exit,
		parseCache:  newParseCache(),
	}
}

func (s *Server) SetClient(client protocol.Client) {
	s.client = client
}

func (s *Server) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	if params.RootURI != "" { //nolint:staticcheck // RootURI needed for older clients
		s.workspace.SetRootFromURI(string(params.RootURI)) //nolint:staticcheck
	} else if params.RootPath != "" { //nolint:staticcheck // RootPath fallback for legacy clients
		s.workspace.SetRoot(params.RootPath) //nolint:staticcheck
	}

	// Initialize runs once before any concurrent handler; plain field write is safe.
	if ws := params.Capabilities.Workspace; ws != nil && ws.DidChangeWatchedFiles != nil {
		s.watchSupported = ws.DidChangeWatchedFiles.DynamicRegistration
	}

	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync: &protocol.TextDocumentSyncOptions{
				OpenClose:         true,
				Change:            protocol.TextDocumentSyncKindFull,
				WillSaveWaitUntil: true,
				Save:              &protocol.SaveOptions{},
			},
			CompletionProvider: &protocol.CompletionOptions{
				TriggerCharacters: []string{":", " ", "-", ".", "(", "<", "+"},
			},
			DocumentOnTypeFormattingProvider: &protocol.DocumentOnTypeFormattingOptions{
				FirstTriggerCharacter: onTypeFormattingTrigger,
			},
			// ResolveProvider is false: CodeLens returns fully-resolved lenses, so
			// clients must not call codeLens/resolve.
			CodeLensProvider: &protocol.CodeLensOptions{
				ResolveProvider: false,
			},
			ColorProvider:                   true,
			DocumentFormattingProvider:      true,
			DocumentRangeFormattingProvider: true,
			FoldingRangeProvider:            true,
			HoverProvider:                   true,
			SignatureHelpProvider: &protocol.SignatureHelpOptions{
				TriggerCharacters:   []string{"(", ","},
				RetriggerCharacters: []string{","},
			},
			DeclarationProvider:        true,
			DefinitionProvider:         true,
			TypeDefinitionProvider:     true,
			ReferencesProvider:         true,
			DocumentHighlightProvider:  true,
			RenameProvider:             true,
			DocumentSymbolProvider:     true,
			WorkspaceSymbolProvider:    true,
			SelectionRangeProvider:     true,
			LinkedEditingRangeProvider: true,
			MonikerProvider:            true,
			DocumentLinkProvider: &protocol.DocumentLinkOptions{
				ResolveProvider: true,
			},
			CodeActionProvider: &protocol.CodeActionOptions{
				CodeActionKinds: []protocol.CodeActionKind{protocol.QuickFix, protocol.Source},
			},
			ExecuteCommandProvider: &protocol.ExecuteCommandOptions{
				Commands: []string{executeCommandFormatDocument},
			},
			SemanticTokensProvider: map[string]any{
				"legend": protocol.SemanticTokensLegend{
					TokenTypes:     semanticTokenLegendTypes,
					TokenModifiers: semanticTokenLegendModifiers,
				},
				"full": map[string]any{
					"delta": true,
				},
				"range": true,
			},
		},
	}, nil
}

// Initialized registers **/*.ridl file watchers and gates the parse cache on success.
func (s *Server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	if !s.watchSupported || s.client == nil {
		return nil
	}
	err := s.client.RegisterCapability(ctx, &protocol.RegistrationParams{
		Registrations: []protocol.Registration{{
			ID:     "ridl-watch-files",
			Method: "workspace/didChangeWatchedFiles",
			RegisterOptions: protocol.DidChangeWatchedFilesRegistrationOptions{
				Watchers: []protocol.FileSystemWatcher{{GlobPattern: "**/*.ridl"}},
			},
		}},
	})
	if err != nil {
		s.logger.Warn("ridl-lsp: file watcher registration failed; session parse cache disabled", zap.Error(err))
		return nil
	}
	s.cacheEnabled.Store(true)
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.shutdown.Store(true)
	return nil
}

func (s *Server) Exit(ctx context.Context) error {
	// LSP: exit 0 only if shutdown was received first, otherwise 1.
	_ = s.logger.Sync() //nolint:errcheck // best-effort flush before exit
	s.exitProcess(exitCode(s.shutdown.Load()))
	return nil
}

// exitCode maps the shutdown-before-exit state to the LSP-mandated process code.
func exitCode(shutdownReceived bool) int {
	if shutdownReceived {
		return 0
	}
	return 1
}

func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	doc := &documents.Document{
		URI:     string(params.TextDocument.URI),
		Path:    workspace.URIToPath(string(params.TextDocument.URI)),
		Content: params.TextDocument.Text,
		Version: params.TextDocument.Version,
	}

	s.workspaceMu.Lock()
	s.docs.Set(doc)
	s.gen.Add(1)
	s.workspaceMu.Unlock()
	s.refreshOpenDocuments(ctx)
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil
	}

	if len(params.ContentChanges) > 0 {
		// Copy-on-write: never mutate the stored *Document in place, since other
		// handlers may hold the prior snapshot. The new content invalidates the
		// cached parse result.
		updated := *doc
		updated.Content = params.ContentChanges[len(params.ContentChanges)-1].Text
		updated.Version = params.TextDocument.Version
		updated.Result = nil
		s.workspaceMu.Lock()
		s.docs.Set(&updated)
		s.gen.Add(1)
		s.workspaceMu.Unlock()
		s.refreshOpenDocuments(ctx)
	}

	return nil
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := string(params.TextDocument.URI)
	s.workspaceMu.Lock()
	s.docs.Delete(uri)
	s.gen.Add(1)
	s.workspaceMu.Unlock()
	if s.client != nil {
		_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
			URI:         protocol.DocumentURI(uri),
			Diagnostics: []protocol.Diagnostic{},
		})
	}
	s.refreshOpenDocuments(ctx)
	return nil
}

func (s *Server) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	if _, ok := s.docs.Get(string(params.TextDocument.URI)); !ok {
		return nil
	}

	s.refreshOpenDocuments(ctx)
	return nil
}

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return &protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	return &protocol.CompletionList{
		Items: semanticDoc.completionItemsAt(params.Position),
	}, nil
}
