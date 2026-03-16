package lsp

import (
	"context"
	"fmt"
	"sort"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

func (s *Server) PrepareRename(ctx context.Context, params *protocol.PrepareRenameParams) (*protocol.Range, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	target, rng := semanticDoc.renameTargetAt(params.Position, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if target == nil || rng == nil {
		return nil, nil
	}

	return rng, nil
}

func (s *Server) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	if !isValidRenameIdentifier(params.NewName) {
		return nil, fmt.Errorf("invalid RIDL identifier %q", params.NewName)
	}

	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	target, _ := semanticDoc.renameTargetAt(params.Position, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if target == nil {
		return nil, nil
	}

	locations := s.collectReferenceLocations(target, true)
	if len(locations) == 0 {
		return nil, nil
	}

	changes := make(map[protocol.DocumentURI][]protocol.TextEdit)
	for _, location := range locations {
		changes[location.URI] = append(changes[location.URI], protocol.TextEdit{
			Range:   location.Range,
			NewText: params.NewName,
		})
	}

	for uri := range changes {
		sort.SliceStable(changes[uri], func(i, j int) bool {
			if changes[uri][i].Range.Start.Line != changes[uri][j].Range.Start.Line {
				return changes[uri][i].Range.Start.Line > changes[uri][j].Range.Start.Line
			}
			return changes[uri][i].Range.Start.Character > changes[uri][j].Range.Start.Character
		})
	}

	return &protocol.WorkspaceEdit{Changes: changes}, nil
}

func (d *semanticDocument) renameTargetAt(
	pos protocol.Position,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
	resolveError func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) (*referenceTarget, *protocol.Range) {
	if !d.valid() {
		return nil, nil
	}

	for _, enumNode := range d.result.Root.Enums() {
		if d.tokenContainsPosition(enumNode.Name(), pos) {
			rng := d.tokenRange(enumNode.Name(), pos)
			return &referenceTarget{
				kind:       referenceKindType,
				name:       enumNode.Name().String(),
				definition: definitionForToken(d.path, enumNode.Name()),
			}, &rng
		}
		if d.tokenContainsPosition(enumNode.TypeName(), pos) {
			name := d.identifierAtTokenPosition(enumNode.TypeName(), pos)
			if name == "" || isBuiltInRIDLType(name) {
				return nil, nil
			}
			if definition := resolveType(d.path, d.result, name); definition != nil {
				rng := d.identifierRangeInToken(enumNode.TypeName(), pos, name)
				return &referenceTarget{kind: referenceKindType, name: name, definition: definition}, &rng
			}
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		if d.tokenContainsPosition(structNode.Name(), pos) {
			rng := d.tokenRange(structNode.Name(), pos)
			return &referenceTarget{
				kind:       referenceKindType,
				name:       structNode.Name().String(),
				definition: definitionForToken(d.path, structNode.Name()),
			}, &rng
		}
		for _, field := range structNode.Fields() {
			if d.tokenContainsPosition(field.Right(), pos) {
				name := d.identifierAtTokenPosition(field.Right(), pos)
				if name == "" || isBuiltInRIDLType(name) {
					return nil, nil
				}
				if definition := resolveType(d.path, d.result, name); definition != nil {
					rng := d.identifierRangeInToken(field.Right(), pos, name)
					return &referenceTarget{kind: referenceKindType, name: name, definition: definition}, &rng
				}
			}
		}
	}

	for _, errorNode := range d.result.Root.Errors() {
		if d.tokenContainsPosition(errorNode.Name(), pos) {
			rng := d.tokenRange(errorNode.Name(), pos)
			return &referenceTarget{
				kind:       referenceKindError,
				name:       errorNode.Name().String(),
				definition: definitionForToken(d.path, errorNode.Name()),
			}, &rng
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		for _, methodNode := range serviceNode.Methods() {
			for _, input := range methodNode.Inputs() {
				typeToken := argumentTypeToken(input)
				if d.tokenContainsPosition(typeToken, pos) {
					name := d.identifierAtTokenPosition(typeToken, pos)
					if name == "" || isBuiltInRIDLType(name) {
						return nil, nil
					}
					if definition := resolveType(d.path, d.result, name); definition != nil {
						rng := d.identifierRangeInToken(typeToken, pos, name)
						return &referenceTarget{kind: referenceKindType, name: name, definition: definition}, &rng
					}
				}
			}
			for _, output := range methodNode.Outputs() {
				typeToken := argumentTypeToken(output)
				if d.tokenContainsPosition(typeToken, pos) {
					name := d.identifierAtTokenPosition(typeToken, pos)
					if name == "" || isBuiltInRIDLType(name) {
						return nil, nil
					}
					if definition := resolveType(d.path, d.result, name); definition != nil {
						rng := d.identifierRangeInToken(typeToken, pos, name)
						return &referenceTarget{kind: referenceKindType, name: name, definition: definition}, &rng
					}
				}
			}
			for _, errorToken := range methodNode.Errors() {
				if d.tokenContainsPosition(errorToken, pos) {
					if definition := resolveError(d.path, d.result, errorToken.String()); definition != nil {
						rng := d.tokenRange(errorToken, pos)
						return &referenceTarget{kind: referenceKindError, name: errorToken.String(), definition: definition}, &rng
					}
				}
			}
		}
	}

	return nil, nil
}

func isValidRenameIdentifier(name string) bool {
	if name == "" {
		return false
	}
	runes := []rune(name)
	if len(runes) == 0 {
		return false
	}
	first := runes[0]
	if first == '_' || first >= '0' && first <= '9' {
		return false
	}
	if !isIdentifierRune(first) {
		return false
	}
	for _, r := range runes[1:] {
		if !isIdentifierRune(r) {
			return false
		}
	}
	return true
}
