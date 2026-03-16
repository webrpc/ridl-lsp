package lsp

import (
	"context"
	"fmt"

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

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return []protocol.CodeLens{}, nil
	}

	return semanticDoc.codeLenses(), nil
}

func (s *Server) CodeLensResolve(ctx context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) {
	if params == nil || params.Command != nil {
		return params, nil
	}

	data, ok := decodeCodeLensData(params.Data)
	if !ok {
		return params, nil
	}

	doc, ok := s.docs.Get(data.URI)
	if !ok {
		return params, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return params, nil
	}

	target := semanticDoc.referenceTargetAt(protocol.Position{
		Line:      data.Line,
		Character: data.Character,
	}, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if target == nil || target.definition == nil {
		return params, nil
	}

	locations := s.collectReferenceLocations(target, false)
	params.Command = &protocol.Command{
		Title:     referenceCountTitle(len(locations)),
		Command:   showReferencesCommand,
		Arguments: []any{protocol.DocumentURI(data.URI), params.Range.Start, locations},
	}
	return params, nil
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
