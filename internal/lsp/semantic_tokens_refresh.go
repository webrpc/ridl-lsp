package lsp

import "context"

func (s *Server) SemanticTokensRefresh(ctx context.Context) error {
	if s.client == nil {
		return nil
	}

	return s.client.SemanticTokensRefresh(ctx)
}
