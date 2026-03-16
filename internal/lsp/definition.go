package lsp

import (
	"context"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/webrpc/ridl-lsp/internal/workspace"
	ridl "github.com/webrpc/webrpc/schema/ridl"
)

func (s *Server) Definition(ctx context.Context, params *protocol.DefinitionParams) ([]protocol.Location, error) {
	doc, ok := s.docs.Get(string(params.TextDocument.URI))
	if !ok {
		return nil, nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, s.parsePathForNavigation(doc.Path))
	if !semanticDoc.valid() {
		return nil, nil
	}

	match := semanticDoc.definitionAt(params.Position, s.resolveTypeDefinition, s.resolveErrorDefinition)
	if match == nil {
		return nil, nil
	}

	return []protocol.Location{match.location()}, nil
}

type definitionMatch struct {
	path  string
	token *ridl.TokenNode
}

func (m *definitionMatch) location() protocol.Location {
	return protocol.Location{
		URI:   PathToURI(m.path),
		Range: fallbackTokenRange(m.token),
	}
}

func definitionForToken(path string, token *ridl.TokenNode) *definitionMatch {
	if token == nil {
		return nil
	}
	return &definitionMatch{path: path, token: token}
}

func (s *Server) parsePathForNavigation(path string) *ridl.ParseResult {
	if doc, ok := s.docs.FindByPath(path); ok && doc.Result != nil && doc.Result.Root != nil {
		return doc.Result
	}

	result, err := s.parser.Parse(s.workspace.Root(), path, s.overlayContents())
	if err != nil {
		return nil
	}
	return result
}

func (s *Server) resolveTypeDefinition(path string, result *ridl.ParseResult, name string) *definitionMatch {
	if name == "" || isBuiltInRIDLType(name) {
		return nil
	}
	return s.resolveNamedDefinition(path, result, name, findTypeDefinitionToken)
}

func (s *Server) resolveErrorDefinition(path string, result *ridl.ParseResult, name string) *definitionMatch {
	if name == "" {
		return nil
	}
	return s.resolveNamedDefinition(path, result, name, findErrorDefinitionToken)
}

type definitionFinder func(root *ridl.RootNode, name string) *ridl.TokenNode

func (s *Server) resolveNamedDefinition(path string, result *ridl.ParseResult, name string, finder definitionFinder) *definitionMatch {
	if result == nil || result.Root == nil {
		return nil
	}

	if token := finder(result.Root, name); token != nil {
		return definitionForToken(path, token)
	}

	visited := map[string]struct{}{path: {}}
	return s.resolveImportedDefinition(path, result.Root, name, visited, finder)
}

func (s *Server) resolveImportedDefinition(path string, root *ridl.RootNode, name string, visited map[string]struct{}, finder definitionFinder) *definitionMatch {
	if root == nil {
		return nil
	}

	for _, importNode := range root.Imports() {
		if !importAllowsName(importNode, name) {
			continue
		}

		importPath := workspace.ResolveImportPath(path, importNode.Path().String())
		if _, seen := visited[importPath]; seen {
			continue
		}
		visited[importPath] = struct{}{}

		importResult := s.parsePathForNavigation(importPath)
		if importResult == nil || importResult.Root == nil {
			continue
		}

		if token := finder(importResult.Root, name); token != nil {
			return definitionForToken(importPath, token)
		}

		if match := s.resolveImportedDefinition(importPath, importResult.Root, name, visited, finder); match != nil {
			return match
		}
	}

	return nil
}

func findTypeDefinitionToken(root *ridl.RootNode, name string) *ridl.TokenNode {
	if root == nil {
		return nil
	}
	for _, enumNode := range root.Enums() {
		if strings.EqualFold(enumNode.Name().String(), name) {
			return enumNode.Name()
		}
	}
	for _, structNode := range root.Structs() {
		if strings.EqualFold(structNode.Name().String(), name) {
			return structNode.Name()
		}
	}
	return nil
}

func findErrorDefinitionToken(root *ridl.RootNode, name string) *ridl.TokenNode {
	if root == nil {
		return nil
	}
	for _, errorNode := range root.Errors() {
		if strings.EqualFold(errorNode.Name().String(), name) {
			return errorNode.Name()
		}
	}
	return nil
}

func importAllowsName(importNode *ridl.ImportNode, name string) bool {
	if importNode == nil {
		return false
	}
	members := importNode.Members()
	if len(members) == 0 {
		return true
	}
	for _, member := range members {
		if strings.EqualFold(member.String(), name) {
			return true
		}
	}
	return false
}

func isBuiltInRIDLType(name string) bool {
	_, ok := schemaCoreType(name)
	return ok
}

func schemaCoreType(name string) (string, bool) {
	if _, ok := coreRIDLTypes[name]; ok {
		return name, true
	}
	return "", false
}

var coreRIDLTypes = map[string]struct{}{
	"null":      {},
	"any":       {},
	"byte":      {},
	"bool":      {},
	"uint":      {},
	"uint8":     {},
	"uint16":    {},
	"uint32":    {},
	"uint64":    {},
	"int":       {},
	"int8":      {},
	"int16":     {},
	"int32":     {},
	"int64":     {},
	"bigint":    {},
	"float32":   {},
	"float64":   {},
	"string":    {},
	"timestamp": {},
	"map":       {},
}
