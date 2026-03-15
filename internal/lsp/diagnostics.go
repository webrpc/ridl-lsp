package lsp

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/webrpc/ridl-lsp/internal/documents"
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

	_ = s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         protocol.DocumentURI(doc.URI),
		Diagnostics: diagnostics,
	})
}

func (s *Server) parseDocument(doc *documents.Document) []protocol.Diagnostic {
	overlays := s.overlayContents(s.workspace.Root())

	result, err := s.parser.Parse(s.workspace.Root(), doc.Path, overlays)
	if err != nil {
		return []protocol.Diagnostic{
			{
				Range:    lineRange(1),
				Severity: severityError(),
				Message:  err.Error(),
				Source:   "ridl",
			},
		}
	}

	doc.Result = result

	if len(result.Errors) == 0 {
		return []protocol.Diagnostic{}
	}

	diagnostics := make([]protocol.Diagnostic, 0, len(result.Errors))
	for _, e := range result.Errors {
		diag := errorToDiagnostic(e)
		diagnostics = append(diagnostics, diag)
	}

	return diagnostics
}

func (s *Server) overlayContents(root string) map[string]string {
	overlays := map[string]string{}
	if root == "" {
		return overlays
	}
	for _, doc := range s.docs.All() {
		relPath, err := filepath.Rel(root, doc.Path)
		if err != nil {
			continue
		}
		relPath = filepath.ToSlash(filepath.Clean(relPath))
		if relPath == "." || strings.HasPrefix(relPath, "../") || relPath == ".." {
			continue
		}
		overlays[relPath] = doc.Content
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
