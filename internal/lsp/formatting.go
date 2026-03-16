package lsp

import (
	"context"
	"strings"

	"github.com/webrpc/ridlfmt/formatter"
	"go.lsp.dev/protocol"
)

func (s *Server) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	formatted, err := formatter.Format(strings.NewReader(doc.Content), false)
	if err != nil {
		return nil, err
	}

	if normalizeFormattedContent(doc.Content) == normalizeFormattedContent(formatted) {
		return []protocol.TextEdit{}, nil
	}

	return []protocol.TextEdit{
		{
			Range:   fullDocumentRange(doc.Content),
			NewText: formatted,
		},
	}, nil
}

func fullDocumentRange(content string) protocol.Range {
	lines := strings.Split(content, "\n")
	lastLine := len(lines) - 1
	lastChar := len([]rune(lines[lastLine]))

	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End: protocol.Position{
			Line:      uint32(lastLine),
			Character: uint32(lastChar),
		},
	}
}

func normalizeFormattedContent(content string) string {
	return strings.TrimSuffix(content, "\n")
}
