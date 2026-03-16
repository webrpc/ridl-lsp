package lsp

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) DocumentHighlight(ctx context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	target := semanticDoc.referenceTargetAt(params.Position, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if target == nil || target.definition == nil {
		return nil, nil
	}

	locations := semanticDoc.referenceLocations(target, s.resolveTypeDefinition, s.resolveErrorDefinition, true)
	if len(locations) == 0 {
		return nil, nil
	}

	highlights := make([]protocol.DocumentHighlight, 0, len(locations))
	for _, location := range locations {
		highlights = append(highlights, protocol.DocumentHighlight{
			Range: location.Range,
			Kind:  semanticDoc.highlightKindForRange(target, location.Range),
		})
	}

	return highlights, nil
}

func (d *semanticDocument) highlightKindForRange(target *referenceTarget, rng protocol.Range) protocol.DocumentHighlightKind {
	if d == nil || target == nil || target.definition == nil || target.definition.path != d.path {
		return protocol.DocumentHighlightKindRead
	}

	definitionRange := d.tokenRange(target.definition.token, protocol.Position{})
	if definitionRange == rng {
		return protocol.DocumentHighlightKindWrite
	}

	return protocol.DocumentHighlightKindRead
}
