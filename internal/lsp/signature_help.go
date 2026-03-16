package lsp

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/webrpc/webrpc/schema"
	ridl "github.com/webrpc/webrpc/schema/ridl"
)

func (s *Server) SignatureHelp(ctx context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	if params == nil {
		return nil, nil
	}

	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	return semanticDoc.signatureHelpAt(params.Position), nil
}

type methodTupleKind int

const (
	methodTupleInputs methodTupleKind = iota
	methodTupleOutputs
)

func (d *semanticDocument) signatureHelpAt(pos protocol.Position) *protocol.SignatureHelp {
	if !d.valid() {
		return nil
	}

	for _, serviceNode := range d.result.Root.Services() {
		service := d.result.Schema.GetServiceByName(serviceNode.Name().String())
		for _, methodNode := range serviceNode.Methods() {
			if help := d.methodSignatureHelp(service, methodNode, pos); help != nil {
				return help
			}
		}
	}

	return nil
}

func (d *semanticDocument) methodSignatureHelp(service *schema.Service, methodNode *ridl.MethodNode, pos protocol.Position) *protocol.SignatureHelp {
	if methodNode == nil || service == nil {
		return nil
	}

	method := d.methodByName(service, methodNode.Name().String())
	if method == nil {
		return nil
	}

	context, activeParameter, ok := d.methodTupleContext(methodNode, pos)
	if !ok {
		return nil
	}

	args := method.Inputs
	if context == methodTupleOutputs {
		args = method.Outputs
	}

	signature := protocol.SignatureInformation{
		Label:         methodSignature(method),
		Documentation: signatureHelpDocumentation(method),
		Parameters:    methodParameterInformation(args),
		ActiveParameter: func() uint32 {
			if len(args) == 0 {
				return 0
			}
			if activeParameter >= len(args) {
				return uint32(len(args) - 1)
			}
			return uint32(activeParameter)
		}(),
	}

	return &protocol.SignatureHelp{
		Signatures:      []protocol.SignatureInformation{signature},
		ActiveSignature: 0,
		ActiveParameter: signature.ActiveParameter,
	}
}

func signatureHelpDocumentation(method *schema.Method) interface{} {
	if method == nil || len(method.Comments) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(method.Comments))
	for _, note := range method.Comments {
		if strings.TrimSpace(note) == "" {
			continue
		}
		filtered = append(filtered, note)
	}
	if len(filtered) == 0 {
		return nil
	}

	return protocol.MarkupContent{
		Kind:  protocol.Markdown,
		Value: strings.Join(filtered, "\n\n"),
	}
}

func methodParameterInformation(args []*schema.MethodArgument) []protocol.ParameterInformation {
	if len(args) == 0 {
		return nil
	}

	parameters := make([]protocol.ParameterInformation, 0, len(args))
	for _, arg := range args {
		if arg == nil {
			continue
		}

		parameters = append(parameters, protocol.ParameterInformation{
			Label: parameterLabel(arg),
		})
	}

	return parameters
}

func parameterLabel(arg *schema.MethodArgument) string {
	if arg == nil || arg.Type == nil {
		return ""
	}
	return arg.Name + optionalSuffix(arg.Optional) + ": " + arg.Type.String()
}

func (d *semanticDocument) methodTupleContext(methodNode *ridl.MethodNode, pos protocol.Position) (methodTupleKind, int, bool) {
	methodRange := d.methodSelectionRange(methodNode)
	if pos.Line != methodRange.Start.Line || pos.Character < methodRange.Start.Character || pos.Character > methodRange.End.Character {
		return methodTupleInputs, 0, false
	}

	line := d.contentLine(int(methodRange.Start.Line))
	lineRunes := []rune(line)
	methodStart := int(methodRange.Start.Character)
	if methodStart < 0 || methodStart >= len(lineRunes) {
		return methodTupleInputs, 0, false
	}

	source := lineRunes[methodStart:]
	cursor := int(pos.Character) - methodStart
	if cursor < 0 {
		return methodTupleInputs, 0, false
	}
	if cursor > len(source) {
		cursor = len(source)
	}

	inputOpen, inputClose, ok := tupleBounds(source, 0)
	if !ok {
		return methodTupleInputs, 0, false
	}

	if cursor > inputOpen && cursor <= inputClose {
		return methodTupleInputs, activeTupleParameter(source, inputOpen, inputClose, cursor), true
	}

	outputArrow := arrowIndex(source, inputClose+1)
	if outputArrow < 0 {
		return methodTupleInputs, 0, false
	}

	outputOpen, outputClose, ok := tupleBounds(source, outputArrow+2)
	if !ok {
		return methodTupleInputs, 0, false
	}

	if cursor > outputOpen && cursor <= outputClose {
		return methodTupleOutputs, activeTupleParameter(source, outputOpen, outputClose, cursor), true
	}

	return methodTupleInputs, 0, false
}

func tupleBounds(source []rune, searchFrom int) (int, int, bool) {
	open := -1
	for idx := searchFrom; idx < len(source); idx++ {
		if source[idx] == '(' {
			open = idx
			break
		}
	}
	if open < 0 {
		return 0, 0, false
	}

	depth := 0
	for idx := open; idx < len(source); idx++ {
		switch source[idx] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return open, idx, true
			}
		}
	}

	return 0, 0, false
}

func arrowIndex(source []rune, searchFrom int) int {
	for idx := searchFrom; idx+1 < len(source); idx++ {
		if source[idx] == '=' && source[idx+1] == '>' {
			return idx
		}
	}
	return -1
}

func activeTupleParameter(source []rune, open, close, cursor int) int {
	if close <= open+1 {
		return 0
	}

	end := cursor
	if end > close {
		end = close
	}
	if end <= open+1 {
		return 0
	}

	active := 0
	angleDepth := 0
	squareDepth := 0
	parenDepth := 0

	for idx := open + 1; idx < end; idx++ {
		switch source[idx] {
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
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case ',':
			if angleDepth == 0 && squareDepth == 0 && parenDepth == 0 {
				active++
			}
		}
	}

	return active
}
