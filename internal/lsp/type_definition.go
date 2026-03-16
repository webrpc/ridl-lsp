package lsp

import (
	"context"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

func (s *Server) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) ([]protocol.Location, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	match := semanticDoc.typeDefinitionAt(params.Position, s.resolveTypeDefinition)
	if match == nil {
		return nil, nil
	}

	return []protocol.Location{match.location()}, nil
}

func (d *semanticDocument) typeDefinitionAt(
	pos protocol.Position,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) *definitionMatch {
	if !d.valid() {
		return nil
	}

	for _, enumNode := range d.result.Root.Enums() {
		if d.tokenContainsPosition(enumNode.TypeName(), pos) {
			return d.typeDefinitionFromToken(enumNode.TypeName(), pos, resolveType)
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		for _, field := range structNode.Fields() {
			if d.tokenContainsPosition(field.Left(), pos) {
				return d.firstResolvableTypeDefinition(field.Right(), resolveType)
			}
			if d.tokenContainsPosition(field.Right(), pos) {
				return d.typeDefinitionFromToken(field.Right(), pos, resolveType)
			}
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		for _, methodNode := range serviceNode.Methods() {
			for _, input := range methodNode.Inputs() {
				if d.tokenContainsPosition(input.Name(), pos) {
					return d.firstResolvableTypeDefinition(input.TypeName(), resolveType)
				}
				if d.tokenContainsPosition(input.TypeName(), pos) {
					return d.typeDefinitionFromToken(input.TypeName(), pos, resolveType)
				}
			}
			for _, output := range methodNode.Outputs() {
				if d.tokenContainsPosition(output.Name(), pos) {
					return d.firstResolvableTypeDefinition(output.TypeName(), resolveType)
				}
				if d.tokenContainsPosition(output.TypeName(), pos) {
					return d.typeDefinitionFromToken(output.TypeName(), pos, resolveType)
				}
			}
		}
	}

	return nil
}

func (d *semanticDocument) typeDefinitionFromToken(
	token *ridl.TokenNode,
	pos protocol.Position,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) *definitionMatch {
	if token == nil {
		return nil
	}

	if name := d.identifierAtTokenPosition(token, pos); name != "" {
		return resolveType(d.path, d.result, name)
	}

	return d.firstResolvableTypeDefinition(token, resolveType)
}

func (d *semanticDocument) firstResolvableTypeDefinition(
	token *ridl.TokenNode,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) *definitionMatch {
	if token == nil {
		return nil
	}

	for _, name := range unresolvedTypeNames(token.String()) {
		if isBuiltInRIDLType(name) {
			continue
		}
		if match := resolveType(d.path, d.result, name); match != nil {
			return match
		}
	}

	return nil
}
