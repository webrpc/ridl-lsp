package lsp

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.lsp.dev/protocol"
	"go.uber.org/zap"

	"github.com/webrpc/ridl-lsp/internal/documents"
	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
	"github.com/webrpc/ridl-lsp/internal/workspace"
)

var (
	errorLineColRegex    = regexp.MustCompile(`^(\d+):(\d+):`)
	nearFileLineColRegex = regexp.MustCompile(`from\s+[^:]+:(\d+):(\d+)`)
)

func (s *Server) parseAndPublishDiagnostics(ctx context.Context, doc *documents.Document) {
	diagnostics := s.parseDocument(doc)
	if s.client == nil {
		return
	}

	if err := s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         protocol.DocumentURI(doc.URI),
		Diagnostics: diagnostics,
	}); err != nil {
		s.logger.Error("failed to publish diagnostics", zap.String("uri", doc.URI), zap.Error(err))
	}
}

func (s *Server) parseDocument(doc *documents.Document) []protocol.Diagnostic {
	overlays := s.overlayContents()

	result, err := s.parser.Parse(s.workspace.Root(), doc.Path, overlays)
	if err != nil {
		doc.Result = nil
		return []protocol.Diagnostic{
			{
				Range:    lineRange(1),
				Severity: severityError(),
				Message:  err.Error(),
				Source:   "ridl",
			},
		}
	}

	if len(result.Errors) == 0 {
		doc.Result = result
		return s.importDiagnostics(doc)
	}

	doc.Result = nil

	diagnostics := make([]protocol.Diagnostic, 0, len(result.Errors))
	for _, e := range result.Errors {
		diag := errorToDiagnostic(e)
		diagnostics = append(diagnostics, diag)
	}

	return diagnostics
}

func (s *Server) overlayContents() map[string]string {
	overlays := make(map[string]string, len(s.docs.All()))
	for _, doc := range s.docs.All() {
		overlays[doc.Path] = doc.Content
	}
	return overlays
}

func (s *Server) refreshOpenDocuments(ctx context.Context) {
	for _, doc := range s.docs.All() {
		s.parseAndPublishDiagnostics(ctx, doc)
	}
}

func errorToDiagnostic(err error) protocol.Diagnostic {
	msg := err.Error()

	if matches := errorLineColRegex.FindStringSubmatch(msg); matches != nil {
		line, _ := strconv.Atoi(matches[1])
		col, _ := strconv.Atoi(matches[2])

		cleanMsg := strings.TrimPrefix(msg, matches[0])
		cleanMsg = strings.TrimSpace(cleanMsg)

		lspLine := line - 1
		if lspLine < 0 {
			lspLine = 0
		}
		lspCol := col
		if lspCol > 0 {
			lspCol--
		}

		return protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: uint32(lspLine), Character: uint32(lspCol)},
				End:   protocol.Position{Line: uint32(lspLine), Character: uint32(lspCol + 1)},
			},
			Severity: severityError(),
			Message:  cleanMsg,
			Source:   "ridl",
		}
	}

	if matches := nearFileLineColRegex.FindStringSubmatch(msg); matches != nil {
		line, _ := strconv.Atoi(matches[1])
		return protocol.Diagnostic{
			Range:    lineRange(line),
			Severity: severityError(),
			Message:  msg,
			Source:   "ridl",
		}
	}

	return protocol.Diagnostic{
		Range:    lineRange(1),
		Severity: severityError(),
		Message:  msg,
		Source:   "ridl",
	}
}

func lineRange(line1Based int) protocol.Range {
	line := line1Based - 1
	if line < 0 {
		line = 0
	}
	return protocol.Range{
		Start: protocol.Position{Line: uint32(line), Character: 0},
		End:   protocol.Position{Line: uint32(line), Character: 1000},
	}
}

func severityError() protocol.DiagnosticSeverity {
	return protocol.DiagnosticSeverityError
}

func PathToURI(p string) protocol.DocumentURI {
	return workspace.PathToURI(p)
}

func severityWarning() protocol.DiagnosticSeverity {
	return protocol.DiagnosticSeverityWarning
}

func (s *Server) importDiagnostics(doc *documents.Document) []protocol.Diagnostic {
	if doc == nil || doc.Result == nil || doc.Result.Root == nil {
		return nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, doc.Result)
	referenced := semanticDoc.referencedNames()

	var diagnostics []protocol.Diagnostic
	for _, importNode := range doc.Result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil {
			continue
		}
		// Only check full imports (no member list).
		if len(importNode.Members()) > 0 {
			continue
		}

		importPath := importNode.Path().String()
		resolvedPath := workspace.ResolveImportPath(doc.Path, importPath)

		importResult, err := s.parser.Parse(s.workspace.Root(), resolvedPath, s.overlayContents())
		if err != nil || importResult == nil || importResult.Root == nil {
			continue
		}

		exported := locallyDefinedNames(importResult.Root)
		if len(exported) == 0 {
			continue
		}

		var used []string
		for name := range exported {
			if _, ok := referenced[name]; ok {
				used = append(used, name)
			}
		}
		sort.Strings(used)

		line := ridl.TokenLine(importNode.Path())

		if len(used) == 0 {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range:    lineRange(line),
				Severity: severityWarning(),
				Message:  `Import "` + importPath + `" is unused`,
				Source:   "ridl",
			})
			continue
		}

		if len(used) == len(exported) {
			continue
		}

		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range:    lineRange(line),
			Severity: severityWarning(),
			Message:  `Import "` + importPath + `" can be narrowed to: ` + strings.Join(used, ", "),
			Source:   "ridl",
		})
	}

	// Check selective imports for transitive re-imports.
	for _, importNode := range doc.Result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil || len(importNode.Members()) == 0 {
			continue
		}

		importPath := workspace.ResolveImportPath(doc.Path, importNode.Path().String())
		importResult := s.parsePathForNavigation(importPath)
		if importResult == nil || importResult.Root == nil {
			continue
		}

		localNames := locallyDefinedNames(importResult.Root)

		for _, member := range importNode.Members() {
			if member == nil || member.String() == "" {
				continue
			}
			name := member.String()

			if _, ok := localNames[name]; ok {
				continue
			}

			originalPath, ok := s.uniqueImportCandidatePath(doc.Path, referenceKindType, name)
			if !ok {
				originalPath, ok = s.uniqueImportCandidatePath(doc.Path, referenceKindError, name)
			}
			if !ok {
				continue
			}

			relOriginal, ok := relativeImportPath(doc.Path, originalPath)
			if !ok {
				continue
			}

			memberLine := ridl.TokenLine(member) - 1
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range:    lineRange(memberLine + 1),
				Severity: severityWarning(),
				Message:  name + ` is defined in "` + relOriginal + `", not "` + importNode.Path().String() + `"`,
				Source:   "ridl",
			})
		}
	}

	return diagnostics
}
