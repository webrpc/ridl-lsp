package lsp

import (
	"context"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/webrpc/schema/ridl"
)

func (s *Server) SelectionRange(ctx context.Context, params *protocol.SelectionRangeParams) ([]protocol.SelectionRange, error) {
	if params == nil {
		return []protocol.SelectionRange{}, nil
	}

	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return []protocol.SelectionRange{}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return []protocol.SelectionRange{}, nil
	}

	ranges := make([]protocol.SelectionRange, 0, len(params.Positions))
	for _, pos := range params.Positions {
		if selection := semanticDoc.selectionRangeAt(pos); selection != nil {
			ranges = append(ranges, *selection)
			continue
		}

		ranges = append(ranges, protocol.SelectionRange{
			Range: protocol.Range{Start: pos, End: pos},
		})
	}

	return ranges, nil
}

func (d *semanticDocument) selectionRangeAt(pos protocol.Position) *protocol.SelectionRange {
	if !d.valid() {
		return nil
	}

	for _, importNode := range d.result.Root.Imports() {
		if selection := d.importSelectionRange(importNode, pos); selection != nil {
			return selection
		}
	}

	for _, enumNode := range d.result.Root.Enums() {
		enumRange := d.enumSelectionRange(enumNode)

		if d.tokenContainsPosition(enumNode.Name(), pos) {
			return buildSelectionRangeChain(d.tokenRange(enumNode.Name(), pos), enumRange)
		}
		if selection := d.typeTokenSelectionRange(enumNode.TypeName(), pos, enumRange); selection != nil {
			return selection
		}
		for _, value := range enumNode.Values() {
			valueRange := d.definitionSelectionRange(value)
			if d.tokenContainsPosition(value.Left(), pos) {
				return buildSelectionRangeChain(d.tokenRange(value.Left(), pos), valueRange, enumRange)
			}
			if selection := d.typeTokenSelectionRange(value.Right(), pos, valueRange, enumRange); selection != nil {
				return selection
			}
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		structRange := d.structSelectionRange(structNode)

		if d.tokenContainsPosition(structNode.Name(), pos) {
			return buildSelectionRangeChain(d.tokenRange(structNode.Name(), pos), structRange)
		}
		for _, field := range structNode.Fields() {
			fieldRange := d.definitionSelectionRange(field)
			if d.tokenContainsPosition(field.Left(), pos) {
				return buildSelectionRangeChain(d.tokenRange(field.Left(), pos), fieldRange, structRange)
			}
			if selection := d.typeTokenSelectionRange(field.Right(), pos, fieldRange, structRange); selection != nil {
				return selection
			}
		}
	}

	for _, errorNode := range d.result.Root.Errors() {
		errorRange := d.errorSelectionRange(errorNode)
		if d.tokenContainsPosition(errorNode.Name(), pos) {
			return buildSelectionRangeChain(d.tokenRange(errorNode.Name(), pos), errorRange)
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		serviceRange := d.serviceSelectionRange(serviceNode)

		if d.tokenContainsPosition(serviceNode.Name(), pos) {
			return buildSelectionRangeChain(d.tokenRange(serviceNode.Name(), pos), serviceRange)
		}

		for _, methodNode := range serviceNode.Methods() {
			methodRange := d.methodSelectionRange(methodNode)
			if d.tokenContainsPosition(methodNode.Name(), pos) {
				return buildSelectionRangeChain(d.tokenRange(methodNode.Name(), pos), methodRange, serviceRange)
			}

			for _, input := range methodNode.Inputs() {
				argRange := d.argumentSelectionRange(input)
				if d.tokenContainsPosition(input.Name(), pos) {
					return buildSelectionRangeChain(d.tokenRange(input.Name(), pos), argRange, methodRange, serviceRange)
				}
				if selection := d.typeTokenSelectionRange(input.TypeName(), pos, argRange, methodRange, serviceRange); selection != nil {
					return selection
				}
			}

			for _, output := range methodNode.Outputs() {
				argRange := d.argumentSelectionRange(output)
				if d.tokenContainsPosition(output.Name(), pos) {
					return buildSelectionRangeChain(d.tokenRange(output.Name(), pos), argRange, methodRange, serviceRange)
				}
				if selection := d.typeTokenSelectionRange(output.TypeName(), pos, argRange, methodRange, serviceRange); selection != nil {
					return selection
				}
			}

			for _, errorToken := range methodNode.Errors() {
				if d.tokenContainsPosition(errorToken, pos) {
					return buildSelectionRangeChain(d.tokenRange(errorToken, pos), methodRange, serviceRange)
				}
			}
		}
	}

	return nil
}

func (d *semanticDocument) importSelectionRange(importNode *ridl.ImportNode, pos protocol.Position) *protocol.SelectionRange {
	if importNode == nil {
		return nil
	}

	importRange := d.importEntrySelectionRange(importNode)
	if d.tokenContainsPosition(importNode.Path(), pos) {
		return buildSelectionRangeChain(d.tokenRange(importNode.Path(), pos), importRange)
	}

	for _, member := range importNode.Members() {
		if d.tokenContainsPosition(member, pos) {
			return buildSelectionRangeChain(d.tokenRange(member, pos), importRange)
		}
	}

	return nil
}

func (d *semanticDocument) typeTokenSelectionRange(token *ridl.TokenNode, pos protocol.Position, parents ...protocol.Range) *protocol.SelectionRange {
	if !d.tokenContainsPosition(token, pos) {
		return nil
	}

	ranges := make([]protocol.Range, 0, len(parents)+2)
	name := d.identifierAtTokenPosition(token, pos)
	if name != "" {
		ranges = append(ranges, d.identifierRangeInToken(token, pos, name))
	}
	ranges = append(ranges, d.tokenRange(token, pos))
	ranges = append(ranges, parents...)
	return buildSelectionRangeChain(ranges...)
}

func (d *semanticDocument) nodeSelectionRange(token *ridl.TokenNode, start, end int) protocol.Range {
	selection := d.tokenRange(token, protocol.Position{})
	rng, ok := d.rangeFromOffsets(start, end)
	if !ok || !rangeContains(rng, selection) {
		return selection
	}
	return rng
}

func (d *semanticDocument) methodSelectionRange(methodNode *ridl.MethodNode) protocol.Range {
	startRange := d.tokenRange(methodNode.Name(), protocol.Position{})
	lineIndex := int(startRange.Start.Line)
	return d.lineRange(lineIndex, startRange.Start.Character, lineEndCharacter(d.contentLine(lineIndex)))
}

func (d *semanticDocument) argumentSelectionRange(arg *ridl.ArgumentNode) protocol.Range {
	token := arg.Name()
	if !validSymbolToken(token) {
		token = arg.TypeName()
	}

	tokenRange := d.tokenRange(token, protocol.Position{})
	endChar := tokenRange.End.Character
	typeRange := d.tokenRange(arg.TypeName(), protocol.Position{})
	if typeRange.End.Line == tokenRange.Start.Line && typeRange.End.Character > endChar {
		endChar = typeRange.End.Character
	}

	return d.lineRange(int(tokenRange.Start.Line), tokenRange.Start.Character, endChar)
}

func (d *semanticDocument) definitionSelectionRange(def *ridl.DefinitionNode) protocol.Range {
	tokenRange := d.tokenRange(def.Left(), protocol.Position{})
	lineIndex := int(tokenRange.Start.Line)
	startChar := lineIndentCharacter(d.contentLine(lineIndex))
	endChar := tokenRange.End.Character

	if right := def.Right(); validSymbolToken(right) {
		rightRange := d.tokenRange(right, protocol.Position{})
		if rightRange.End.Line == tokenRange.Start.Line && rightRange.End.Character > endChar {
			endChar = rightRange.End.Character
		}
	}

	for _, meta := range def.Meta() {
		if !validSymbolToken(meta.Left()) {
			continue
		}
		metaRange := d.tokenRange(meta.Left(), protocol.Position{})
		if metaRange.End.Line == tokenRange.Start.Line && metaRange.End.Character > endChar {
			endChar = metaRange.End.Character
		}
		if validSymbolToken(meta.Right()) {
			rightRange := d.tokenRange(meta.Right(), protocol.Position{})
			if rightRange.End.Line == tokenRange.Start.Line && rightRange.End.Character > endChar {
				endChar = rightRange.End.Character
			}
		}
	}

	return d.lineRange(lineIndex, startChar, endChar)
}

func (d *semanticDocument) importEntrySelectionRange(importNode *ridl.ImportNode) protocol.Range {
	pathRange := d.tokenRange(importNode.Path(), protocol.Position{})
	lineIndex := int(pathRange.Start.Line)
	startChar := lineIndentCharacter(d.contentLine(lineIndex))
	endChar := pathRange.End.Character

	for _, member := range importNode.Members() {
		memberRange := d.tokenRange(member, protocol.Position{})
		if memberRange.End.Line == pathRange.Start.Line && memberRange.End.Character > endChar {
			endChar = memberRange.End.Character
		}
	}

	return d.lineRange(lineIndex, startChar, endChar)
}

func (d *semanticDocument) structSelectionRange(structNode *ridl.StructNode) protocol.Range {
	startLine := tokenLine(structNode.Name())
	endLine := startLine
	for _, field := range structNode.Fields() {
		endLine = maxLine(endLine, definitionEndLine(field))
		for _, meta := range field.Meta() {
			endLine = maxLine(endLine, definitionEndLine(meta))
		}
	}
	return d.blockLineRange(int(startLine), int(endLine))
}

func (d *semanticDocument) enumSelectionRange(enumNode *ridl.EnumNode) protocol.Range {
	startLine := tokenLine(enumNode.Name())
	endLine := maxLine(startLine, tokenLine(enumNode.TypeName()))
	for _, value := range enumNode.Values() {
		endLine = maxLine(endLine, definitionEndLine(value))
	}
	return d.blockLineRange(int(startLine), int(endLine))
}

func (d *semanticDocument) serviceSelectionRange(serviceNode *ridl.ServiceNode) protocol.Range {
	startLine := tokenLine(serviceNode.Name())
	endLine := startLine
	for _, method := range serviceNode.Methods() {
		endLine = maxLine(endLine, methodEndLine(method))
	}
	return d.blockLineRange(int(startLine), int(endLine))
}

func (d *semanticDocument) errorSelectionRange(errorNode *ridl.ErrorNode) protocol.Range {
	line := tokenLine(errorNode.Name())
	return d.blockLineRange(int(line), int(line))
}

func (d *semanticDocument) blockLineRange(startLine, endLine int) protocol.Range {
	startIndex := startLine
	endIndex := endLine
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex < startIndex {
		endIndex = startIndex
	}
	startChar := lineIndentCharacter(d.contentLine(startIndex))
	endChar := lineEndCharacter(d.contentLine(endIndex))
	return protocol.Range{
		Start: protocol.Position{Line: uint32(startIndex), Character: startChar},
		End:   protocol.Position{Line: uint32(endIndex), Character: endChar},
	}
}

func (d *semanticDocument) lineRange(lineIndex int, startChar, endChar uint32) protocol.Range {
	if endChar < startChar {
		endChar = startChar
	}
	return protocol.Range{
		Start: protocol.Position{Line: uint32(lineIndex), Character: startChar},
		End:   protocol.Position{Line: uint32(lineIndex), Character: endChar},
	}
}

func (d *semanticDocument) contentLine(lineIndex int) string {
	line, _ := d.lineText(lineIndex)
	return line
}

func lineIndentCharacter(line string) uint32 {
	var idx uint32
	for _, r := range line {
		if r != ' ' && r != '\t' {
			break
		}
		idx++
	}
	return idx
}

func lineEndCharacter(line string) uint32 {
	var length uint32
	for _, r := range line {
		if r == '\n' || r == '\r' {
			break
		}
		length++
	}
	return length
}

func buildSelectionRangeChain(ranges ...protocol.Range) *protocol.SelectionRange {
	clean := make([]protocol.Range, 0, len(ranges))
	for _, rng := range ranges {
		if len(clean) > 0 && rangesEqual(clean[len(clean)-1], rng) {
			continue
		}
		if len(clean) > 0 && !rangeContains(rng, clean[len(clean)-1]) {
			continue
		}
		clean = append(clean, rng)
	}

	if len(clean) == 0 {
		return nil
	}

	var parent *protocol.SelectionRange
	for idx := len(clean) - 1; idx >= 0; idx-- {
		parent = &protocol.SelectionRange{
			Range:  clean[idx],
			Parent: parent,
		}
	}
	return parent
}

func rangesEqual(a, b protocol.Range) bool {
	return a.Start == b.Start && a.End == b.End
}
