package lsp

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	"github.com/webrpc/webrpc/schema"
	ridl "github.com/webrpc/webrpc/schema/ridl"
)

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok || doc.Result == nil || doc.Result.Root == nil || doc.Result.Schema == nil {
		return nil, nil
	}

	match := hoverAtPosition(doc.Content, doc.Result, params.Position)
	if match == nil {
		return nil, nil
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: match.contents,
		},
		Range: &match.rng,
	}, nil
}

type hoverMatch struct {
	contents string
	rng      protocol.Range
}

func hoverAtPosition(content string, result *ridl.ParseResult, pos protocol.Position) *hoverMatch {
	root := result.Root
	if root == nil || result.Schema == nil {
		return nil
	}

	for _, enumNode := range root.Enums() {
		if match := hoverForToken(content, enumNode.Name(), pos, func() string {
			return formatTypeHover(typeByName(result.Schema, enumNode.Name().String()))
		}); match != nil {
			return match
		}

		if match := hoverForToken(content, enumNode.TypeName(), pos, func() string {
			return formatTypeTokenHover(content, result.Schema, enumNode.TypeName(), pos)
		}); match != nil {
			return match
		}

		for _, value := range enumNode.Values() {
			if match := hoverForToken(content, value.Left(), pos, func() string {
				return formatEnumValueHover(typeByName(result.Schema, enumNode.Name().String()), value.Left().String())
			}); match != nil {
				return match
			}

			if match := hoverForToken(content, value.Right(), pos, func() string {
				return formatTypeTokenHover(content, result.Schema, value.Right(), pos)
			}); match != nil {
				return match
			}
		}
	}

	for _, structNode := range root.Structs() {
		if match := hoverForToken(content, structNode.Name(), pos, func() string {
			return formatTypeHover(typeByName(result.Schema, structNode.Name().String()))
		}); match != nil {
			return match
		}

		structType := typeByName(result.Schema, structNode.Name().String())
		for _, field := range structNode.Fields() {
			if match := hoverForToken(content, field.Left(), pos, func() string {
				return formatFieldHover(structType, field.Left().String())
			}); match != nil {
				return match
			}

			if match := hoverForToken(content, field.Right(), pos, func() string {
				return formatTypeTokenHover(content, result.Schema, field.Right(), pos)
			}); match != nil {
				return match
			}
		}
	}

	for _, errorNode := range root.Errors() {
		if match := hoverForToken(content, errorNode.Name(), pos, func() string {
			return formatErrorHover(errorByName(result.Schema, errorNode.Name().String()))
		}); match != nil {
			return match
		}
	}

	for _, serviceNode := range root.Services() {
		service := result.Schema.GetServiceByName(serviceNode.Name().String())

		if match := hoverForToken(content, serviceNode.Name(), pos, func() string {
			return formatServiceHover(service)
		}); match != nil {
			return match
		}

		for _, methodNode := range serviceNode.Methods() {
			method := methodByName(service, methodNode.Name().String())

			if match := hoverForToken(content, methodNode.Name(), pos, func() string {
				return formatMethodHover(method)
			}); match != nil {
				return match
			}

			for _, input := range methodNode.Inputs() {
				if match := hoverForToken(content, input.Name(), pos, func() string {
					return formatMethodArgumentHover(method, input.Name().String(), true)
				}); match != nil {
					return match
				}

				if match := hoverForToken(content, input.TypeName(), pos, func() string {
					return formatTypeTokenHover(content, result.Schema, input.TypeName(), pos)
				}); match != nil {
					return match
				}
			}

			for _, output := range methodNode.Outputs() {
				if match := hoverForToken(content, output.Name(), pos, func() string {
					return formatMethodArgumentHover(method, output.Name().String(), false)
				}); match != nil {
					return match
				}

				if match := hoverForToken(content, output.TypeName(), pos, func() string {
					return formatTypeTokenHover(content, result.Schema, output.TypeName(), pos)
				}); match != nil {
					return match
				}
			}

			for _, errorToken := range methodNode.Errors() {
				if match := hoverForToken(content, errorToken, pos, func() string {
					return formatErrorHover(errorByName(result.Schema, errorToken.String()))
				}); match != nil {
					return match
				}
			}
		}
	}

	return nil
}

func hoverForToken(content string, token *ridl.TokenNode, pos protocol.Position, build func() string) *hoverMatch {
	if !tokenContainsPosition(content, token, pos) {
		return nil
	}

	contents := build()
	if contents == "" {
		return nil
	}

	rng := tokenRangeForContent(content, token, pos)
	return &hoverMatch{contents: contents, rng: rng}
}

func tokenContainsPosition(content string, token *ridl.TokenNode, pos protocol.Position) bool {
	if token == nil {
		return false
	}
	rng, ok := tokenRangeInContent(content, token, &pos)
	return ok && pos.Line == rng.Start.Line && pos.Character >= rng.Start.Character && pos.Character < rng.End.Character
}

func tokenRange(token *ridl.TokenNode) protocol.Range {
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

func tokenRangeForContent(content string, token *ridl.TokenNode, hint protocol.Position) protocol.Range {
	if rng, ok := tokenRangeInContent(content, token, &hint); ok {
		return rng
	}
	return tokenRange(token)
}

func tokenRangeInContent(content string, token *ridl.TokenNode, hint *protocol.Position) (protocol.Range, bool) {
	if token == nil || token.Line() <= 0 {
		return protocol.Range{}, false
	}

	lineIndex := token.Line() - 1
	line, ok := lineText(content, lineIndex)
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

func lineText(content string, targetLine int) (string, bool) {
	if targetLine < 0 {
		return "", false
	}

	currentLine := 0
	start := 0
	for i, r := range content {
		if r != '\n' {
			continue
		}
		if currentLine == targetLine {
			return content[start:i], true
		}
		currentLine++
		start = i + 1
	}

	if currentLine == targetLine {
		return content[start:], true
	}
	return "", false
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

func typeByName(schemaDoc *schema.WebRPCSchema, name string) *schema.Type {
	if schemaDoc == nil {
		return nil
	}
	return schemaDoc.GetTypeByName(name)
}

func errorByName(schemaDoc *schema.WebRPCSchema, name string) *schema.Error {
	if schemaDoc == nil {
		return nil
	}
	for _, schemaError := range schemaDoc.Errors {
		if strings.EqualFold(schemaError.Name, name) {
			return schemaError
		}
	}
	return nil
}

func methodByName(service *schema.Service, name string) *schema.Method {
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

func formatTypeHover(typ *schema.Type) string {
	if typ == nil {
		return ""
	}

	lines := []string{fmt.Sprintf("%s %s", typ.Kind, typ.Name)}
	if typ.Kind == schema.TypeKind_Enum && typ.Type != nil {
		lines[0] = fmt.Sprintf("enum %s %s", typ.Name, typ.Type.String())
	}

	for _, field := range typ.Fields {
		switch typ.Kind {
		case schema.TypeKind_Struct:
			lines = append(lines, fmt.Sprintf("  - %s%s: %s", field.Name, optionalSuffix(field.Optional), field.Type.String()))
		case schema.TypeKind_Enum:
			lines = append(lines, fmt.Sprintf("  - %s = %s", field.Name, field.Value))
		}
	}

	return markdownWithNotes(strings.Join(lines, "\n"), typ.Comments)
}

func formatFieldHover(typ *schema.Type, fieldName string) string {
	if typ == nil {
		return ""
	}
	for _, field := range typ.Fields {
		if strings.EqualFold(field.Name, fieldName) {
			signature := fmt.Sprintf("%s%s: %s", field.Name, optionalSuffix(field.Optional), field.Type.String())
			notes := append([]string{fmt.Sprintf("Field of `%s`.", typ.Name)}, field.Comments...)
			return markdownWithNotes(signature, notes)
		}
	}
	return ""
}

func formatServiceHover(service *schema.Service) string {
	if service == nil {
		return ""
	}

	lines := []string{fmt.Sprintf("service %s", service.Name)}
	for _, method := range service.Methods {
		lines = append(lines, "  - "+methodSignature(method))
	}
	return markdownWithNotes(strings.Join(lines, "\n"), service.Comments)
}

func formatMethodHover(method *schema.Method) string {
	if method == nil {
		return ""
	}
	return markdownWithNotes(methodSignature(method), method.Comments)
}

func formatMethodArgumentHover(method *schema.Method, name string, input bool) string {
	if method == nil {
		return ""
	}

	args := method.Outputs
	kind := "Output"
	if input {
		args = method.Inputs
		kind = "Input"
	}

	for _, arg := range args {
		if strings.EqualFold(arg.Name, name) {
			note := fmt.Sprintf("%s of `%s`.", kind, method.Name)
			return markdownWithNotes(fmt.Sprintf("%s%s: %s", arg.Name, optionalSuffix(arg.Optional), arg.Type.String()), []string{note})
		}
	}
	return ""
}

func formatErrorHover(schemaError *schema.Error) string {
	if schemaError == nil {
		return ""
	}
	signature := fmt.Sprintf("error %d %s %q HTTP %d", schemaError.Code, schemaError.Name, schemaError.Message, schemaError.HTTPStatus)
	return markdownWithNotes(signature, nil)
}

func formatEnumValueHover(typ *schema.Type, valueName string) string {
	if typ == nil || typ.Kind != schema.TypeKind_Enum {
		return ""
	}

	for _, field := range typ.Fields {
		if strings.EqualFold(field.Name, valueName) {
			return markdownWithNotes(fmt.Sprintf("%s = %s", field.Name, field.Value), field.Comments)
		}
	}
	return ""
}

func formatTypeExprHover(schemaDoc *schema.WebRPCSchema, expr string) string {
	if expr == "" {
		return ""
	}

	var varType schema.VarType
	if err := schema.ParseVarTypeExpr(schemaDoc, expr, &varType); err == nil {
		if varType.Struct != nil {
			return formatTypeHover(varType.Struct.Type)
		}
		if varType.Enum != nil {
			return formatTypeHover(varType.Enum.Type)
		}
	}

	notes := []string{fmt.Sprintf("RIDL type expression `%s`.", expr)}
	if coreType, ok := schema.CoreTypeFromString[expr]; ok {
		if warning, ok := schema.CoreTypeWebWarnings[coreType]; ok {
			notes = append(notes, warning)
		}
	}
	return markdownWithNotes(expr, notes)
}

func formatTypeTokenHover(content string, schemaDoc *schema.WebRPCSchema, token *ridl.TokenNode, pos protocol.Position) string {
	name := identifierAtTokenPosition(content, token, pos)
	if typ := typeByName(schemaDoc, name); typ != nil {
		return formatTypeHover(typ)
	}
	return formatTypeExprHover(schemaDoc, token.String())
}

func methodSignature(method *schema.Method) string {
	if method == nil {
		return ""
	}

	var b strings.Builder

	if method.StreamInput {
		b.WriteString("stream ")
	} else if method.Proxy {
		b.WriteString("proxy ")
	}

	b.WriteString(method.Name)
	b.WriteString("(")
	b.WriteString(joinMethodArguments(method.Inputs))
	b.WriteString(")")

	if len(method.Outputs) > 0 || method.StreamOutput {
		b.WriteString(" => ")
		if method.StreamOutput {
			b.WriteString("stream ")
		}
		b.WriteString("(")
		b.WriteString(joinMethodArguments(method.Outputs))
		b.WriteString(")")
	}

	if len(method.Errors) > 0 {
		b.WriteString(" errors ")
		b.WriteString(strings.Join(method.Errors, ", "))
	}

	return b.String()
}

func joinMethodArguments(args []*schema.MethodArgument) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%s%s: %s", arg.Name, optionalSuffix(arg.Optional), arg.Type.String()))
	}
	return strings.Join(parts, ", ")
}

func optionalSuffix(optional bool) string {
	if optional {
		return "?"
	}
	return ""
}

func markdownWithNotes(signature string, notes []string) string {
	content := "```ridl\n" + signature + "\n```"
	if len(notes) == 0 {
		return content
	}

	filtered := make([]string, 0, len(notes))
	for _, note := range notes {
		if strings.TrimSpace(note) == "" {
			continue
		}
		filtered = append(filtered, note)
	}
	if len(filtered) == 0 {
		return content
	}

	return content + "\n\n" + strings.Join(filtered, "\n\n")
}
