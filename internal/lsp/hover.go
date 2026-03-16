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

	match := hoverAtPosition(doc.Result, params.Position)
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

func hoverAtPosition(result *ridl.ParseResult, pos protocol.Position) *hoverMatch {
	root := result.Root
	if root == nil || result.Schema == nil {
		return nil
	}

	for _, enumNode := range root.Enums() {
		if match := hoverForToken(enumNode.Name(), pos, func() string {
			return formatTypeHover(typeByName(result.Schema, enumNode.Name().String()))
		}); match != nil {
			return match
		}

		if match := hoverForToken(enumNode.TypeName(), pos, func() string {
			return formatTypeExprHover(result.Schema, enumNode.TypeName().String())
		}); match != nil {
			return match
		}

		for _, value := range enumNode.Values() {
			if match := hoverForToken(value.Left(), pos, func() string {
				return formatEnumValueHover(typeByName(result.Schema, enumNode.Name().String()), value.Left().String())
			}); match != nil {
				return match
			}

			if match := hoverForToken(value.Right(), pos, func() string {
				return formatTypeExprHover(result.Schema, value.Right().String())
			}); match != nil {
				return match
			}
		}
	}

	for _, structNode := range root.Structs() {
		if match := hoverForToken(structNode.Name(), pos, func() string {
			return formatTypeHover(typeByName(result.Schema, structNode.Name().String()))
		}); match != nil {
			return match
		}

		structType := typeByName(result.Schema, structNode.Name().String())
		for _, field := range structNode.Fields() {
			if match := hoverForToken(field.Left(), pos, func() string {
				return formatFieldHover(structType, field.Left().String())
			}); match != nil {
				return match
			}

			if match := hoverForToken(field.Right(), pos, func() string {
				return formatTypeExprHover(result.Schema, field.Right().String())
			}); match != nil {
				return match
			}
		}
	}

	for _, errorNode := range root.Errors() {
		if match := hoverForToken(errorNode.Name(), pos, func() string {
			return formatErrorHover(errorByName(result.Schema, errorNode.Name().String()))
		}); match != nil {
			return match
		}
	}

	for _, serviceNode := range root.Services() {
		service := result.Schema.GetServiceByName(serviceNode.Name().String())

		if match := hoverForToken(serviceNode.Name(), pos, func() string {
			return formatServiceHover(service)
		}); match != nil {
			return match
		}

		for _, methodNode := range serviceNode.Methods() {
			method := methodByName(service, methodNode.Name().String())

			if match := hoverForToken(methodNode.Name(), pos, func() string {
				return formatMethodHover(method)
			}); match != nil {
				return match
			}

			for _, input := range methodNode.Inputs() {
				if match := hoverForToken(input.Name(), pos, func() string {
					return formatMethodArgumentHover(method, input.Name().String(), true)
				}); match != nil {
					return match
				}

				if match := hoverForToken(input.TypeName(), pos, func() string {
					return formatTypeExprHover(result.Schema, input.TypeName().String())
				}); match != nil {
					return match
				}
			}

			for _, output := range methodNode.Outputs() {
				if match := hoverForToken(output.Name(), pos, func() string {
					return formatMethodArgumentHover(method, output.Name().String(), false)
				}); match != nil {
					return match
				}

				if match := hoverForToken(output.TypeName(), pos, func() string {
					return formatTypeExprHover(result.Schema, output.TypeName().String())
				}); match != nil {
					return match
				}
			}

			for _, errorToken := range methodNode.Errors() {
				if match := hoverForToken(errorToken, pos, func() string {
					return formatErrorHover(errorByName(result.Schema, errorToken.String()))
				}); match != nil {
					return match
				}
			}
		}
	}

	return nil
}

func hoverForToken(token *ridl.TokenNode, pos protocol.Position, build func() string) *hoverMatch {
	if !tokenContainsPosition(token, pos) {
		return nil
	}

	contents := build()
	if contents == "" {
		return nil
	}

	rng := tokenRange(token)
	return &hoverMatch{contents: contents, rng: rng}
}

func tokenContainsPosition(token *ridl.TokenNode, pos protocol.Position) bool {
	if token == nil {
		return false
	}
	if token.Line() <= 0 || token.Col() <= 0 {
		return false
	}

	line := uint32(token.Line() - 1)
	if pos.Line != line {
		return false
	}

	width := uint32(utf8.RuneCountInString(token.String()))
	if width == 0 {
		return false
	}

	start := uint32(token.Col()) - width
	end := start + width
	return pos.Character >= start && pos.Character < end
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
