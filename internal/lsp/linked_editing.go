package lsp

import (
	"context"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

const ridlIdentifierWordPattern = `[A-Za-z][A-Za-z0-9_]*`

func (s *Server) LinkedEditingRange(ctx context.Context, params *protocol.LinkedEditingRangeParams) (*protocol.LinkedEditingRanges, error) {
	if params == nil {
		return nil, nil
	}

	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	ranges := semanticDoc.linkedEditingRangesAt(params.Position, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if len(ranges) == 0 {
		return nil, nil
	}

	return &protocol.LinkedEditingRanges{
		Ranges:      ranges,
		WordPattern: ridlIdentifierWordPattern,
	}, nil
}

func (d *semanticDocument) linkedEditingRangesAt(
	pos protocol.Position,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
	resolveError func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) []protocol.Range {
	target := d.referenceTargetAt(pos, resolveType, resolveError)
	if target == nil || target.definition == nil || target.definition.path != d.path {
		return nil
	}

	locations := d.referenceLocations(target, resolveType, resolveError, true)
	if len(locations) < 2 {
		return nil
	}

	ranges := make([]protocol.Range, 0, len(locations))
	seen := map[string]struct{}{}
	for _, location := range locations {
		if string(location.URI) != string(PathToURI(d.path)) {
			continue
		}

		key := referenceLocationKey(location)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ranges = append(ranges, location.Range)
	}

	if len(ranges) < 2 {
		return nil
	}

	return ranges
}
