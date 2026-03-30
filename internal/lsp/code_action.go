package lsp

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/webrpc/ridl-lsp/internal/documents"
	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
	"github.com/webrpc/ridl-lsp/internal/workspace"
	"github.com/webrpc/webrpc/schema"
)

func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	if params == nil {
		return nil, nil
	}

	actions := []protocol.CodeAction{}

	if codeActionKindRequested(params.Context.Only, protocol.QuickFix) {
		if doc, ok := s.docs.Get(string(params.TextDocument.URI)); ok {
			actions = append(actions, s.unresolvedImportCodeActions(doc, params.Context.Diagnostics)...)
			actions = append(actions, s.missingImportCodeActions(doc, params.Context.Diagnostics)...)
			actions = append(actions, s.narrowImportCodeActions(doc, params.Context.Diagnostics)...)
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

func (s *Server) missingImportCodeActions(doc *documents.Document, diagnostics []protocol.Diagnostic) []protocol.CodeAction {
	ridlDiagnostics := filterRIDLDiagnostics(diagnostics)
	if doc == nil || len(ridlDiagnostics) == 0 {
		return nil
	}

	result := s.parsePathForNavigation(doc.Path)
	if result == nil || result.Root == nil {
		return nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, result)
	candidates := s.missingImportCandidates(semanticDoc)
	if len(candidates) == 0 {
		return nil
	}

	actions := make([]protocol.CodeAction, 0, len(candidates))
	for _, candidate := range candidates {
		actions = append(actions, protocol.CodeAction{
			Title:       `Import "` + candidate.importPath + `" for "` + candidate.name + `"`,
			Kind:        protocol.QuickFix,
			Diagnostics: ridlDiagnostics,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					protocol.DocumentURI(doc.URI): {candidate.edit},
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

type unresolvedSymbol struct {
	kind referenceKind
	name string
	rng  protocol.Range
}

type missingImportCandidate struct {
	name       string
	importPath string
	edit       protocol.TextEdit
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

		edit, ok := unresolvedImportEdit(doc, ridl.TokenLine(importNode.Path())-1)
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

func (s *Server) missingImportCandidates(doc *semanticDocument) []missingImportCandidate {
	if doc == nil || !doc.valid() {
		return nil
	}

	candidates := make([]missingImportCandidate, 0, 4)
	seen := map[string]struct{}{}
	for _, unresolved := range doc.unresolvedSymbols(s.resolveTypeDefinition, s.resolveErrorDefinition) {
		targetPath, ok := s.uniqueImportCandidatePath(doc.path, unresolved.kind, unresolved.name)
		if !ok || docHasImportedPath(doc, targetPath) {
			continue
		}

		importPath, ok := relativeImportPath(doc.path, targetPath)
		if !ok {
			continue
		}

		edit, ok := missingImportEdit(doc, importPath)
		if !ok {
			continue
		}

		key := unresolved.name + ":" + importPath + ":" + editKey(edit)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		candidates = append(candidates, missingImportCandidate{
			name:       unresolved.name,
			importPath: importPath,
			edit:       edit,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].name != candidates[j].name {
			return strings.ToLower(candidates[i].name) < strings.ToLower(candidates[j].name)
		}
		return strings.ToLower(candidates[i].importPath) < strings.ToLower(candidates[j].importPath)
	})

	return candidates
}

func (d *semanticDocument) unresolvedSymbols(
	resolveType func(path string, result *ridl.ParseResult, name string) *definitionMatch,
	resolveError func(path string, result *ridl.ParseResult, name string) *definitionMatch,
) []unresolvedSymbol {
	if d == nil || !d.valid() {
		return nil
	}

	symbols := make([]unresolvedSymbol, 0, 8)
	seen := map[string]struct{}{}

	appendTypeRefs := func(token *ridl.TokenNode) {
		for _, name := range unresolvedTypeNames(token.String()) {
			if isBuiltInRIDLType(name) || resolveType(d.path, d.result, name) != nil {
				continue
			}
			for _, rng := range d.identifierRangesInToken(token, name) {
				key := name + ":" + positionKey(rng.Start)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				symbols = append(symbols, unresolvedSymbol{
					kind: referenceKindType,
					name: name,
					rng:  rng,
				})
			}
		}
	}

	appendErrorRef := func(token *ridl.TokenNode) {
		if token == nil {
			return
		}
		name := token.String()
		if name == "" || resolveError(d.path, d.result, name) != nil {
			return
		}

		rng := d.tokenRange(token, protocol.Position{})
		key := name + ":" + positionKey(rng.Start)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		symbols = append(symbols, unresolvedSymbol{
			kind: referenceKindError,
			name: name,
			rng:  rng,
		})
	}

	for _, enumNode := range d.result.Root.Enums() {
		appendTypeRefs(enumNode.TypeName())
		for _, value := range enumNode.Values() {
			appendTypeRefs(value.Right())
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		for _, field := range structNode.Fields() {
			appendTypeRefs(field.Right())
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		for _, methodNode := range serviceNode.Methods() {
			for _, input := range methodNode.Inputs() {
				appendTypeRefs(argumentTypeToken(input))
			}
			for _, output := range methodNode.Outputs() {
				appendTypeRefs(argumentTypeToken(output))
			}
			for _, errorToken := range methodNode.Errors() {
				appendErrorRef(errorToken)
			}
		}
	}

	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].rng.Start.Line != symbols[j].rng.Start.Line {
			return symbols[i].rng.Start.Line < symbols[j].rng.Start.Line
		}
		if symbols[i].rng.Start.Character != symbols[j].rng.Start.Character {
			return symbols[i].rng.Start.Character < symbols[j].rng.Start.Character
		}
		return strings.ToLower(symbols[i].name) < strings.ToLower(symbols[j].name)
	})

	return symbols
}

func locallyDefinedNames(root *ridl.RootNode) map[string]struct{} {
	names := map[string]struct{}{}
	if root == nil {
		return names
	}
	for _, enumNode := range root.Enums() {
		if enumNode != nil && enumNode.Name() != nil && enumNode.Name().String() != "" {
			names[enumNode.Name().String()] = struct{}{}
		}
	}
	for _, structNode := range root.Structs() {
		if structNode != nil && structNode.Name() != nil && structNode.Name().String() != "" {
			names[structNode.Name().String()] = struct{}{}
		}
	}
	for _, errorNode := range root.Errors() {
		if errorNode != nil && errorNode.Name() != nil && errorNode.Name().String() != "" {
			names[errorNode.Name().String()] = struct{}{}
		}
	}
	return names
}

func (d *semanticDocument) referencedNames() map[string]struct{} {
	names := map[string]struct{}{}
	if d == nil || !d.valid() {
		return names
	}

	addTypeRefs := func(token *ridl.TokenNode) {
		if token == nil {
			return
		}
		for _, name := range unresolvedTypeNames(token.String()) {
			if !isBuiltInRIDLType(name) {
				names[name] = struct{}{}
			}
		}
	}

	addErrorRef := func(token *ridl.TokenNode) {
		if token != nil && token.String() != "" {
			names[token.String()] = struct{}{}
		}
	}

	for _, enumNode := range d.result.Root.Enums() {
		addTypeRefs(enumNode.TypeName())
		for _, value := range enumNode.Values() {
			addTypeRefs(value.Right())
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		for _, field := range structNode.Fields() {
			addTypeRefs(field.Right())
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		for _, methodNode := range serviceNode.Methods() {
			for _, input := range methodNode.Inputs() {
				addTypeRefs(argumentTypeToken(input))
			}
			for _, output := range methodNode.Outputs() {
				addTypeRefs(argumentTypeToken(output))
			}
			for _, errorToken := range methodNode.Errors() {
				addErrorRef(errorToken)
			}
		}
	}

	// Remove locally defined names — we only want external references.
	for name := range locallyDefinedNames(d.result.Root) {
		delete(names, name)
	}

	return names
}

func unresolvedTypeNames(expr string) []string {
	if expr == "" {
		return nil
	}

	names := make([]string, 0, 2)
	seen := map[string]struct{}{}
	var current []rune

	flush := func() {
		if len(current) == 0 {
			return
		}
		name := string(current)
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			names = append(names, name)
		}
		current = current[:0]
	}

	for _, r := range expr {
		if isIdentifierRune(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()

	return names
}

func (s *Server) uniqueImportCandidatePath(docPath string, kind referenceKind, name string) (string, bool) {
	var definers, reExporters []string
	seen := map[string]struct{}{}

	for _, path := range s.referenceCandidatePaths() {
		if path == "" || path == docPath {
			continue
		}

		result := s.parsePathForNavigation(path)
		if result == nil || result.Root == nil {
			continue
		}

		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		// Check if name is locally defined in this file's AST (not via imports).
		var token *ridl.TokenNode
		switch kind {
		case referenceKindType:
			token = findTypeDefinitionToken(result.Root, name)
		case referenceKindError:
			token = findErrorDefinitionToken(result.Root, name)
		}

		if token != nil {
			definers = append(definers, path)
			continue
		}

		// Check if the name is available through this file's resolved schema
		// (i.e., it re-exports the name via imports).
		if result.Schema != nil && schemaHasName(result.Schema, kind, name) {
			reExporters = append(reExporters, path)
		}
	}

	// Prefer the unique definer.
	if len(definers) == 1 {
		return definers[0], true
	}
	if len(definers) > 1 {
		return "", false
	}

	// Fall back to re-exporters only if no definer found.
	if len(reExporters) == 1 {
		return reExporters[0], true
	}
	return "", false
}

func schemaHasName(s *schema.WebRPCSchema, kind referenceKind, name string) bool {
	switch kind {
	case referenceKindType:
		return s.GetTypeByName(name) != nil
	case referenceKindError:
		for _, e := range s.Errors {
			if strings.EqualFold(e.Name, name) {
				return true
			}
		}
	}
	return false
}

func docHasImportedPath(doc *semanticDocument, targetPath string) bool {
	if doc == nil || !doc.valid() {
		return false
	}

	for _, importNode := range doc.result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil {
			continue
		}
		if workspace.ResolveImportPath(doc.path, importNode.Path().String()) == targetPath {
			return true
		}
	}

	return false
}

func relativeImportPath(sourcePath, targetPath string) (string, bool) {
	if sourcePath == "" || targetPath == "" {
		return "", false
	}

	relPath, err := filepath.Rel(filepath.Dir(sourcePath), targetPath)
	if err != nil {
		return "", false
	}

	relPath = filepath.ToSlash(filepath.Clean(relPath))
	if relPath == "." || relPath == "" {
		return "", false
	}

	return relPath, true
}

func rangesOverlap(a, b protocol.Range) bool {
	return positionLess(a.Start, b.End) && positionLess(b.Start, a.End)
}

func positionLess(a, b protocol.Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}

func missingImportEdit(doc *semanticDocument, importPath string) (protocol.TextEdit, bool) {
	if doc == nil || importPath == "" {
		return protocol.TextEdit{}, false
	}

	lines := splitContentLines(doc.content)
	imports := doc.result.Root.Imports()
	if len(imports) > 0 {
		if imports[0] == nil || imports[0].Path() == nil || imports[len(imports)-1] == nil || imports[len(imports)-1].Path() == nil {
			return protocol.TextEdit{}, false
		}

		firstLine := ridl.TokenLine(imports[0].Path()) - 1
		lastLine := ridl.TokenLine(imports[len(imports)-1].Path()) - 1
		if firstLine >= 0 && firstLine < len(lines) && strings.HasPrefix(trimmedLine(lines[firstLine]), "import ") {
			return protocol.TextEdit{
				Range: lineDeletionRange(doc, lines, firstLine, firstLine+1),
				NewText: "import\n" +
					"  - " + imports[0].Path().String() + "\n" +
					"  - " + importPath + "\n",
			}, true
		}

		insertOffset := lineStartOffset(lines, lastLine+1)
		insertPos, ok := doc.positionAtOffset(insertOffset)
		if !ok {
			return protocol.TextEdit{}, false
		}

		return protocol.TextEdit{
			Range:   protocol.Range{Start: insertPos, End: insertPos},
			NewText: "  - " + importPath + "\n",
		}, true
	}

	insertLine := importInsertLine(lines)
	insertOffset := lineStartOffset(lines, insertLine)
	insertPos, ok := doc.positionAtOffset(insertOffset)
	if !ok {
		return protocol.TextEdit{}, false
	}

	prefix := ""
	if insertLine > 0 && trimmedLine(lines[insertLine-1]) != "" {
		prefix = "\n"
	}

	suffix := ""
	if insertLine < len(lines) && trimmedLine(lines[insertLine]) != "" {
		suffix = "\n"
	}

	return protocol.TextEdit{
		Range: protocol.Range{Start: insertPos, End: insertPos},
		NewText: prefix + "import\n" +
			"  - " + importPath + "\n" +
			suffix,
	}, true
}

func importInsertLine(lines []string) int {
	for i, line := range lines {
		trimmed := trimmedLine(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "service ") ||
			strings.HasPrefix(trimmed, "struct ") ||
			strings.HasPrefix(trimmed, "enum ") ||
			strings.HasPrefix(trimmed, "error ") ||
			trimmed == "import" ||
			strings.HasPrefix(trimmed, "import ") {
			return i
		}
	}

	return len(lines)
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

func (s *Server) narrowImportCodeActions(doc *documents.Document, diagnostics []protocol.Diagnostic) []protocol.CodeAction {
	if doc == nil || doc.Result == nil || doc.Result.Root == nil {
		return nil
	}

	var actions []protocol.CodeAction
	for _, diag := range diagnostics {
		if diag.Source != "ridl" || diag.Severity != protocol.DiagnosticSeverityWarning {
			continue
		}

		isUnused := strings.Contains(diag.Message, "is unused")
		isNarrowable := strings.Contains(diag.Message, "can be narrowed")
		if !isUnused && !isNarrowable {
			continue
		}

		lineIndex := int(diag.Range.Start.Line)
		semanticDoc := newSemanticDocument(doc.Path, doc.Content, doc.Result)

		if isUnused {
			edit, ok := unresolvedImportEdit(semanticDoc, lineIndex)
			if !ok {
				continue
			}
			// Extract import path from message: Import "X" is unused
			importPath := extractQuotedString(diag.Message)
			actions = append(actions, protocol.CodeAction{
				Title:       `Remove unused import "` + importPath + `"`,
				Kind:        protocol.QuickFix,
				Diagnostics: []protocol.Diagnostic{diag},
				IsPreferred: true,
				Edit: &protocol.WorkspaceEdit{
					Changes: map[protocol.DocumentURI][]protocol.TextEdit{
						protocol.DocumentURI(doc.URI): {edit},
					},
				},
			})
			continue
		}

		// Narrowable: extract used names from message after "can be narrowed to: "
		importPath := extractQuotedString(diag.Message)
		idx := strings.Index(diag.Message, "can be narrowed to: ")
		if idx < 0 {
			continue
		}
		nameList := diag.Message[idx+len("can be narrowed to: "):]
		names := strings.Split(nameList, ", ")

		lines := splitContentLines(doc.Content)
		if lineIndex < 0 || lineIndex >= len(lines) {
			continue
		}

		var memberLines string
		for _, name := range names {
			memberLines += "    - " + name + "\n"
		}

		actions = append(actions, protocol.CodeAction{
			Title:       `Narrow import "` + importPath + `"`,
			Kind:        protocol.QuickFix,
			Diagnostics: []protocol.Diagnostic{diag},
			IsPreferred: true,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					protocol.DocumentURI(doc.URI): {
						{
							Range: protocol.Range{
								Start: protocol.Position{Line: uint32(lineIndex), Character: uint32(len(strings.TrimRight(lines[lineIndex], "\n")))},
								End:   protocol.Position{Line: uint32(lineIndex), Character: uint32(len(strings.TrimRight(lines[lineIndex], "\n")))},
							},
							NewText: "\n" + memberLines[:len(memberLines)-1], // trim trailing newline; the existing line already has one
						},
					},
				},
			},
		})
	}

	return actions
}

func extractQuotedString(s string) string {
	start := strings.IndexByte(s, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(s[start+1:], '"')
	if end < 0 {
		return ""
	}
	return s[start+1 : start+1+end]
}
