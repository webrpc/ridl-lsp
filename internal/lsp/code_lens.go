package lsp

import (
	"context"
	"fmt"
	"path/filepath"

	"go.lsp.dev/protocol"

	ridl "github.com/webrpc/ridl-lsp/internal/ridl"
)

const showReferencesCommand = "ridl.showReferences"

type codeLensData struct {
	URI       string `json:"uri"`
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

func (s *Server) CodeLens(ctx context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return []protocol.CodeLens{}, nil
	}

	parse := s.newRequestParse()
	semanticDoc := newSemanticDocument(doc.Path, doc.Content, parse(doc.Path))
	if !semanticDoc.valid() {
		return []protocol.CodeLens{}, nil
	}

	// Resolve every lens up front in one workspace pass: the client then never
	// calls CodeLensResolve, so there is no per-version command memo to go stale
	// when a cross-file reference changes.
	candidatePaths := s.referenceCandidatePaths()
	resolveType := func(path string, result *ridl.ParseResult, name string) *definitionMatch {
		return resolveTypeDefinitionWith(parse, path, result, name)
	}
	resolveError := func(path string, result *ridl.ParseResult, name string) *definitionMatch {
		return resolveErrorDefinitionWith(parse, path, result, name)
	}

	lenses := semanticDoc.codeLenses()
	for i := range lenses {
		data, ok := decodeCodeLensData(lenses[i].Data)
		if !ok {
			continue
		}
		target := semanticDoc.referenceTargetAt(
			protocol.Position{Line: data.Line, Character: data.Character},
			resolveType, resolveError,
		)
		if target == nil || target.definition == nil {
			continue
		}
		locations := collectReferenceLocationsWith(parse, candidatePaths, s.contentForPath, target, false, resolveType, resolveError)
		lenses[i].Command = &protocol.Command{
			Title:     referenceCountTitle(len(locations)),
			Command:   showReferencesCommand,
			Arguments: []any{protocol.DocumentURI(data.URI), lenses[i].Range.Start, locations},
		}
		lenses[i].Data = nil
	}

	return lenses, nil
}

// CodeLensResolve is a passthrough: CodeLens returns fully-resolved lenses, so
// resolution never needs to recompute.
func (s *Server) CodeLensResolve(ctx context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) {
	return params, nil
}

// newRequestParse builds a request-scoped parser that AST-parses each file at
// most once: an open buffer's already-built result is reused; everything else is
// parsed AST-only (no schema build, no import recursion). Keyed by cleaned path
// so each file is parsed once per request.
func (s *Server) newRequestParse() parseFn {
	overlays := s.overlayContents()
	memo := map[string]*ridl.ParseResult{}
	return func(path string) *ridl.ParseResult {
		key := filepath.Clean(path)
		if result, ok := memo[key]; ok {
			return result
		}
		var result *ridl.ParseResult
		if doc, ok := s.docs.FindByPath(path); ok && doc.Result != nil && doc.Result.Root != nil {
			result = doc.Result
		} else {
			result, _ = s.parser.ParseAST(context.Background(), s.workspace.Root(), path, overlays)
		}
		memo[key] = result
		return result
	}
}

func (d *semanticDocument) codeLenses() []protocol.CodeLens {
	lenses := make([]protocol.CodeLens, 0, len(d.result.Root.Enums())+len(d.result.Root.Structs())+len(d.result.Root.Errors()))

	appendLens := func(token *ridl.TokenNode) {
		if !validSymbolToken(token) {
			return
		}

		rng := d.tokenRange(token, protocol.Position{})
		lenses = append(lenses, protocol.CodeLens{
			Range: rng,
			Data: codeLensData{
				URI:       string(PathToURI(d.path)),
				Line:      rng.Start.Line,
				Character: rng.Start.Character,
			},
		})
	}

	for _, enumNode := range d.result.Root.Enums() {
		appendLens(enumNode.Name())
	}
	for _, structNode := range d.result.Root.Structs() {
		appendLens(structNode.Name())
	}
	for _, errorNode := range d.result.Root.Errors() {
		appendLens(errorNode.Name())
	}

	return lenses
}

func decodeCodeLensData(data any) (codeLensData, bool) {
	switch value := data.(type) {
	case codeLensData:
		return value, value.URI != ""
	case map[string]any:
		uri, ok := value["uri"].(string)
		if !ok || uri == "" {
			return codeLensData{}, false
		}
		line, ok := lensNumber(value["line"])
		if !ok {
			return codeLensData{}, false
		}
		character, ok := lensNumber(value["character"])
		if !ok {
			return codeLensData{}, false
		}
		return codeLensData{URI: uri, Line: line, Character: character}, true
	default:
		return codeLensData{}, false
	}
}

func lensNumber(value any) (uint32, bool) {
	switch number := value.(type) {
	case uint32:
		return number, true
	case int:
		if number < 0 {
			return 0, false
		}
		return uint32(number), true
	case float64:
		if number < 0 {
			return 0, false
		}
		return uint32(number), true
	default:
		return 0, false
	}
}

func referenceCountTitle(count int) string {
	if count == 1 {
		return "1 reference"
	}
	return fmt.Sprintf("%d references", count)
}
