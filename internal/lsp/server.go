package lsp

import (
	"context"

	"go.lsp.dev/protocol"

	"github.com/webrpc/ridl-lsp/internal/documents"
	ridlparser "github.com/webrpc/ridl-lsp/internal/ridl"
	"github.com/webrpc/ridl-lsp/internal/workspace"
)

type Server struct {
	docs      *documents.Store
	workspace *workspace.Manager
	parser    *ridlparser.Parser
	client    protocol.Client
}

func NewServer() *Server {
	return &Server{
		docs:      documents.NewStore(),
		workspace: workspace.NewManager(),
		parser:    ridlparser.NewParser(),
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
			CodeLensProvider: &protocol.CodeLensOptions{
				ResolveProvider: true,
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
			DocumentLinkProvider: &protocol.DocumentLinkOptions{
				ResolveProvider: true,
			},
			CodeActionProvider: &protocol.CodeActionOptions{
				CodeActionKinds: []protocol.CodeActionKind{protocol.QuickFix, protocol.Source},
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

func (s *Server) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

func (s *Server) Exit(ctx context.Context) error {
	return nil
}

func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	doc := &documents.Document{
		URI:     string(params.TextDocument.URI),
		Path:    workspace.URIToPath(string(params.TextDocument.URI)),
		Content: params.TextDocument.Text,
		Version: params.TextDocument.Version,
	}

	s.docs.Set(doc)
	s.refreshOpenDocuments(ctx)
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil
	}

	if len(params.ContentChanges) > 0 {
		doc.Content = params.ContentChanges[len(params.ContentChanges)-1].Text
		doc.Version = params.TextDocument.Version
		s.docs.Set(doc)
		s.refreshOpenDocuments(ctx)
	}

	return nil
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := string(params.TextDocument.URI)
	s.docs.Delete(uri)
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
