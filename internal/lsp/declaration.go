package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) Declaration(ctx context.Context, params *protocol.DeclarationParams) ([]protocol.Location, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	match := semanticDoc.definitionAt(params.Position, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if match == nil {
		return nil, nil
	}

	return []protocol.Location{match.location()}, nil
}
