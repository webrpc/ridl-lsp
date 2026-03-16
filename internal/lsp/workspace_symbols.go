package lsp

import (
	"context"
	"sort"
	"strings"

	"go.lsp.dev/protocol"
)

func (s *Server) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) ([]protocol.SymbolInformation, error) {
	query := ""
	if params != nil {
		query = params.Query
	}

	symbols := make([]protocol.SymbolInformation, 0, 16)
	for _, path := range s.referenceCandidatePaths() {
		result := s.parsePathForNavigation(path)
		if result == nil || result.Root == nil {
			continue
		}

		content, ok := s.contentForPath(path)
		if !ok {
			continue
		}

		semanticDoc := newSemanticDocument(path, content, result)
		symbols = append(symbols, semanticDoc.workspaceSymbols(query)...)
	}

	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].Name != symbols[j].Name {
			return strings.ToLower(symbols[i].Name) < strings.ToLower(symbols[j].Name)
		}
		if symbols[i].ContainerName != symbols[j].ContainerName {
			return strings.ToLower(symbols[i].ContainerName) < strings.ToLower(symbols[j].ContainerName)
		}
		if symbols[i].Location.URI != symbols[j].Location.URI {
			return string(symbols[i].Location.URI) < string(symbols[j].Location.URI)
		}
		if symbols[i].Location.Range.Start.Line != symbols[j].Location.Range.Start.Line {
			return symbols[i].Location.Range.Start.Line < symbols[j].Location.Range.Start.Line
		}
		return symbols[i].Location.Range.Start.Character < symbols[j].Location.Range.Start.Character
	})

	return symbols, nil
}

func (d *semanticDocument) workspaceSymbols(query string) []protocol.SymbolInformation {
	if !d.valid() {
		return nil
	}

	query = strings.TrimSpace(strings.ToLower(query))
	symbols := make([]protocol.SymbolInformation, 0, len(d.result.Root.Enums())+len(d.result.Root.Structs())+len(d.result.Root.Errors())+len(d.result.Root.Services()))

	appendSymbol := func(name string, kind protocol.SymbolKind, token *protocol.Range, containerName string) {
		if token == nil || !workspaceSymbolMatches(name, query) {
			return
		}
		symbols = append(symbols, protocol.SymbolInformation{
			Name:          name,
			Kind:          kind,
			Location:      protocol.Location{URI: PathToURI(d.path), Range: *token},
			ContainerName: containerName,
		})
	}

	for _, enumNode := range d.result.Root.Enums() {
		rng := d.tokenRange(enumNode.Name(), protocol.Position{})
		appendSymbol(enumNode.Name().String(), protocol.SymbolKindEnum, &rng, "")
	}

	for _, structNode := range d.result.Root.Structs() {
		rng := d.tokenRange(structNode.Name(), protocol.Position{})
		appendSymbol(structNode.Name().String(), protocol.SymbolKindStruct, &rng, "")
	}

	for _, errorNode := range d.result.Root.Errors() {
		rng := d.tokenRange(errorNode.Name(), protocol.Position{})
		appendSymbol(errorNode.Name().String(), protocol.SymbolKindObject, &rng, "")
	}

	for _, serviceNode := range d.result.Root.Services() {
		rng := d.tokenRange(serviceNode.Name(), protocol.Position{})
		serviceName := serviceNode.Name().String()
		appendSymbol(serviceName, protocol.SymbolKindInterface, &rng, "")

		for _, methodNode := range serviceNode.Methods() {
			methodRange := d.tokenRange(methodNode.Name(), protocol.Position{})
			appendSymbol(methodNode.Name().String(), protocol.SymbolKindMethod, &methodRange, serviceName)
		}
	}

	return symbols
}

func workspaceSymbolMatches(name, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), query)
}
