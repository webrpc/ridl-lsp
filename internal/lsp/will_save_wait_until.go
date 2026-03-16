package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) WillSaveWaitUntil(ctx context.Context, params *protocol.WillSaveTextDocumentParams) ([]protocol.TextEdit, error) {
	if params == nil {
		return nil, nil
	}

	edits, err := s.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: params.TextDocument,
	})
	if err != nil {
		return nil, nil
	}

	return edits, nil
}
