package lsp

import (
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	"github.com/webrpc/webrpc/schema"
	ridl "github.com/webrpc/webrpc/schema/ridl"
)

type semanticDocument struct {
	path    string
	content string
	result  *ridl.ParseResult
}

func newSemanticDocument(path, content string, result *ridl.ParseResult) *semanticDocument {
	return &semanticDocument{
		path:    path,
		content: content,
		result:  result,
	}
}

func (d *semanticDocument) valid() bool {
	return d != nil && d.result != nil && d.result.Root != nil && d.result.Schema != nil
}

func (d *semanticDocument) hoverAt(pos protocol.Position) *hoverMatch {
	if !d.valid() {
		return nil
	}

	root := d.result.Root
	for _, enumNode := range root.Enums() {
		if match := d.hoverForToken(enumNode.Name(), pos, func() string {
			return formatTypeHover(d.typeByName(enumNode.Name().String()))
		}); match != nil {
			return match
		}

		if match := d.hoverForToken(enumNode.TypeName(), pos, func() string {
			return d.formatTypeTokenHover(enumNode.TypeName(), pos)
		}); match != nil {
			return match
		}

		for _, value := range enumNode.Values() {
			if match := d.hoverForToken(value.Left(), pos, func() string {
				return formatEnumValueHover(d.typeByName(enumNode.Name().String()), value.Left().String())
			}); match != nil {
				return match
			}

			if match := d.hoverForToken(value.Right(), pos, func() string {
				return d.formatTypeTokenHover(value.Right(), pos)
			}); match != nil {
				return match
			}
		}
	}

	for _, structNode := range root.Structs() {
		if match := d.hoverForToken(structNode.Name(), pos, func() string {
			return formatTypeHover(d.typeByName(structNode.Name().String()))
		}); match != nil {
			return match
		}

		structType := d.typeByName(structNode.Name().String())
		for _, field := range structNode.Fields() {
			if match := d.hoverForToken(field.Left(), pos, func() string {
				return formatFieldHover(structType, field.Left().String())
			}); match != nil {
				return match
			}

			if match := d.hoverForToken(field.Right(), pos, func() string {
				return d.formatTypeTokenHover(field.Right(), pos)
			}); match != nil {
				return match
			}
		}
	}

	for _, errorNode := range root.Errors() {
		if match := d.hoverForToken(errorNode.Name(), pos, func() string {
			return formatErrorHover(d.errorByName(errorNode.Name().String()))
		}); match != nil {
			return match
		}
	}

	for _, serviceNode := range root.Services() {
		service := d.result.Schema.GetServiceByName(serviceNode.Name().String())

		if match := d.hoverForToken(serviceNode.Name(), pos, func() string {
			return formatServiceHover(service)
		}); match != nil {
			return match
		}

		for _, methodNode := range serviceNode.Methods() {
			method := d.methodByName(service, methodNode.Name().String())

			if match := d.hoverForToken(methodNode.Name(), pos, func() string {
				return formatMethodHover(method)
			}); match != nil {
				return match
			}

			for _, input := range methodNode.Inputs() {
				if match := d.hoverForToken(input.Name(), pos, func() string {
					return formatMethodArgumentHover(method, input.Name().String(), true)
				}); match != nil {
					return match
				}

				if match := d.hoverForToken(input.TypeName(), pos, func() string {
					return d.formatTypeTokenHover(input.TypeName(), pos)
				}); match != nil {
					return match
				}
			}

			for _, output := range methodNode.Outputs() {
				if match := d.hoverForToken(output.Name(), pos, func() string {
					return formatMethodArgumentHover(method, output.Name().String(), false)
				}); match != nil {
					return match
				}

				if match := d.hoverForToken(output.TypeName(), pos, func() string {
					return d.formatTypeTokenHover(output.TypeName(), pos)
				}); match != nil {
					return match
				}
			}

			for _, errorToken := range methodNode.Errors() {
				if match := d.hoverForToken(errorToken, pos, func() string {
					return formatErrorHover(d.errorByName(errorToken.String()))
				}); match != nil {
					return match
				}
			}
		}
	}

	return nil
}

func (d *semanticDocument) definitionAt(
	pos protocol.Position,
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
	resolveError func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) *definitionMatch {
	if !d.valid() {
		return nil
	}

	root := d.result.Root
	for _, enumNode := range root.Enums() {
		if d.tokenContainsPosition(enumNode.Name(), pos) {
			return definitionForToken(d.path, enumNode.Name())
		}
		if d.tokenContainsPosition(enumNode.TypeName(), pos) {
			return resolveType(d.path, d.result, d.identifierAtTokenPosition(enumNode.TypeName(), pos))
		}
		for _, value := range enumNode.Values() {
			if d.tokenContainsPosition(value.Left(), pos) {
				return definitionForToken(d.path, value.Left())
			}
		}
	}

	for _, structNode := range root.Structs() {
		if d.tokenContainsPosition(structNode.Name(), pos) {
			return definitionForToken(d.path, structNode.Name())
		}
		for _, field := range structNode.Fields() {
			if d.tokenContainsPosition(field.Left(), pos) {
				return definitionForToken(d.path, field.Left())
			}
			if d.tokenContainsPosition(field.Right(), pos) {
				return resolveType(d.path, d.result, d.identifierAtTokenPosition(field.Right(), pos))
			}
		}
	}

	for _, errorNode := range root.Errors() {
		if d.tokenContainsPosition(errorNode.Name(), pos) {
			return definitionForToken(d.path, errorNode.Name())
		}
	}

	for _, serviceNode := range root.Services() {
		if d.tokenContainsPosition(serviceNode.Name(), pos) {
			return definitionForToken(d.path, serviceNode.Name())
		}
		for _, methodNode := range serviceNode.Methods() {
			if d.tokenContainsPosition(methodNode.Name(), pos) {
				return definitionForToken(d.path, methodNode.Name())
			}
			for _, input := range methodNode.Inputs() {
				if d.tokenContainsPosition(input.Name(), pos) {
					return definitionForToken(d.path, input.Name())
				}
				if d.tokenContainsPosition(input.TypeName(), pos) {
					return resolveType(d.path, d.result, d.identifierAtTokenPosition(input.TypeName(), pos))
				}
			}
			for _, output := range methodNode.Outputs() {
				if d.tokenContainsPosition(output.Name(), pos) {
					return definitionForToken(d.path, output.Name())
				}
				if d.tokenContainsPosition(output.TypeName(), pos) {
					return resolveType(d.path, d.result, d.identifierAtTokenPosition(output.TypeName(), pos))
				}
			}
			for _, errorToken := range methodNode.Errors() {
				if d.tokenContainsPosition(errorToken, pos) {
					return resolveError(d.path, d.result, errorToken.String())
				}
			}
		}
	}

	return nil
}

func (d *semanticDocument) hoverForToken(token *ridl.TokenNode, pos protocol.Position, build func() string) *hoverMatch {
	if !d.tokenContainsPosition(token, pos) {
		return nil
	}

	contents := build()
	if contents == "" {
		return nil
	}

	rng := d.tokenRange(token, pos)
	return &hoverMatch{contents: contents, rng: rng}
}

func (d *semanticDocument) tokenContainsPosition(token *ridl.TokenNode, pos protocol.Position) bool {
	if token == nil {
		return false
	}
	rng, ok := d.tokenRangeInContent(token, &pos)
	return ok && pos.Line == rng.Start.Line && pos.Character >= rng.Start.Character && pos.Character < rng.End.Character
}

func (d *semanticDocument) tokenRange(token *ridl.TokenNode, hint protocol.Position) protocol.Range {
	if rng, ok := d.tokenRangeInContent(token, &hint); ok {
		return rng
	}
	return fallbackTokenRange(token)
}

func (d *semanticDocument) tokenRangeInContent(token *ridl.TokenNode, hint *protocol.Position) (protocol.Range, bool) {
	if token == nil || token.Line() <= 0 {
		return protocol.Range{}, false
	}

	lineIndex := token.Line() - 1
	line, ok := d.lineText(lineIndex)
	if !ok {
		return protocol.Range{}, false
	}

	tokenText := []rune(token.String())
	if len(tokenText) == 0 {
		return protocol.Range{}, false
	}

	lineRunes := []rune(line)
	occurrences := tokenOccurrences(lineRunes, tokenText)
	if len(occurrences) == 0 {
		return protocol.Range{}, false
	}

	start := -1
	if hint != nil && hint.Line == uint32(lineIndex) {
		for _, occ := range occurrences {
			if hint.Character >= uint32(occ) && hint.Character < uint32(occ+len(tokenText)) {
				start = occ
				break
			}
		}
	}

	if start < 0 && token.Col() > 0 {
		expectedEnd := token.Col()
		expectedStart := expectedEnd - len(tokenText)
		if expectedStart >= 0 && expectedEnd <= len(lineRunes) {
			if string(lineRunes[expectedStart:expectedEnd]) == string(tokenText) {
				start = expectedStart
			}
		}
	}

	if start < 0 && len(occurrences) == 1 {
		start = occurrences[0]
	}

	if start < 0 && token.Col() > 0 {
		bestDistance := -1
		for _, occ := range occurrences {
			distance := occ + len(tokenText) - token.Col()
			if distance < 0 {
				distance = -distance
			}
			if bestDistance == -1 || distance < bestDistance {
				bestDistance = distance
				start = occ
			}
		}
	}

	if start < 0 {
		start = occurrences[0]
	}

	return protocol.Range{
		Start: protocol.Position{
			Line:      uint32(lineIndex),
			Character: uint32(start),
		},
		End: protocol.Position{
			Line:      uint32(lineIndex),
			Character: uint32(start + len(tokenText)),
		},
	}, true
}

func (d *semanticDocument) identifierAtTokenPosition(token *ridl.TokenNode, pos protocol.Position) string {
	rng, ok := d.tokenRangeInContent(token, &pos)
	if token == nil || !ok || pos.Line != rng.Start.Line || pos.Character < rng.Start.Character || pos.Character >= rng.End.Character {
		return ""
	}

	value := []rune(token.String())
	if len(value) == 0 {
		return ""
	}

	offset := int(pos.Character - rng.Start.Character)
	if offset < 0 || offset >= len(value) || !isIdentifierRune(value[offset]) {
		return ""
	}

	start := offset
	for start > 0 && isIdentifierRune(value[start-1]) {
		start--
	}

	end := offset
	for end+1 < len(value) && isIdentifierRune(value[end+1]) {
		end++
	}

	return string(value[start : end+1])
}

func (d *semanticDocument) typeByName(name string) *schema.Type {
	if d == nil || d.result == nil || d.result.Schema == nil {
		return nil
	}
	return d.result.Schema.GetTypeByName(name)
}

func (d *semanticDocument) errorByName(name string) *schema.Error {
	if d == nil || d.result == nil || d.result.Schema == nil {
		return nil
	}
	for _, schemaError := range d.result.Schema.Errors {
		if strings.EqualFold(schemaError.Name, name) {
			return schemaError
		}
	}
	return nil
}

func (d *semanticDocument) methodByName(service *schema.Service, name string) *schema.Method {
	if service == nil {
		return nil
	}
	for _, method := range service.Methods {
		if strings.EqualFold(method.Name, name) {
			return method
		}
	}
	return nil
}

func (d *semanticDocument) formatTypeTokenHover(token *ridl.TokenNode, pos protocol.Position) string {
	name := d.identifierAtTokenPosition(token, pos)
	if typ := d.typeByName(name); typ != nil {
		return formatTypeHover(typ)
	}
	return formatTypeExprHover(d.result.Schema, token.String())
}

func (d *semanticDocument) lineText(targetLine int) (string, bool) {
	if targetLine < 0 {
		return "", false
	}

	currentLine := 0
	start := 0
	for i, r := range d.content {
		if r != '\n' {
			continue
		}
		if currentLine == targetLine {
			return d.content[start:i], true
		}
		currentLine++
		start = i + 1
	}

	if currentLine == targetLine {
		return d.content[start:], true
	}
	return "", false
}

func fallbackTokenRange(token *ridl.TokenNode) protocol.Range {
	width := uint32(utf8.RuneCountInString(token.String()))
	startChar := uint32(token.Col()) - width
	start := protocol.Position{
		Line:      uint32(token.Line() - 1),
		Character: startChar,
	}
	end := protocol.Position{
		Line:      start.Line,
		Character: start.Character + width,
	}
	return protocol.Range{Start: start, End: end}
}

func tokenOccurrences(line, token []rune) []int {
	if len(token) == 0 || len(token) > len(line) {
		return nil
	}

	occurrences := make([]int, 0, 1)
	for i := 0; i <= len(line)-len(token); i++ {
		if string(line[i:i+len(token)]) == string(token) && tokenOccurrenceHasBoundaries(line, token, i) {
			occurrences = append(occurrences, i)
		}
	}
	return occurrences
}

func tokenOccurrenceHasBoundaries(line, token []rune, start int) bool {
	end := start + len(token)
	if isIdentifierRune(token[0]) && start > 0 && isIdentifierRune(line[start-1]) {
		return false
	}
	if isIdentifierRune(token[len(token)-1]) && end < len(line) && isIdentifierRune(line[end]) {
		return false
	}
	return true
}

func isIdentifierRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}
