package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) DocumentColor(ctx context.Context, params *protocol.DocumentColorParams) ([]protocol.ColorInformation, error) {
	if params == nil {
		return []protocol.ColorInformation{}, nil
	}
	if _, ok := s.docs.Get(string(params.TextDocument.URI)); !ok {
		return []protocol.ColorInformation{}, nil
	}
	return []protocol.ColorInformation{}, nil
}

func (s *Server) ColorPresentation(ctx context.Context, params *protocol.ColorPresentationParams) ([]protocol.ColorPresentation, error) {
	if params == nil {
		return []protocol.ColorPresentation{}, nil
	}
	if _, ok := s.docs.Get(string(params.TextDocument.URI)); !ok {
		return []protocol.ColorPresentation{}, nil
	}
	return []protocol.ColorPresentation{}, nil
}
