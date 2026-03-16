package lsp

import (
	"context"
	"sort"
	"strings"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

func (s *Server) FoldingRanges(ctx context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return []protocol.FoldingRange{}, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return []protocol.FoldingRange{}, nil
	}

	return semanticDoc.foldingRanges(), nil
}

func (d *semanticDocument) foldingRanges() []protocol.FoldingRange {
	ranges := make([]protocol.FoldingRange, 0, 8)

	for _, enumNode := range d.result.Root.Enums() {
		if rng, ok := d.enumFoldingRange(enumNode); ok {
			ranges = append(ranges, rng)
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		if rng, ok := d.structFoldingRange(structNode); ok {
			ranges = append(ranges, rng)
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		if rng, ok := d.serviceFoldingRange(serviceNode); ok {
			ranges = append(ranges, rng)
		}
	}

	ranges = append(ranges, d.importFoldingRanges()...)
	ranges = append(ranges, d.commentFoldingRanges()...)

	sort.SliceStable(ranges, func(i, j int) bool {
		if ranges[i].StartLine != ranges[j].StartLine {
			return ranges[i].StartLine < ranges[j].StartLine
		}
		if ranges[i].EndLine != ranges[j].EndLine {
			return ranges[i].EndLine < ranges[j].EndLine
		}
		return ranges[i].Kind < ranges[j].Kind
	})

	return dedupeFoldingRanges(ranges)
}

func (d *semanticDocument) enumFoldingRange(enumNode *ridl.EnumNode) (protocol.FoldingRange, bool) {
	if enumNode == nil || !validSymbolToken(enumNode.Name()) {
		return protocol.FoldingRange{}, false
	}

	startLine := uint32(ridl.TokenLine(enumNode.Name()) - 1)
	endLine := startLine
	for _, value := range enumNode.Values() {
		endLine = maxLine(endLine, definitionEndLine(value))
	}
	if endLine <= startLine {
		return protocol.FoldingRange{}, false
	}

	return protocol.FoldingRange{
		StartLine: startLine,
		EndLine:   endLine,
	}, true
}

func (d *semanticDocument) structFoldingRange(structNode *ridl.StructNode) (protocol.FoldingRange, bool) {
	if structNode == nil || !validSymbolToken(structNode.Name()) {
		return protocol.FoldingRange{}, false
	}

	startLine := uint32(ridl.TokenLine(structNode.Name()) - 1)
	endLine := startLine
	for _, field := range structNode.Fields() {
		endLine = maxLine(endLine, definitionEndLine(field))
	}
	if endLine <= startLine {
		return protocol.FoldingRange{}, false
	}

	return protocol.FoldingRange{
		StartLine: startLine,
		EndLine:   endLine,
	}, true
}

func (d *semanticDocument) serviceFoldingRange(serviceNode *ridl.ServiceNode) (protocol.FoldingRange, bool) {
	if serviceNode == nil || !validSymbolToken(serviceNode.Name()) {
		return protocol.FoldingRange{}, false
	}

	startLine := uint32(ridl.TokenLine(serviceNode.Name()) - 1)
	endLine := startLine
	for _, method := range serviceNode.Methods() {
		endLine = maxLine(endLine, methodEndLine(method))
	}
	if endLine <= startLine {
		return protocol.FoldingRange{}, false
	}

	return protocol.FoldingRange{
		StartLine: startLine,
		EndLine:   endLine,
	}, true
}

func (d *semanticDocument) importFoldingRanges() []protocol.FoldingRange {
	lines := strings.Split(d.content, "\n")
	ranges := make([]protocol.FoldingRange, 0, 1)

	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "import" {
			continue
		}

		end := i
		for j := i + 1; j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == "" {
				break
			}
			if !strings.HasPrefix(trimmed, "-") {
				break
			}
			end = j
		}

		if end > i {
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: uint32(i),
				EndLine:   uint32(end),
				Kind:      protocol.ImportsFoldingRange,
			})
		}
	}

	return ranges
}

func (d *semanticDocument) commentFoldingRanges() []protocol.FoldingRange {
	lines := strings.Split(d.content, "\n")
	ranges := make([]protocol.FoldingRange, 0, 2)

	start := -1
	for i, line := range lines {
		if isCommentLine(line) {
			if start < 0 {
				start = i
			}
			continue
		}

		if start >= 0 && i-start > 1 {
			ranges = append(ranges, protocol.FoldingRange{
				StartLine: uint32(start),
				EndLine:   uint32(i - 1),
				Kind:      protocol.CommentFoldingRange,
			})
		}
		start = -1
	}

	if start >= 0 && len(lines)-start > 1 {
		ranges = append(ranges, protocol.FoldingRange{
			StartLine: uint32(start),
			EndLine:   uint32(len(lines) - 1),
			Kind:      protocol.CommentFoldingRange,
		})
	}

	return ranges
}

func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

func definitionEndLine(definition *ridl.DefinitionNode) uint32 {
	if definition == nil {
		return 0
	}

	endLine := tokenLine(definition.Left())
	endLine = maxLine(endLine, tokenLine(definition.Right()))
	for _, meta := range definition.Meta() {
		endLine = maxLine(endLine, definitionEndLine(meta))
	}
	return endLine
}

func methodEndLine(method *ridl.MethodNode) uint32 {
	if method == nil {
		return 0
	}

	endLine := tokenLine(method.Name())
	for _, input := range method.Inputs() {
		endLine = maxLine(endLine, tokenLine(input.Name()))
		endLine = maxLine(endLine, tokenLine(input.TypeName()))
	}
	for _, output := range method.Outputs() {
		endLine = maxLine(endLine, tokenLine(output.Name()))
		endLine = maxLine(endLine, tokenLine(output.TypeName()))
	}
	for _, errorToken := range method.Errors() {
		endLine = maxLine(endLine, tokenLine(errorToken))
	}
	for _, annotation := range method.Annotations() {
		if annotation == nil {
			continue
		}
		endLine = maxLine(endLine, tokenLine(annotation.AnnotationType()))
		endLine = maxLine(endLine, tokenLine(annotation.Value()))
	}
	return endLine
}

func tokenLine(token *ridl.TokenNode) uint32 {
	if !validSymbolToken(token) {
		return 0
	}
	return uint32(ridl.TokenLine(token) - 1)
}

func maxLine(current, candidate uint32) uint32 {
	if candidate > current {
		return candidate
	}
	return current
}

func dedupeFoldingRanges(ranges []protocol.FoldingRange) []protocol.FoldingRange {
	if len(ranges) == 0 {
		return ranges
	}

	out := make([]protocol.FoldingRange, 0, len(ranges))
	var prev *protocol.FoldingRange
	for i := range ranges {
		rng := ranges[i]
		if prev != nil &&
			prev.StartLine == rng.StartLine &&
			prev.EndLine == rng.EndLine &&
			prev.Kind == rng.Kind {
			continue
		}
		out = append(out, rng)
		prev = &out[len(out)-1]
	}
	return out
}
