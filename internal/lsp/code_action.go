package lsp

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/webrpc/ridl-lsp/internal/documents"
	"github.com/webrpc/ridl-lsp/internal/workspace"
)

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	if params == nil {
		return nil, nil
	}

	actions := []protocol.CodeAction{}

	if codeActionKindRequested(params.Context.Only, protocol.QuickFix) {
		if doc, ok := s.docs.Get(string(params.TextDocument.URI)); ok {
			actions = append(actions, s.unresolvedImportCodeActions(doc, params.Context.Diagnostics)...)
		}
	}

	if codeActionKindRequested(params.Context.Only, protocol.Source) {
		edits, err := s.Formatting(ctx, &protocol.DocumentFormattingParams{
			TextDocument: params.TextDocument,
		})
		if err == nil && len(edits) > 0 {
			actions = append(actions, protocol.CodeAction{
				Title: "Format document",
				Kind:  protocol.Source,
				Edit: &protocol.WorkspaceEdit{
					Changes: map[protocol.DocumentURI][]protocol.TextEdit{
						params.TextDocument.URI: edits,
					},
				},
			})
		}
	}

	return actions, nil
}

func codeActionKindRequested(only []protocol.CodeActionKind, kind protocol.CodeActionKind) bool {
	if len(only) == 0 {
		return true
	}

	for _, requested := range only {
		if requested == kind {
			return true
		}
		if requested != "" && strings.HasPrefix(string(kind), string(requested)+".") {
			return true
		}
	}

	return false
}

func (s *Server) unresolvedImportCodeActions(doc *documents.Document, diagnostics []protocol.Diagnostic) []protocol.CodeAction {
	if doc == nil || len(filterRIDLDiagnostics(diagnostics)) == 0 {
		return nil
	}

	result := s.parsePathForNavigation(doc.Path)
	if result == nil || result.Root == nil {
		return nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, result)
	ridlDiagnostics := filterRIDLDiagnostics(diagnostics)
	missingImports := s.missingImports(semanticDoc)
	if len(missingImports) == 0 {
		return nil
	}

	actions := make([]protocol.CodeAction, 0, len(missingImports)+1)
	for _, missingImport := range missingImports {
		actions = append(actions, protocol.CodeAction{
			Title:       `Remove unresolved import "` + missingImport.path + `"`,
			Kind:        protocol.QuickFix,
			Diagnostics: ridlDiagnostics,
			IsPreferred: true,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					protocol.DocumentURI(doc.URI): {missingImport.edit},
				},
			},
		})
	}

	if len(missingImports) > 1 {
		edits := make([]protocol.TextEdit, 0, len(missingImports))
		for _, missingImport := range missingImports {
			edits = append(edits, missingImport.edit)
		}
		sortTextEditsDescending(edits)

		actions = append(actions, protocol.CodeAction{
			Title:       "Remove all unresolved imports",
			Kind:        protocol.QuickFix,
			Diagnostics: ridlDiagnostics,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					protocol.DocumentURI(doc.URI): edits,
				},
			},
		})
	}

	return actions
}

type missingImport struct {
	path string
	edit protocol.TextEdit
}

func (s *Server) missingImports(doc *semanticDocument) []missingImport {
	if doc == nil || !doc.valid() {
		return nil
	}

	missing := make([]missingImport, 0, len(doc.result.Root.Imports()))
	seen := map[string]struct{}{}
	for _, importNode := range doc.result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil || !s.isMissingImportPath(doc.path, importNode.Path().String()) {
			continue
		}

		edit, ok := unresolvedImportEdit(doc, importNode.Path().Line()-1)
		if !ok {
			continue
		}

		key := editKey(edit)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		missing = append(missing, missingImport{
			path: importNode.Path().String(),
			edit: edit,
		})
	}

	return missing
}

func (s *Server) isMissingImportPath(docPath, importPath string) bool {
	resolvedPath := workspace.ResolveImportPath(docPath, importPath)
	if _, ok := s.docs.FindByPath(resolvedPath); ok {
		return false
	}

	_, err := os.Stat(resolvedPath)
	return err != nil && (errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err))
}

func filterRIDLDiagnostics(diagnostics []protocol.Diagnostic) []protocol.Diagnostic {
	filtered := make([]protocol.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == "ridl" {
			filtered = append(filtered, diagnostic)
		}
	}
	return filtered
}

func unresolvedImportEdit(doc *semanticDocument, lineIndex int) (protocol.TextEdit, bool) {
	if doc == nil || lineIndex < 0 {
		return protocol.TextEdit{}, false
	}

	lines := splitContentLines(doc.content)
	if lineIndex >= len(lines) {
		return protocol.TextEdit{}, false
	}

	trimmed := trimmedLine(lines[lineIndex])
	if strings.HasPrefix(trimmed, "import ") {
		endLine := lineIndex + 1
		if endLine < len(lines) && trimmedLine(lines[endLine]) == "" {
			endLine++
		}
		return protocol.TextEdit{
			Range:   lineDeletionRange(doc, lines, lineIndex, endLine),
			NewText: "",
		}, true
	}

	if !strings.HasPrefix(trimmed, "- ") {
		return protocol.TextEdit{}, false
	}

	headerLine, itemCount, ok := importBlockInfo(lines, lineIndex)
	if !ok {
		return protocol.TextEdit{
			Range:   lineDeletionRange(doc, lines, lineIndex, lineIndex+1),
			NewText: "",
		}, true
	}

	startLine := lineIndex
	endLine := lineIndex + 1
	if itemCount == 1 {
		startLine = headerLine
		if endLine < len(lines) && trimmedLine(lines[endLine]) == "" {
			endLine++
		}
	}

	return protocol.TextEdit{
		Range:   lineDeletionRange(doc, lines, startLine, endLine),
		NewText: "",
	}, true
}

func splitContentLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	return strings.SplitAfter(content, "\n")
}

func trimmedLine(line string) string {
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return strings.TrimSpace(line)
}

func importBlockInfo(lines []string, itemLine int) (int, int, bool) {
	headerLine := -1
scanUp:
	for line := itemLine - 1; line >= 0; line-- {
		trimmed := trimmedLine(lines[line])
		switch {
		case trimmed == "import":
			headerLine = line
			break scanUp
		case strings.HasPrefix(trimmed, "- "), trimmed == "", strings.HasPrefix(trimmed, "#"):
			continue
		default:
			return -1, 0, false
		}
	}

	if headerLine < 0 {
		return -1, 0, false
	}

	itemCount := 0
	for line := headerLine + 1; line < len(lines); line++ {
		trimmed := trimmedLine(lines[line])
		switch {
		case strings.HasPrefix(trimmed, "- "):
			itemCount++
		case trimmed == "", strings.HasPrefix(trimmed, "#"):
			continue
		default:
			return headerLine, itemCount, true
		}
	}

	return headerLine, itemCount, true
}

func lineDeletionRange(doc *semanticDocument, lines []string, startLine, endLine int) protocol.Range {
	startOffset := lineStartOffset(lines, startLine)
	endOffset := lineStartOffset(lines, endLine)
	start, _ := doc.positionAtOffset(startOffset)
	end, _ := doc.positionAtOffset(endOffset)
	return protocol.Range{Start: start, End: end}
}

func lineStartOffset(lines []string, targetLine int) int {
	if targetLine <= 0 {
		return 0
	}

	offset := 0
	for line := 0; line < targetLine && line < len(lines); line++ {
		offset += len(lines[line])
	}
	return offset
}

func editKey(edit protocol.TextEdit) string {
	return strings.Join([]string{
		positionKey(edit.Range.Start),
		positionKey(edit.Range.End),
		edit.NewText,
	}, ":")
}

func sortTextEditsDescending(edits []protocol.TextEdit) {
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].Range.Start.Line != edits[j].Range.Start.Line {
			return edits[i].Range.Start.Line > edits[j].Range.Start.Line
		}
		return edits[i].Range.Start.Character > edits[j].Range.Start.Character
	})
}

func positionKey(pos protocol.Position) string {
	return strings.Join([]string{
		intString(pos.Line),
		intString(pos.Character),
	}, ":")
}

func intString[T ~uint32 | ~int](value T) string {
	return strconv.Itoa(int(value))
}
