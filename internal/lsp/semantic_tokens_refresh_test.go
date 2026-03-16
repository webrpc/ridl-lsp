package lsp

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestSemanticTokensRefreshRequestsClientRefresh(t *testing.T) {
	srv, client, _ := setupServer(t)
	ctx := context.Background()

	if err := srv.SemanticTokensRefresh(ctx); err != nil {
		t.Fatal(err)
	}

	if client.semanticTokensRefreshCount() != 1 {
		t.Fatalf("expected 1 semantic token refresh request, got %d", client.semanticTokensRefreshCount())
	}
}

func TestSemanticTokensRefreshSkipsWhenNoClient(t *testing.T) {
	srv := NewServer(zap.NewNop())
	ctx := context.Background()

	if err := srv.SemanticTokensRefresh(ctx); err != nil {
		t.Fatal(err)
	}
}
