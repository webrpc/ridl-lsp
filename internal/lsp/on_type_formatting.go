package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

const onTypeFormattingTrigger = "\n"

func (s *Server) OnTypeFormatting(ctx context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil || params.Ch != onTypeFormattingTrigger {
		return []protocol.TextEdit{}, nil
	}

	return s.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: params.TextDocument,
		Options:      params.Options,
	})
}
