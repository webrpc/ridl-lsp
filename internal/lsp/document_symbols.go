package lsp

import (
	"context"
	"sort"

	"go.lsp.dev/protocol"

	"github.com/webrpc/webrpc/schema"
	ridl "github.com/webrpc/webrpc/schema/ridl"
)

func (s *Server) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) ([]any, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return []any{}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return []any{}, nil
	}

	symbols := semanticDoc.documentSymbols()
	result := make([]any, 0, len(symbols))
	for _, symbol := range symbols {
		result = append(result, symbol)
	}

	return result, nil
}

func (d *semanticDocument) documentSymbols() []protocol.DocumentSymbol {
	if !d.valid() {
		return nil
	}

	type positionedSymbol struct {
		start  int
		symbol protocol.DocumentSymbol
	}

	positioned := make([]positionedSymbol, 0, len(d.result.Root.Enums())+len(d.result.Root.Structs())+len(d.result.Root.Errors())+len(d.result.Root.Services()))

	for _, enumNode := range d.result.Root.Enums() {
		positioned = append(positioned, positionedSymbol{
			start:  enumNode.Name().Pos(),
			symbol: d.enumDocumentSymbol(enumNode),
		})
	}

	for _, structNode := range d.result.Root.Structs() {
		positioned = append(positioned, positionedSymbol{
			start:  structNode.Name().Pos(),
			symbol: d.structDocumentSymbol(structNode),
		})
	}

	for _, errorNode := range d.result.Root.Errors() {
		positioned = append(positioned, positionedSymbol{
			start:  errorNode.Name().Pos(),
			symbol: d.errorDocumentSymbol(errorNode),
		})
	}

	for _, serviceNode := range d.result.Root.Services() {
		positioned = append(positioned, positionedSymbol{
			start:  serviceNode.Name().Pos(),
			symbol: d.serviceDocumentSymbol(serviceNode),
		})
	}

	sort.SliceStable(positioned, func(i, j int) bool {
		return positioned[i].start < positioned[j].start
	})

	symbols := make([]protocol.DocumentSymbol, 0, len(positioned))
	for _, entry := range positioned {
		symbols = append(symbols, entry.symbol)
	}

	return symbols
}

func (d *semanticDocument) enumDocumentSymbol(enumNode *ridl.EnumNode) protocol.DocumentSymbol {
	children := make([]protocol.DocumentSymbol, 0, len(enumNode.Values()))
	for _, value := range enumNode.Values() {
		detail := ""
		if value.Right() != nil && value.Right().String() != "" {
			detail = "= " + value.Right().String()
		}
		children = append(children, d.definitionDocumentSymbol(value, protocol.SymbolKindEnumMember, detail))
	}

	detail := ""
	if enumNode.TypeName() != nil && enumNode.TypeName().String() != "" {
		detail = enumNode.TypeName().String()
	}

	return d.nodeDocumentSymbol(
		enumNode.Name().String(),
		protocol.SymbolKindEnum,
		detail,
		enumNode.Name(),
		enumNode.Start(),
		enumNode.End(),
		children,
	)
}

func (d *semanticDocument) structDocumentSymbol(structNode *ridl.StructNode) protocol.DocumentSymbol {
	children := make([]protocol.DocumentSymbol, 0, len(structNode.Fields()))
	for _, field := range structNode.Fields() {
		detail := ""
		if field.Right() != nil && field.Right().String() != "" {
			detail = field.Right().String()
		}
		children = append(children, d.definitionDocumentSymbol(field, protocol.SymbolKindField, detail))
	}

	return d.nodeDocumentSymbol(
		structNode.Name().String(),
		protocol.SymbolKindStruct,
		"",
		structNode.Name(),
		structNode.Start(),
		structNode.End(),
		children,
	)
}

func (d *semanticDocument) errorDocumentSymbol(errorNode *ridl.ErrorNode) protocol.DocumentSymbol {
	var detail string
	if schemaError := d.errorByName(errorNode.Name().String()); schemaError != nil {
		detail = formatErrorSymbolDetail(schemaError)
	}

	return d.nodeDocumentSymbol(
		errorNode.Name().String(),
		protocol.SymbolKindObject,
		detail,
		errorNode.Name(),
		errorNode.Start(),
		errorNode.End(),
		nil,
	)
}

func (d *semanticDocument) serviceDocumentSymbol(serviceNode *ridl.ServiceNode) protocol.DocumentSymbol {
	service := d.result.Schema.GetServiceByName(serviceNode.Name().String())

	children := make([]protocol.DocumentSymbol, 0, len(serviceNode.Methods()))
	for _, methodNode := range serviceNode.Methods() {
		method := d.methodByName(service, methodNode.Name().String())
		children = append(children, d.methodDocumentSymbol(methodNode, method))
	}

	return d.nodeDocumentSymbol(
		serviceNode.Name().String(),
		protocol.SymbolKindInterface,
		"",
		serviceNode.Name(),
		serviceNode.Start(),
		serviceNode.End(),
		children,
	)
}

func (d *semanticDocument) methodDocumentSymbol(methodNode *ridl.MethodNode, method *schema.Method) protocol.DocumentSymbol {
	detail := ""
	if method != nil {
		detail = methodSignature(method)
	}

	start, end := methodRangeBounds(methodNode)
	return d.nodeDocumentSymbol(
		methodNode.Name().String(),
		protocol.SymbolKindMethod,
		detail,
		methodNode.Name(),
		start,
		end,
		nil,
	)
}

func (d *semanticDocument) definitionDocumentSymbol(definition *ridl.DefinitionNode, kind protocol.SymbolKind, detail string) protocol.DocumentSymbol {
	return d.nodeDocumentSymbol(
		definition.Left().String(),
		kind,
		detail,
		definition.Left(),
		definition.Start(),
		definition.End(),
		nil,
	)
}

func (d *semanticDocument) nodeDocumentSymbol(
	name string,
	kind protocol.SymbolKind,
	detail string,
	token *ridl.TokenNode,
	start int,
	end int,
	children []protocol.DocumentSymbol,
) protocol.DocumentSymbol {
	selectionRange := d.tokenRange(token, protocol.Position{})
	rng, ok := d.rangeFromOffsets(start, end)
	if !ok || !rangeContains(rng, selectionRange) {
		rng = selectionRange
	}

	symbol := protocol.DocumentSymbol{
		Name:           name,
		Detail:         detail,
		Kind:           kind,
		Range:          rng,
		SelectionRange: selectionRange,
	}
	if len(children) > 0 {
		symbol.Children = children
	}
	return symbol
}

func (d *semanticDocument) rangeFromOffsets(start, end int) (protocol.Range, bool) {
	startPos, ok := d.positionAtOffset(start)
	if !ok {
		return protocol.Range{}, false
	}

	endPos, ok := d.positionAtOffset(end)
	if !ok {
		return protocol.Range{}, false
	}

	if endPos.Line < startPos.Line || (endPos.Line == startPos.Line && endPos.Character < startPos.Character) {
		return protocol.Range{}, false
	}

	return protocol.Range{
		Start: startPos,
		End:   endPos,
	}, true
}

func (d *semanticDocument) positionAtOffset(offset int) (protocol.Position, bool) {
	if offset < 0 || offset > len(d.content) {
		return protocol.Position{}, false
	}

	line := 0
	character := 0
	for idx, r := range d.content {
		if idx >= offset {
			return protocol.Position{
				Line:      uint32(line),
				Character: uint32(character),
			}, true
		}
		if r == '\n' {
			line++
			character = 0
			continue
		}
		character++
	}

	if offset == len(d.content) {
		return protocol.Position{
			Line:      uint32(line),
			Character: uint32(character),
		}, true
	}

	return protocol.Position{}, false
}

func methodRangeBounds(methodNode *ridl.MethodNode) (int, int) {
	start := methodNode.Name().Start()
	end := methodNode.Name().End()

	extend := func(token *ridl.TokenNode) {
		if !validSymbolToken(token) {
			return
		}
		if token.Start() < start {
			start = token.Start()
		}
		if token.End() > end {
			end = token.End()
		}
	}

	for _, input := range methodNode.Inputs() {
		extend(input.Name())
		extend(input.TypeName())
	}
	for _, output := range methodNode.Outputs() {
		extend(output.Name())
		extend(output.TypeName())
	}
	for _, errorToken := range methodNode.Errors() {
		extend(errorToken)
	}

	return start, end
}

func validSymbolToken(token *ridl.TokenNode) bool {
	return token != nil && token.Line() > 0 && token.String() != ""
}

func rangeContains(outer, inner protocol.Range) bool {
	if inner.Start.Line < outer.Start.Line || inner.End.Line > outer.End.Line {
		return false
	}
	if inner.Start.Line == outer.Start.Line && inner.Start.Character < outer.Start.Character {
		return false
	}
	if inner.End.Line == outer.End.Line && inner.End.Character > outer.End.Character {
		return false
	}
	return true
}

func formatErrorSymbolDetail(schemaError *schema.Error) string {
	if schemaError == nil {
		return ""
	}
	return "error " + schemaError.Name
}
