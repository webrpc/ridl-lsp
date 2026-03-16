package lsp

import (
	"sort"
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

type completionContext int

const (
	completionContextNone completionContext = iota
	completionContextTopLevel
	completionContextEnumType
	completionContextType
)

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

func (d *semanticDocument) completionItemsAt(pos protocol.Position) []protocol.CompletionItem {
	ctx, prefix := d.completionContextAt(pos)

	switch ctx {
	case completionContextTopLevel:
		return keywordCompletionItems(prefix)
	case completionContextEnumType:
		return enumTypeCompletionItems(prefix)
	case completionContextType:
		return d.typeCompletionItems(prefix)
	default:
		return []protocol.CompletionItem{}
	}
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

func (d *semanticDocument) completionContextAt(pos protocol.Position) (completionContext, string) {
	line, ok := d.lineText(int(pos.Line))
	if !ok {
		return completionContextNone, ""
	}

	lineRunes := []rune(line)
	char := int(pos.Character)
	if char < 0 {
		char = 0
	}
	if char > len(lineRunes) {
		char = len(lineRunes)
	}

	before := completionPrefix(string(lineRunes[:char]))
	trimmed := strings.TrimSpace(before)
	indent := leadingIndentWidth(before)

	if indent == 0 && looksLikeTopLevelContext(trimmed) {
		return completionContextTopLevel, trailingIdentifierFragment(trimmed)
	}

	if looksLikeEnumTypeContext(trimmed) {
		return completionContextEnumType, trailingIdentifierFragment(trimmed)
	}

	if looksLikeTypeContext(before) {
		return completionContextType, trailingIdentifierFragment(before)
	}

	return completionContextNone, ""
}

func (d *semanticDocument) typeCompletionItems(prefix string) []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(coreTypeCompletions)+len(d.schemaTypeCompletions()))
	items = append(items, filterCompletionItems(coreTypeCompletions, prefix)...)
	items = append(items, d.schemaTypeCompletions()...)
	items = filterCompletionItems(items, prefix)
	return dedupeCompletionItems(items)
}

func (d *semanticDocument) schemaTypeCompletions() []protocol.CompletionItem {
	if d == nil || d.result == nil || d.result.Schema == nil {
		return nil
	}

	items := make([]protocol.CompletionItem, 0, len(d.result.Schema.Types))
	for _, typ := range d.result.Schema.Types {
		kind := protocol.CompletionItemKindStruct
		if typ.Kind == schema.TypeKind_Enum {
			kind = protocol.CompletionItemKindEnum
		}

		items = append(items, protocol.CompletionItem{
			Label:         typ.Name,
			Kind:          kind,
			Detail:        typ.Kind,
			InsertText:    typ.Name,
			SortText:      "2_" + strings.ToLower(typ.Name),
			FilterText:    typ.Name,
			Documentation: typeCompletionDocumentation(typ),
		})
	}
	return items
}

func keywordCompletionItems(prefix string) []protocol.CompletionItem {
	return filterCompletionItems(topLevelKeywordCompletions, prefix)
}

func enumTypeCompletionItems(prefix string) []protocol.CompletionItem {
	return filterCompletionItems(enumTypeCompletions, prefix)
}

func leadingIndentWidth(s string) int {
	width := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			width++
			continue
		}
		break
	}
	return width
}

func looksLikeTopLevelContext(trimmed string) bool {
	if trimmed == "" {
		return true
	}
	if strings.Contains(trimmed, "=") {
		return false
	}
	return len(strings.Fields(trimmed)) <= 1
}

func looksLikeEnumTypeContext(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "enum ") {
		return false
	}

	_, after, ok := strings.Cut(trimmed, ":")
	if !ok {
		return false
	}

	return !hasCompletedTypeExpr(after)
}

func looksLikeTypeContext(before string) bool {
	trimmed := strings.TrimSpace(before)
	if trimmed == "" {
		return false
	}

	segment := activeTypeSegment(before)
	namePart, typePart, ok := strings.Cut(segment, ":")
	if !ok {
		return false
	}

	name := typeTargetName(namePart)
	if name == "" {
		return false
	}

	return !hasCompletedTypeExpr(typePart)
}

func trailingIdentifierFragment(s string) string {
	runes := []rune(s)
	end := len(runes)
	start := end
	for start > 0 && isIdentifierRune(runes[start-1]) {
		start--
	}
	if start == end {
		return ""
	}
	return string(runes[start:end])
}

func filterCompletionItems(items []protocol.CompletionItem, prefix string) []protocol.CompletionItem {
	if prefix == "" {
		return append([]protocol.CompletionItem(nil), items...)
	}

	filtered := make([]protocol.CompletionItem, 0, len(items))
	for _, item := range items {
		target := item.FilterText
		if target == "" {
			target = item.Label
		}
		if strings.HasPrefix(strings.ToLower(target), strings.ToLower(prefix)) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func dedupeCompletionItems(items []protocol.CompletionItem) []protocol.CompletionItem {
	if len(items) == 0 {
		return items
	}

	seen := make(map[string]protocol.CompletionItem, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(item.Label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = item
		order = append(order, key)
	}

	sort.Strings(order)
	out := make([]protocol.CompletionItem, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
	}
	return out
}

func typeCompletionDocumentation(typ *schema.Type) string {
	if typ == nil {
		return ""
	}
	return typ.Kind + " " + typ.Name
}

func completionPrefix(before string) string {
	if idx := strings.Index(before, "#"); idx >= 0 {
		return before[:idx]
	}
	return before
}

func activeTypeSegment(before string) string {
	runes := []rune(before)
	start := 0
	angleDepth := 0
	squareDepth := 0
	parenDepth := 0

	for i, r := range runes {
		switch r {
		case '<':
			angleDepth++
		case '>':
			if angleDepth > 0 {
				angleDepth--
			}
		case '[':
			squareDepth++
		case ']':
			if squareDepth > 0 {
				squareDepth--
			}
		case '(':
			if angleDepth == 0 && squareDepth == 0 {
				parenDepth++
				start = i + 1
			}
		case ')':
			if angleDepth == 0 && squareDepth == 0 && parenDepth > 0 {
				parenDepth--
				start = i + 1
			}
		case ',':
			if angleDepth == 0 && squareDepth == 0 {
				start = i + 1
			}
		}
	}

	return string(runes[start:])
}

func typeTargetName(beforeColon string) string {
	trimmed := strings.TrimSpace(beforeColon)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "-") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}

	name := fields[len(fields)-1]
	name = strings.TrimSuffix(name, "?")
	if name == "" {
		return ""
	}

	for _, r := range name {
		if !isIdentifierRune(r) {
			return ""
		}
	}

	return name
}

func hasCompletedTypeExpr(afterColon string) bool {
	runes := []rune(afterColon)
	angleDepth := 0
	squareDepth := 0
	parenDepth := 0

	for _, r := range runes {
		switch r {
		case '<':
			angleDepth++
		case '>':
			if angleDepth > 0 {
				angleDepth--
			}
		case '[':
			squareDepth++
		case ']':
			if squareDepth > 0 {
				squareDepth--
			}
		case '(':
			if angleDepth == 0 && squareDepth == 0 {
				parenDepth++
			}
		case ')':
			if angleDepth == 0 && squareDepth == 0 && parenDepth > 0 {
				parenDepth--
			}
			if angleDepth == 0 && squareDepth == 0 && parenDepth == 0 {
				return true
			}
		case ',':
			if angleDepth == 0 && squareDepth == 0 && parenDepth == 0 {
				return true
			}
		}
	}

	return false
}

var topLevelKeywordCompletions = []protocol.CompletionItem{
	keywordCompletionItem("webrpc", "schema version declaration"),
	keywordCompletionItem("name", "schema name declaration"),
	keywordCompletionItem("version", "schema version declaration"),
	keywordCompletionItem("basepath", "basepath declaration"),
	keywordCompletionItem("import", "import other RIDL files"),
	keywordCompletionItem("struct", "declare a struct"),
	keywordCompletionItem("enum", "declare an enum"),
	keywordCompletionItem("error", "declare an error"),
	keywordCompletionItem("service", "declare a service"),
}

var coreTypeCompletions = []protocol.CompletionItem{
	typeCompletionItem("null", "core type"),
	typeCompletionItem("any", "core type"),
	typeCompletionItem("byte", "core type"),
	typeCompletionItem("bool", "core type"),
	typeCompletionItem("uint", "core type"),
	typeCompletionItem("uint8", "core type"),
	typeCompletionItem("uint16", "core type"),
	typeCompletionItem("uint32", "core type"),
	typeCompletionItem("uint64", "core type"),
	typeCompletionItem("int", "core type"),
	typeCompletionItem("int8", "core type"),
	typeCompletionItem("int16", "core type"),
	typeCompletionItem("int32", "core type"),
	typeCompletionItem("int64", "core type"),
	typeCompletionItem("bigint", "core type"),
	typeCompletionItem("float32", "core type"),
	typeCompletionItem("float64", "core type"),
	typeCompletionItem("string", "core type"),
	typeCompletionItem("timestamp", "core type"),
	typeCompletionItem("map", "map type"),
}

var enumTypeCompletions = []protocol.CompletionItem{
	typeCompletionItem("uint", "enum base type"),
	typeCompletionItem("uint8", "enum base type"),
	typeCompletionItem("uint16", "enum base type"),
	typeCompletionItem("uint32", "enum base type"),
	typeCompletionItem("uint64", "enum base type"),
	typeCompletionItem("int", "enum base type"),
	typeCompletionItem("int8", "enum base type"),
	typeCompletionItem("int16", "enum base type"),
	typeCompletionItem("int32", "enum base type"),
	typeCompletionItem("int64", "enum base type"),
	typeCompletionItem("string", "enum base type"),
}

func keywordCompletionItem(label, detail string) protocol.CompletionItem {
	return protocol.CompletionItem{
		Label:      label,
		Kind:       protocol.CompletionItemKindKeyword,
		Detail:     detail,
		InsertText: label,
		SortText:   "1_" + label,
		FilterText: label,
	}
}

func typeCompletionItem(label, detail string) protocol.CompletionItem {
	return protocol.CompletionItem{
		Label:      label,
		Kind:       protocol.CompletionItemKindKeyword,
		Detail:     detail,
		InsertText: label,
		SortText:   "1_" + label,
		FilterText: label,
	}
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
