package lsp

import (
	"context"
	"os"
	"sort"

	"go.lsp.dev/protocol"

	"github.com/webrpc/ridl-lsp/internal/workspace"
)

func (s *Server) DocumentLink(ctx context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	if params == nil {
		return []protocol.DocumentLink{}, nil
	}

	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return []protocol.DocumentLink{}, nil
	}

	result := s.parsePathForNavigation(doc.Path)
	if result == nil || result.Root == nil {
		return []protocol.DocumentLink{}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, result)
	links := make([]protocol.DocumentLink, 0, len(result.Root.Imports()))
	for _, importNode := range result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil {
			continue
		}

		targetPath, ok := s.existingImportPath(doc.Path, importNode.Path().String())
		if !ok {
			continue
		}

		links = append(links, protocol.DocumentLink{
			Range:   semanticDoc.tokenRange(importNode.Path(), protocol.Position{}),
			Target:  PathToURI(targetPath),
			Tooltip: "Open imported RIDL file",
		})
	}

	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Range.Start.Line != links[j].Range.Start.Line {
			return links[i].Range.Start.Line < links[j].Range.Start.Line
		}
		return links[i].Range.Start.Character < links[j].Range.Start.Character
	})

	return links, nil
}

func (s *Server) DocumentLinkResolve(ctx context.Context, params *protocol.DocumentLink) (*protocol.DocumentLink, error) {
	if params == nil {
		return nil, nil
	}

	return params, nil
}

func (s *Server) existingImportPath(docPath, importPath string) (string, bool) {
	resolvedPath := workspace.ResolveImportPath(docPath, importPath)
	if _, ok := s.docs.FindByPath(resolvedPath); ok {
		return resolvedPath, true
	}

	if _, err := os.Stat(resolvedPath); err == nil {
		return resolvedPath, true
	}

	return "", false
}
