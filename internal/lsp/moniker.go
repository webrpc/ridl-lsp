package lsp

import (
	"context"
	"path/filepath"

	"go.lsp.dev/protocol"
)

const monikerScheme = "ridl"

func (s *Server) Moniker(ctx context.Context, params *protocol.MonikerParams) ([]protocol.Moniker, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return []protocol.Moniker{}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return []protocol.Moniker{}, nil
	}

	target := semanticDoc.referenceTargetAt(params.Position, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if target == nil || target.definition == nil {
		return []protocol.Moniker{}, nil
	}

	kind := protocol.MonikerKindImport
	if target.definition.path == doc.Path && rangeContains(target.definition.location().Range, protocol.Range{
		Start: params.Position,
		End:   params.Position,
	}) {
		kind = protocol.MonikerKindExport
	}

	return []protocol.Moniker{{
		Scheme:     monikerScheme,
		Identifier: s.monikerIdentifier(target),
		Unique:     protocol.UniquenessLevelProject,
		Kind:       kind,
	}}, nil
}

func (s *Server) monikerIdentifier(target *referenceTarget) string {
	if target == nil || target.definition == nil {
		return ""
	}

	kind := "symbol"
	switch target.kind {
	case referenceKindType:
		kind = "type"
	case referenceKindError:
		kind = "error"
	}

	return kind + ":" + s.monikerPath(target.definition.path) + "#" + target.name
}

func (s *Server) monikerPath(path string) string {
	root := s.workspace.Root()
	if root != "" {
		if relPath, err := filepath.Rel(root, path); err == nil {
			relPath = filepath.ToSlash(relPath)
			if relPath != "." && relPath != "" && relPath != ".." {
				return relPath
			}
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}
