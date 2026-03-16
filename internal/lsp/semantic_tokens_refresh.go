package lsp

import "context"

type semanticTokensRefresher interface {
	SemanticTokensRefresh(ctx context.Context) error
}

func (s *Server) SemanticTokensRefresh(ctx context.Context) error {
	if s.client == nil {
		return nil
	}

	refresher, ok := s.client.(semanticTokensRefresher)
	if !ok {
		return nil
	}

	return refresher.SemanticTokensRefresh(ctx)
}
