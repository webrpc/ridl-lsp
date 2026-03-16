package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) RangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil {
		return nil, nil
	}

	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	if !rangesEqual(params.Range, fullDocumentRange(doc.Content)) {
		return []protocol.TextEdit{}, nil
	}

	return s.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: params.TextDocument,
		Options:      params.Options,
	})
}
