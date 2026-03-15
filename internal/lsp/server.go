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
			TextDocumentSync: protocol.TextDocumentSyncKindFull,
			CompletionProvider: &protocol.CompletionOptions{
				TriggerCharacters: []string{":", " ", "-", ".", "(", "<", "+"},
			},
			HoverProvider:          true,
			DefinitionProvider:     true,
			DocumentSymbolProvider: true,
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
	return &protocol.CompletionList{}, nil
}

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	return nil, nil
}

func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) {
	return nil, nil
}

func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) ([]any, error) {
	return []any{}, nil
}
