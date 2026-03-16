package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) SemanticTokensFullDelta(ctx context.Context, params *protocol.SemanticTokensDeltaParams) (any, error) {
	if params == nil {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	return s.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{
		TextDocument: params.TextDocument,
	})
}
