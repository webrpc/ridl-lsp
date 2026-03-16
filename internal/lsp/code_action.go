package lsp

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"
)

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	if params == nil || !codeActionKindRequested(params.Context.Only, protocol.Source) {
		return nil, nil
	}

	edits, err := s.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: params.TextDocument,
	})
	if err != nil || len(edits) == 0 {
		return nil, nil
	}

	return []protocol.CodeAction{
		{
			Title: "Format document",
			Kind:  protocol.Source,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					params.TextDocument.URI: edits,
				},
			},
		},
	}, nil
}

func codeActionKindRequested(only []protocol.CodeActionKind, kind protocol.CodeActionKind) bool {
	if len(only) == 0 {
		return true
	}

	for _, requested := range only {
		if requested == kind {
			return true
		}
		if requested != "" && strings.HasPrefix(string(kind), string(requested)+".") {
			return true
		}
	}

	return false
}
