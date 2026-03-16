package lsp

import (
	"context"
	"sort"
	"strings"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

var semanticTokenLegendTypes = []protocol.SemanticTokenTypes{
	protocol.SemanticTokenKeyword,
	protocol.SemanticTokenStruct,
	protocol.SemanticTokenEnum,
	protocol.SemanticTokenInterface,
	protocol.SemanticTokenMethod,
	protocol.SemanticTokenProperty,
	protocol.SemanticTokenParameter,
	protocol.SemanticTokenEnumMember,
	protocol.SemanticTokenType,
	protocol.SemanticTokenClass,
}

var semanticTokenLegendModifiers = []protocol.SemanticTokenModifiers{
	protocol.SemanticTokenModifierDeclaration,
	protocol.SemanticTokenModifierDefaultLibrary,
}

var semanticTokenTypeIndex = semanticTokenIndexMap(semanticTokenLegendTypes)
var semanticTokenModifierIndex = semanticTokenIndexMap(semanticTokenLegendModifiers)

func (s *Server) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	return &protocol.SemanticTokens{
		Data: semanticDoc.semanticTokensData(),
	}, nil
}

func (s *Server) SemanticTokensRange(ctx context.Context, params *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	return &protocol.SemanticTokens{
		Data: semanticDoc.semanticTokensDataInRange(params.Range),
	}, nil
}

type semanticTokenEntry struct {
	rng       protocol.Range
	tokenType protocol.SemanticTokenTypes
	modifiers uint32
}

func (d *semanticDocument) semanticTokensData() []uint32 {
	return semanticTokenData(d.semanticTokenEntries())
}

func (d *semanticDocument) semanticTokensDataInRange(rng protocol.Range) []uint32 {
	entries := d.semanticTokenEntries()
	if len(entries) == 0 {
		return []uint32{}
	}

	expanded := d.expandSemanticTokenRange(rng)

	filtered := make([]semanticTokenEntry, 0, len(entries))
	for _, entry := range entries {
		if rangesOverlap(entry.rng, rng) {
			filtered = append(filtered, entry)
			continue
		}
		if rangesOverlap(entry.rng, expanded) && includeExpandedSemanticToken(entry) {
			filtered = append(filtered, entry)
		}
	}

	return semanticTokenData(filtered)
}

func includeExpandedSemanticToken(entry semanticTokenEntry) bool {
	if entry.modifiers&semanticTokenModifierBits(protocol.SemanticTokenModifierDeclaration) != 0 {
		return true
	}

	switch entry.tokenType {
	case protocol.SemanticTokenMethod, protocol.SemanticTokenParameter, protocol.SemanticTokenProperty, protocol.SemanticTokenEnumMember:
		return true
	default:
		return false
	}
}

func (d *semanticDocument) expandSemanticTokenRange(rng protocol.Range) protocol.Range {
	lines := strings.Split(d.content, "\n")
	endLine := int(rng.End.Line)
	if endLine < 0 || endLine >= len(lines) {
		return rng
	}

	if !isSemanticTokenBlockStart(trimmedLine(lines[endLine])) {
		return rng
	}

	lastLine := endLine
	for i := endLine + 1; i < len(lines); i++ {
		trimmed := trimmedLine(lines[i])
		if trimmed == "" {
			lastLine = i
			continue
		}
		if leadingIndentWidth(lines[i]) == 0 {
			break
		}
		lastLine = i
	}

	return protocol.Range{
		Start: rng.Start,
		End: protocol.Position{
			Line:      uint32(lastLine),
			Character: lineEndCharacter(lines[lastLine]),
		},
	}
}

func isSemanticTokenBlockStart(trimmed string) bool {
	return strings.HasPrefix(trimmed, "service ") ||
		strings.HasPrefix(trimmed, "struct ") ||
		strings.HasPrefix(trimmed, "enum ") ||
		strings.HasPrefix(trimmed, "error ") ||
		trimmed == "import" ||
		strings.HasPrefix(trimmed, "import ")
}

func semanticTokenData(entries []semanticTokenEntry) []uint32 {
	if len(entries) == 0 {
		return []uint32{}
	}

	data := make([]uint32, 0, len(entries)*5)
	var prevLine uint32
	var prevChar uint32
	for i, entry := range entries {
		line := entry.rng.Start.Line
		char := entry.rng.Start.Character
		length := entry.rng.End.Character - entry.rng.Start.Character

		deltaLine := line
		deltaStart := char
		if i > 0 {
			deltaLine = line - prevLine
			if deltaLine == 0 {
				deltaStart = char - prevChar
			}
		}

		data = append(data,
			deltaLine,
			deltaStart,
			length,
			uint32(semanticTokenTypeIndex[entry.tokenType]),
			entry.modifiers,
		)

		prevLine = line
		prevChar = char
	}

	return data
}

func (d *semanticDocument) semanticTokenEntries() []semanticTokenEntry {
	if !d.valid() {
		return nil
	}

	entries := make([]semanticTokenEntry, 0, 32)
	entries = append(entries, d.keywordSemanticTokens()...)

	addToken := func(rng protocol.Range, tokenType protocol.SemanticTokenTypes, modifiers ...protocol.SemanticTokenModifiers) {
		if rng.End.Line != rng.Start.Line || rng.End.Character <= rng.Start.Character {
			return
		}
		entries = append(entries, semanticTokenEntry{
			rng:       rng,
			tokenType: tokenType,
			modifiers: semanticTokenModifierBits(modifiers...),
		})
	}

	for _, enumNode := range d.result.Root.Enums() {
		addToken(d.tokenRange(enumNode.Name(), protocol.Position{}), protocol.SemanticTokenEnum, protocol.SemanticTokenModifierDeclaration)
		for _, token := range d.typeExprSemanticTokens(enumNode.TypeName()) {
			entries = append(entries, token)
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		addToken(d.tokenRange(structNode.Name(), protocol.Position{}), protocol.SemanticTokenStruct, protocol.SemanticTokenModifierDeclaration)
		for _, field := range structNode.Fields() {
			for _, token := range d.typeExprSemanticTokens(field.Right()) {
				entries = append(entries, token)
			}
		}
	}

	for _, errorNode := range d.result.Root.Errors() {
		addToken(d.tokenRange(errorNode.Name(), protocol.Position{}), protocol.SemanticTokenClass, protocol.SemanticTokenModifierDeclaration)
	}

	for _, serviceNode := range d.result.Root.Services() {
		addToken(d.tokenRange(serviceNode.Name(), protocol.Position{}), protocol.SemanticTokenInterface, protocol.SemanticTokenModifierDeclaration)
		for _, methodNode := range serviceNode.Methods() {
			addToken(d.tokenRange(methodNode.Name(), protocol.Position{}), protocol.SemanticTokenMethod, protocol.SemanticTokenModifierDeclaration)
			for _, input := range methodNode.Inputs() {
				if validSymbolToken(input.Name()) {
					addToken(d.tokenRange(input.Name(), protocol.Position{}), protocol.SemanticTokenParameter, protocol.SemanticTokenModifierDeclaration)
				}
				for _, token := range d.typeExprSemanticTokens(input.TypeName()) {
					entries = append(entries, token)
				}
			}
			for _, output := range methodNode.Outputs() {
				if validSymbolToken(output.Name()) {
					addToken(d.tokenRange(output.Name(), protocol.Position{}), protocol.SemanticTokenParameter, protocol.SemanticTokenModifierDeclaration)
				}
				for _, token := range d.typeExprSemanticTokens(output.TypeName()) {
					entries = append(entries, token)
				}
			}
			for _, errorToken := range methodNode.Errors() {
				if !validSymbolToken(errorToken) {
					continue
				}
				addToken(d.tokenRange(errorToken, protocol.Position{}), protocol.SemanticTokenClass)
			}
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].rng.Start.Line != entries[j].rng.Start.Line {
			return entries[i].rng.Start.Line < entries[j].rng.Start.Line
		}
		if entries[i].rng.Start.Character != entries[j].rng.Start.Character {
			return entries[i].rng.Start.Character < entries[j].rng.Start.Character
		}
		if entries[i].rng.End.Character != entries[j].rng.End.Character {
			return entries[i].rng.End.Character < entries[j].rng.End.Character
		}
		if entries[i].tokenType != entries[j].tokenType {
			return entries[i].tokenType < entries[j].tokenType
		}
		return entries[i].modifiers < entries[j].modifiers
	})

	return dedupeSemanticTokens(entries)
}

func (d *semanticDocument) keywordSemanticTokens() []semanticTokenEntry {
	lines := strings.Split(d.content, "\n")
	entries := make([]semanticTokenEntry, 0, 8)

	addKeyword := func(lineIndex, start int, keyword string) {
		entries = append(entries, semanticTokenEntry{
			rng: protocol.Range{
				Start: protocol.Position{Line: uint32(lineIndex), Character: uint32(start)},
				End:   protocol.Position{Line: uint32(lineIndex), Character: uint32(start + len([]rune(keyword)))},
			},
			tokenType: protocol.SemanticTokenKeyword,
		})
	}

	for i, line := range lines {
		indent := leadingIndentWidth(line)
		trimmed := strings.TrimSpace(line)
		if indent != 0 || trimmed == "" {
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "struct "):
			addKeyword(i, indent, "struct")
		case strings.HasPrefix(trimmed, "enum "):
			addKeyword(i, indent, "enum")
		case strings.HasPrefix(trimmed, "service "):
			addKeyword(i, indent, "service")
		case strings.HasPrefix(trimmed, "error "):
			addKeyword(i, indent, "error")
		case trimmed == "import" || strings.HasPrefix(trimmed, "import "):
			addKeyword(i, indent, "import")
		}
	}

	return entries
}

func (d *semanticDocument) typeExprSemanticTokens(token *ridl.TokenNode) []semanticTokenEntry {
	if !validSymbolToken(token) {
		return nil
	}

	rng, ok := d.tokenRangeInContent(token, nil)
	if !ok {
		return nil
	}

	line, ok := d.lineText(int(rng.Start.Line))
	if !ok {
		return nil
	}

	lineRunes := []rune(line)
	start := int(rng.Start.Character)
	end := int(rng.End.Character)
	if start < 0 || end > len(lineRunes) || start >= end {
		return nil
	}

	tokenRunes := lineRunes[start:end]
	entries := make([]semanticTokenEntry, 0, 4)

	for i := 0; i < len(tokenRunes); {
		if !isIdentifierRune(tokenRunes[i]) {
			i++
			continue
		}

		j := i + 1
		for j < len(tokenRunes) && isIdentifierRune(tokenRunes[j]) {
			j++
		}

		word := string(tokenRunes[i:j])
		tokenType, modifiers, ok := d.semanticTypeForWord(word)
		if ok {
			entries = append(entries, semanticTokenEntry{
				rng: protocol.Range{
					Start: protocol.Position{
						Line:      rng.Start.Line,
						Character: rng.Start.Character + uint32(i),
					},
					End: protocol.Position{
						Line:      rng.Start.Line,
						Character: rng.Start.Character + uint32(j),
					},
				},
				tokenType: tokenType,
				modifiers: modifiers,
			})
		}

		i = j
	}

	return entries
}

func (d *semanticDocument) semanticTypeForWord(word string) (protocol.SemanticTokenTypes, uint32, bool) {
	if _, ok := schemaCoreType(word); ok {
		return protocol.SemanticTokenType, semanticTokenModifierBits(protocol.SemanticTokenModifierDefaultLibrary), true
	}

	typ := d.typeByName(word)
	if typ == nil {
		return "", 0, false
	}

	switch typ.Kind {
	case "struct":
		return protocol.SemanticTokenStruct, 0, true
	case "enum":
		return protocol.SemanticTokenEnum, 0, true
	default:
		return protocol.SemanticTokenType, 0, true
	}
}

func dedupeSemanticTokens(entries []semanticTokenEntry) []semanticTokenEntry {
	if len(entries) == 0 {
		return entries
	}

	out := make([]semanticTokenEntry, 0, len(entries))
	var prev *semanticTokenEntry
	for i := range entries {
		entry := entries[i]
		if prev != nil &&
			prev.rng == entry.rng &&
			prev.tokenType == entry.tokenType &&
			prev.modifiers == entry.modifiers {
			continue
		}
		out = append(out, entry)
		prev = &out[len(out)-1]
	}
	return out
}

func semanticTokenModifierBits(modifiers ...protocol.SemanticTokenModifiers) uint32 {
	var bits uint32
	for _, modifier := range modifiers {
		idx, ok := semanticTokenModifierIndex[modifier]
		if !ok {
			continue
		}
		bits |= 1 << idx
	}
	return bits
}

func semanticTokenIndexMap[T comparable](values []T) map[T]int {
	index := make(map[T]int, len(values))
	for i, value := range values {
		index[value] = i
	}
	return index
}
