package lsp

import (
	"context"
	"fmt"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/webrpc/webrpc/schema"
)

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, doc.Result)
	if !semanticDoc.valid() {
		return nil, nil
	}

	match := semanticDoc.hoverAt(params.Position)
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

	args := method.Inputs
	kind := "Input"
	if !input {
		args = method.Outputs
		kind = "Output"
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
