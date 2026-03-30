package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

func (s *Server) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
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

	return s.collectReferenceLocations(target, params.Context.IncludeDeclaration), nil
}

type referenceKind int

const (
	referenceKindType referenceKind = iota
	referenceKindError
)

type referenceTarget struct {
	kind       referenceKind
	name       string
	definition *definitionMatch
}

func (d *semanticDocument) referenceTargetAt(
	pos protocol.Position,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
	resolveError func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) *referenceTarget {
	if !d.valid() {
		return nil
	}

	for _, enumNode := range d.result.Root.Enums() {
		if d.tokenContainsPosition(enumNode.Name(), pos) {
			return &referenceTarget{
				kind:       referenceKindType,
				name:       enumNode.Name().String(),
				definition: definitionForToken(d.path, enumNode.Name()),
			}
		}
		if d.tokenContainsPosition(enumNode.TypeName(), pos) {
			name := d.identifierAtTokenPosition(enumNode.TypeName(), pos)
			if definition := resolveType(d.path, d.result, name); definition != nil {
				return &referenceTarget{kind: referenceKindType, name: name, definition: definition}
			}
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		if d.tokenContainsPosition(structNode.Name(), pos) {
			return &referenceTarget{
				kind:       referenceKindType,
				name:       structNode.Name().String(),
				definition: definitionForToken(d.path, structNode.Name()),
			}
		}
		for _, field := range structNode.Fields() {
			if d.tokenContainsPosition(field.Right(), pos) {
				name := d.identifierAtTokenPosition(field.Right(), pos)
				if definition := resolveType(d.path, d.result, name); definition != nil {
					return &referenceTarget{kind: referenceKindType, name: name, definition: definition}
				}
			}
		}
	}

	for _, aliasNode := range d.result.Root.TypeAliases() {
		if d.tokenContainsPosition(aliasNode.Name(), pos) {
			return &referenceTarget{
				kind:       referenceKindType,
				name:       aliasNode.Name().String(),
				definition: definitionForToken(d.path, aliasNode.Name()),
			}
		}
		if d.tokenContainsPosition(aliasNode.TypeName(), pos) {
			name := d.identifierAtTokenPosition(aliasNode.TypeName(), pos)
			if definition := resolveType(d.path, d.result, name); definition != nil {
				return &referenceTarget{kind: referenceKindType, name: name, definition: definition}
			}
		}
	}

	for _, errorNode := range d.result.Root.Errors() {
		if d.tokenContainsPosition(errorNode.Name(), pos) {
			return &referenceTarget{
				kind:       referenceKindError,
				name:       errorNode.Name().String(),
				definition: definitionForToken(d.path, errorNode.Name()),
			}
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		for _, methodNode := range serviceNode.Methods() {
			for _, input := range methodNode.Inputs() {
				typeToken := argumentTypeToken(input)
				if d.tokenContainsPosition(typeToken, pos) {
					name := d.identifierAtTokenPosition(typeToken, pos)
					if definition := resolveType(d.path, d.result, name); definition != nil {
						return &referenceTarget{kind: referenceKindType, name: name, definition: definition}
					}
				}
			}
			for _, output := range methodNode.Outputs() {
				typeToken := argumentTypeToken(output)
				if d.tokenContainsPosition(typeToken, pos) {
					name := d.identifierAtTokenPosition(typeToken, pos)
					if definition := resolveType(d.path, d.result, name); definition != nil {
						return &referenceTarget{kind: referenceKindType, name: name, definition: definition}
					}
				}
			}
			for _, errorToken := range methodNode.Errors() {
				if d.tokenContainsPosition(errorToken, pos) {
					name := errorToken.String()
					if definition := resolveError(d.path, d.result, name); definition != nil {
						return &referenceTarget{kind: referenceKindError, name: name, definition: definition}
					}
				}
			}
		}
	}

	return nil
}

func (d *semanticDocument) referenceLocations(
	target *referenceTarget,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
	resolveError func(path string, result *ridl.ParseResult, name string) *definitionMatch,
	includeDeclaration bool,
) []protocol.Location {
	if !d.valid() || target == nil || target.definition == nil {
		return nil
	}

	locations := make([]protocol.Location, 0, 8)
	appendDefinition := func(token *ridl.TokenNode, kind referenceKind) {
		if !includeDeclaration || token == nil {
			return
		}
		match := definitionForToken(d.path, token)
		if sameDefinition(match, target.definition) && kind == target.kind {
			locations = append(locations, protocol.Location{
				URI:   PathToURI(d.path),
				Range: d.tokenRange(token, protocol.Position{}),
			})
		}
	}
	appendResolvedTypeRefs := func(token *ridl.TokenNode) {
		for _, rng := range d.identifierRangesInToken(token, target.name) {
			if definition := resolveType(d.path, d.result, target.name); sameDefinition(definition, target.definition) {
				locations = append(locations, protocol.Location{
					URI:   PathToURI(d.path),
					Range: rng,
				})
			}
		}
	}
	appendResolvedErrorRef := func(token *ridl.TokenNode) {
		if token == nil || !strings.EqualFold(token.String(), target.name) {
			return
		}
		if definition := resolveError(d.path, d.result, token.String()); sameDefinition(definition, target.definition) {
			locations = append(locations, protocol.Location{
				URI:   PathToURI(d.path),
				Range: d.tokenRange(token, protocol.Position{}),
			})
		}
	}

	for _, enumNode := range d.result.Root.Enums() {
		appendDefinition(enumNode.Name(), referenceKindType)
		if target.kind == referenceKindType {
			appendResolvedTypeRefs(enumNode.TypeName())
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		appendDefinition(structNode.Name(), referenceKindType)
		if target.kind == referenceKindType {
			for _, field := range structNode.Fields() {
				appendResolvedTypeRefs(field.Right())
			}
		}
	}

	for _, aliasNode := range d.result.Root.TypeAliases() {
		appendDefinition(aliasNode.Name(), referenceKindType)
		if target.kind == referenceKindType {
			appendResolvedTypeRefs(aliasNode.TypeName())
		}
	}

	for _, errorNode := range d.result.Root.Errors() {
		appendDefinition(errorNode.Name(), referenceKindError)
	}

	for _, serviceNode := range d.result.Root.Services() {
		for _, methodNode := range serviceNode.Methods() {
			if target.kind == referenceKindType {
				for _, input := range methodNode.Inputs() {
					appendResolvedTypeRefs(argumentTypeToken(input))
				}
				for _, output := range methodNode.Outputs() {
					appendResolvedTypeRefs(argumentTypeToken(output))
				}
			}
			if target.kind == referenceKindError {
				for _, errorToken := range methodNode.Errors() {
					appendResolvedErrorRef(errorToken)
				}
			}
		}
	}

	return locations
}

func (d *semanticDocument) identifierRangesInToken(token *ridl.TokenNode, name string) []protocol.Range {
	if token == nil || name == "" {
		return nil
	}

	tokenRange, ok := d.tokenRangeInContent(token, nil)
	if !ok {
		if strings.EqualFold(token.String(), name) {
			return []protocol.Range{fallbackTokenRange(token)}
		}
		return nil
	}

	line, ok := d.lineText(int(tokenRange.Start.Line))
	if !ok {
		return nil
	}

	lineRunes := []rune(line)
	start := int(tokenRange.Start.Character)
	end := int(tokenRange.End.Character)
	if start < 0 || end > len(lineRunes) || start >= end {
		return nil
	}

	tokenRunes := lineRunes[start:end]
	nameRunes := []rune(name)
	occurrences := tokenOccurrences(tokenRunes, nameRunes)
	if len(occurrences) == 0 {
		return nil
	}

	ranges := make([]protocol.Range, 0, len(occurrences))
	for _, occurrence := range occurrences {
		ranges = append(ranges, protocol.Range{
			Start: protocol.Position{
				Line:      tokenRange.Start.Line,
				Character: tokenRange.Start.Character + uint32(occurrence),
			},
			End: protocol.Position{
				Line:      tokenRange.Start.Line,
				Character: tokenRange.Start.Character + uint32(occurrence+len(nameRunes)),
			},
		})
	}

	return ranges
}

func (d *semanticDocument) identifierRangeInToken(token *ridl.TokenNode, pos protocol.Position, name string) protocol.Range {
	if token == nil || name == "" {
		return fallbackTokenRange(token)
	}

	for _, rng := range d.identifierRangesInToken(token, name) {
		if pos.Line == rng.Start.Line && pos.Character >= rng.Start.Character && pos.Character < rng.End.Character {
			return rng
		}
	}

	ranges := d.identifierRangesInToken(token, name)
	if len(ranges) > 0 {
		return ranges[0]
	}

	return d.tokenRange(token, pos)
}

func (s *Server) referenceCandidatePaths() []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0, len(s.docs.All()))

	for _, doc := range s.docs.All() {
		if doc == nil || filepath.Ext(doc.Path) != ".ridl" {
			continue
		}
		if _, ok := seen[doc.Path]; ok {
			continue
		}
		seen[doc.Path] = struct{}{}
		paths = append(paths, doc.Path)
	}

	root := s.workspace.Root()
	if root == "" {
		sort.Strings(paths)
		return paths
	}

	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || filepath.Ext(path) != ".ridl" {
			return nil
		}
		if _, ok := seen[path]; ok {
			return nil
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
		return nil
	})

	sort.Strings(paths)
	return paths
}

func (s *Server) collectReferenceLocations(target *referenceTarget, includeDeclaration bool) []protocol.Location {
	if target == nil || target.definition == nil {
		return nil
	}

	locations := make([]protocol.Location, 0, 8)
	seen := map[string]struct{}{}

	for _, path := range s.referenceCandidatePaths() {
		result := s.parsePathForNavigation(path)
		if result == nil || result.Root == nil {
			continue
		}

		content, ok := s.contentForPath(path)
		if !ok {
			continue
		}

		doc := newSemanticDocument(path, content, result)
		for _, location := range doc.referenceLocations(target, s.resolveTypeDefinition, s.resolveErrorDefinition, includeDeclaration) {
			key := referenceLocationKey(location)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			locations = append(locations, location)
		}
	}

	sort.SliceStable(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return string(locations[i].URI) < string(locations[j].URI)
		}
		if locations[i].Range.Start.Line != locations[j].Range.Start.Line {
			return locations[i].Range.Start.Line < locations[j].Range.Start.Line
		}
		return locations[i].Range.Start.Character < locations[j].Range.Start.Character
	})

	return locations
}

func (s *Server) contentForPath(path string) (string, bool) {
	if doc, ok := s.docs.FindByPath(path); ok {
		return doc.Content, true
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(content), true
}

func sameDefinition(a, b *definitionMatch) bool {
	if a == nil || b == nil || a.token == nil || b.token == nil {
		return false
	}
	return a.path == b.path && ridl.TokenPos(a.token) == ridl.TokenPos(b.token) && a.token.String() == b.token.String()
}

func referenceLocationKey(location protocol.Location) string {
	return fmt.Sprintf(
		"%s:%d:%d:%d:%d",
		location.URI,
		location.Range.Start.Line,
		location.Range.Start.Character,
		location.Range.End.Line,
		location.Range.End.Character,
	)
}
