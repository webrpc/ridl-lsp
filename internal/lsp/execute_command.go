package lsp

import (
	"context"
	"errors"
	"fmt"

	"go.lsp.dev/protocol"
)

const executeCommandFormatDocument = "ridl.formatDocument"

func (s *Server) ExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	if params == nil {
		return false, nil
	}

	switch params.Command {
	case executeCommandFormatDocument:
		return s.executeFormatDocument(ctx, params.Arguments)
	default:
		return nil, fmt.Errorf("unsupported command %q", params.Command)
	}
}

func (s *Server) executeFormatDocument(ctx context.Context, args []any) (bool, error) {
	if s.client == nil {
		return false, nil
	}

	uri, ok := executeCommandURI(args)
	if !ok {
		return false, errors.New("ridl.formatDocument requires a document URI argument")
	}

	edits, err := s.Formatting(ctx, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
		return false, err
	}
	if len(edits) == 0 {
		return false, nil
	}

	return s.client.ApplyEdit(ctx, &protocol.ApplyWorkspaceEditParams{
		Label: "Format RIDL document",
		Edit: protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				uri: edits,
			},
		},
	})
}

func executeCommandURI(args []any) (protocol.DocumentURI, bool) {
	if len(args) == 0 {
		return "", false
	}

	switch value := args[0].(type) {
	case protocol.DocumentURI:
		return value, value != ""
	case string:
		if value == "" {
			return "", false
		}
		return protocol.DocumentURI(value), true
	default:
		return "", false
	}
}
