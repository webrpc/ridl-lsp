package ridl

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"github.com/webrpc/webrpc/schema"
	upridl "github.com/webrpc/webrpc/schema/ridl"
)

type ParseResult struct {
	Root   *RootNode
	Schema *schema.WebRPCSchema
	Errors []error
}

type RootNode = upridl.RootNode
type TokenNode = upridl.TokenNode
type DefinitionNode = upridl.DefinitionNode
type ImportNode = upridl.ImportNode
type EnumNode = upridl.EnumNode
type StructNode = upridl.StructNode
type ErrorNode = upridl.ErrorNode
type ArgumentNode = upridl.ArgumentNode
type AnnotationNode = upridl.AnnotationNode
type MethodNode = upridl.MethodNode
type ServiceNode = upridl.ServiceNode

type upstreamNode struct {
	children []upridl.Node
	start    int
	end      int
}

type upstreamTokenType uint8

type upstreamToken struct {
	tt  upstreamTokenType
	val string

	pos  int
	line int
	col  int
}

type upstreamParser struct {
	file   string
	tokens []upstreamToken
	length int
	pos    int

	words    chan interface{}
	comments map[int]string

	root RootNode
}

type upstreamTokenNode struct {
	node upstreamNode
	tok  *upstreamToken
}

type upstreamErrorNode struct {
	node       upstreamNode
	code       *TokenNode
	name       *TokenNode
	message    *TokenNode
	httpStatus *TokenNode
}

type upstreamArgumentNode struct {
	node         upstreamNode
	name         *TokenNode
	argumentType *TokenNode
	optional     bool
	inlineStruct *TokenNode
}

//go:linkname newUpstreamParser github.com/webrpc/webrpc/schema/ridl.newParser
func newUpstreamParser(file string, src []byte) (*upstreamParser, error)

//go:linkname runUpstreamParser github.com/webrpc/webrpc/schema/ridl.(*parser).run
func runUpstreamParser(parser *upstreamParser) error

func TokenPos(token *TokenNode) int {
	if token == nil {
		return 0
	}
	return token.Start()
}

func TokenLine(token *TokenNode) int {
	if token == nil {
		return 0
	}

	mirror := (*upstreamTokenNode)(unsafe.Pointer(token))
	if mirror.tok == nil {
		return 0
	}
	return mirror.tok.line
}

func TokenCol(token *TokenNode) int {
	if token == nil {
		return 0
	}

	mirror := (*upstreamTokenNode)(unsafe.Pointer(token))
	if mirror.tok == nil {
		return 0
	}
	return mirror.tok.col
}

func ErrorCodeToken(node *ErrorNode) *TokenNode {
	if node == nil {
		return nil
	}
	return (*upstreamErrorNode)(unsafe.Pointer(node)).code
}

func ErrorMessageToken(node *ErrorNode) *TokenNode {
	if node == nil {
		return nil
	}
	return (*upstreamErrorNode)(unsafe.Pointer(node)).message
}

func ErrorHTTPStatusToken(node *ErrorNode) *TokenNode {
	if node == nil {
		return nil
	}
	return (*upstreamErrorNode)(unsafe.Pointer(node)).httpStatus
}

func ArgumentTypeToken(node *ArgumentNode) *TokenNode {
	if node == nil {
		return nil
	}

	if token := node.TypeName(); token != nil && token.String() != "" {
		return token
	}

	return (*upstreamArgumentNode)(unsafe.Pointer(node)).inlineStruct
}

// Parser wraps the upstream RIDL parser, providing an overlay-aware filesystem
// so that unsaved editor buffers are visible during parsing.
type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

// Parse parses the RIDL file at path using workspace as the preferred fs root.
// overlays maps document paths to in-memory content (open editor buffers).
func (p *Parser) Parse(workspace, path string, overlays map[string]string) (*ParseResult, error) {
	return p.parse(workspace, path, overlays, map[string]struct{}{})
}

func (p *Parser) parse(workspace, path string, overlays map[string]string, visited map[string]struct{}) (*ParseResult, error) {
	fsys, root, relPath, err := parserFS(workspace, path, overlays)
	if err != nil {
		return nil, err
	}

	src, err := fs.ReadFile(fsys, relPath)
	if err != nil {
		return nil, err
	}

	result := &ParseResult{}

	astParser, err := newUpstreamParser(relPath, src)
	if err != nil {
		result.Errors = []error{err}
		return result, nil
	}

	if err := runUpstreamParser(astParser); err != nil {
		rootNode := astParser.root
		result.Root = &rootNode
		result.Schema = p.buildPartialSchema(workspace, path, result.Root, overlays, visited)
		result.Errors = []error{err}
		return result, nil
	}

	rootNode := astParser.root
	result.Root = &rootNode
	imported := len(visited) > 0
	result.Schema = p.buildPartialSchema(workspace, path, result.Root, overlays, visited)

	schemaDoc, err := upridl.NewParser(fsys, root, relPath).Parse()
	if err != nil {
		if isVersionOptionalSchemaError(err, imported) {
			return result, nil
		}
		result.Errors = []error{err}
		return result, nil
	}

	result.Schema = schemaDoc
	return result, nil
}

func isVersionOptionalSchemaError(err error, imported bool) bool {
	if err == nil {
		return false
	}

	if !strings.Contains(err.Error(), "schema error: version is required when services are defined") {
		return false
	}

	return imported || strings.Contains(err.Error(), "stack trace:")
}

func (p *Parser) buildPartialSchema(workspace, path string, root *RootNode, overlays map[string]string, visited map[string]struct{}) *schema.WebRPCSchema {
	doc := &schema.WebRPCSchema{
		Types:    []*schema.Type{},
		Errors:   []*schema.Error{},
		Services: []*schema.Service{},
	}
	if root == nil {
		return doc
	}

	for _, line := range root.Definitions() {
		if line == nil || line.Left() == nil || line.Right() == nil {
			continue
		}

		switch line.Left().String() {
		case "webrpc":
			doc.WebrpcVersion = line.Right().String()
		case "name":
			doc.SchemaName = line.Right().String()
		case "version":
			doc.SchemaVersion = line.Right().String()
		case "basepath":
			doc.BasePath = line.Right().String()
		}
	}

	if visited == nil {
		visited = map[string]struct{}{}
	}
	visited[path] = struct{}{}

	for _, importNode := range root.Imports() {
		if importNode == nil || importNode.Path() == nil || importNode.Path().String() == "" {
			continue
		}

		importPath := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(importNode.Path().String())))
		if _, seen := visited[importPath]; seen {
			continue
		}

		importResult, err := p.parse(workspace, importPath, overlays, cloneVisited(visited))
		if err != nil || importResult == nil || importResult.Schema == nil {
			continue
		}

		mergeImportedSchema(doc, importResult.Schema, importNode)
	}

	for _, enumNode := range root.Enums() {
		if enumNode == nil || enumNode.Name() == nil || enumNode.Name().String() == "" {
			continue
		}

		enumType := &schema.Type{
			Kind:     schema.TypeKind_Enum,
			Name:     enumNode.Name().String(),
			Type:     &schema.VarType{Expr: enumNode.TypeName().String()},
			Fields:   []*schema.TypeField{},
			Comments: commentLines(enumNode.Comments()),
		}

		for _, value := range enumNode.Values() {
			if value == nil || value.Left() == nil || value.Left().String() == "" {
				continue
			}
			enumType.Fields = append(enumType.Fields, &schema.TypeField{
				Name:     value.Left().String(),
				Comments: commentLines(value.Comment()),
				TypeExtra: schema.TypeExtra{
					Value: value.Right().String(),
				},
			})
		}

		doc.Types = append(doc.Types, enumType)
	}

	for _, structNode := range root.Structs() {
		if structNode == nil || structNode.Name() == nil || structNode.Name().String() == "" {
			continue
		}

		structType := &schema.Type{
			Kind:     schema.TypeKind_Struct,
			Name:     structNode.Name().String(),
			Fields:   []*schema.TypeField{},
			Comments: commentLines(structNode.Comment()),
		}

		for _, field := range structNode.Fields() {
			if field == nil || field.Left() == nil || field.Left().String() == "" {
				continue
			}

			structType.Fields = append(structType.Fields, &schema.TypeField{
				Name:     field.Left().String(),
				Comments: commentLines(field.Comment()),
				Type:     &schema.VarType{Expr: field.Right().String()},
				TypeExtra: schema.TypeExtra{
					Optional: field.Optional(),
				},
			})
		}

		doc.Types = append(doc.Types, structType)
	}

	for _, errorNode := range root.Errors() {
		if errorNode == nil || errorNode.Name() == nil || errorNode.Name().String() == "" {
			continue
		}

		doc.Errors = append(doc.Errors, &schema.Error{
			Code:       parseTokenInt(ErrorCodeToken(errorNode)),
			Name:       errorNode.Name().String(),
			Message:    ErrorMessageToken(errorNode).String(),
			HTTPStatus: parseTokenInt(ErrorHTTPStatusToken(errorNode)),
		})
	}

	for _, serviceNode := range root.Services() {
		if serviceNode == nil || serviceNode.Name() == nil || serviceNode.Name().String() == "" {
			continue
		}

		service := &schema.Service{
			Name:     serviceNode.Name().String(),
			Comments: commentLines(serviceNode.Comment()),
			Methods:  []*schema.Method{},
		}

		for _, methodNode := range serviceNode.Methods() {
			if methodNode == nil || methodNode.Name() == nil || methodNode.Name().String() == "" {
				continue
			}

			method := &schema.Method{
				Name:         methodNode.Name().String(),
				Proxy:        methodNode.Proxy(),
				StreamInput:  methodNode.StreamInput(),
				StreamOutput: methodNode.StreamOutput(),
				Comments:     commentLines(methodNode.Comment()),
				Inputs:       []*schema.MethodArgument{},
				Outputs:      []*schema.MethodArgument{},
				Errors:       []string{},
			}

			for _, input := range methodNode.Inputs() {
				method.Inputs = append(method.Inputs, partialMethodArgument(input))
			}
			for _, output := range methodNode.Outputs() {
				method.Outputs = append(method.Outputs, partialMethodArgument(output))
			}
			for _, errorToken := range methodNode.Errors() {
				if errorToken == nil || errorToken.String() == "" {
					continue
				}
				method.Errors = append(method.Errors, errorToken.String())
			}

			service.Methods = append(service.Methods, method)
		}

		doc.Services = append(doc.Services, service)
	}

	return doc
}

func partialMethodArgument(arg *ArgumentNode) *schema.MethodArgument {
	if arg == nil {
		return &schema.MethodArgument{Type: &schema.VarType{}}
	}

	typeExpr := ""
	if token := ArgumentTypeToken(arg); token != nil {
		typeExpr = token.String()
	}

	return &schema.MethodArgument{
		Name:     arg.Name().String(),
		Optional: arg.Optional(),
		Type:     &schema.VarType{Expr: typeExpr},
	}
}

func mergeImportedSchema(dst, src *schema.WebRPCSchema, importNode *ImportNode) {
	if dst == nil || src == nil {
		return
	}

	allowed := map[string]struct{}{}
	for _, member := range importNode.Members() {
		if member == nil || member.String() == "" {
			continue
		}
		allowed[strings.ToLower(member.String())] = struct{}{}
	}

	allowAll := len(allowed) == 0
	typeSeen := existingNames(dst.Types, func(typ *schema.Type) string { return typ.Name })
	errorSeen := existingNames(dst.Errors, func(schemaError *schema.Error) string { return schemaError.Name })
	serviceSeen := existingNames(dst.Services, func(service *schema.Service) string { return service.Name })

	for _, typ := range src.Types {
		if typ == nil || typ.Name == "" || (!allowAll && !nameAllowed(allowed, typ.Name)) || typeSeen[strings.ToLower(typ.Name)] {
			continue
		}
		typeSeen[strings.ToLower(typ.Name)] = true
		dst.Types = append(dst.Types, typ)
	}

	for _, schemaError := range src.Errors {
		if schemaError == nil || schemaError.Name == "" || (!allowAll && !nameAllowed(allowed, schemaError.Name)) || errorSeen[strings.ToLower(schemaError.Name)] {
			continue
		}
		errorSeen[strings.ToLower(schemaError.Name)] = true
		dst.Errors = append(dst.Errors, schemaError)
	}

	for _, service := range src.Services {
		if service == nil || service.Name == "" || (!allowAll && !nameAllowed(allowed, service.Name)) || serviceSeen[strings.ToLower(service.Name)] {
			continue
		}
		serviceSeen[strings.ToLower(service.Name)] = true
		dst.Services = append(dst.Services, service)
	}
}

func existingNames[T any](items []*T, name func(*T) string) map[string]bool {
	seen := map[string]bool{}
	for _, item := range items {
		if item == nil {
			continue
		}
		if itemName := strings.ToLower(name(item)); itemName != "" {
			seen[itemName] = true
		}
	}
	return seen
}

func nameAllowed(allowed map[string]struct{}, name string) bool {
	_, ok := allowed[strings.ToLower(name)]
	return ok
}

func cloneVisited(visited map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(visited)+1)
	for path := range visited {
		cloned[path] = struct{}{}
	}
	return cloned
}

func commentLines(comment string) []string {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return nil
	}
	return []string{comment}
}

func parseTokenInt(token *TokenNode) int {
	if token == nil || token.String() == "" {
		return 0
	}
	value, err := strconv.Atoi(token.String())
	if err != nil {
		return 0
	}
	return value
}

func parserFS(workspace, path string, overlays map[string]string) (fs.FS, string, string, error) {
	if workspace != "" {
		if relPath, ok := fsRelativePath(workspace, path); ok {
			return newOverlayFS(os.DirFS(workspace), overlaysForRoot(workspace, overlays)), workspace, relPath, nil
		}
	}

	root := filesystemRoot(path)
	if relPath, ok := fsRelativePath(root, path); ok {
		return newOverlayFS(os.DirFS(root), overlaysForRoot(root, overlays)), root, relPath, nil
	}

	docDir := filepath.Dir(path)
	docBase := filepath.Base(path)
	return newOverlayFS(os.DirFS(docDir), overlaysForRoot(docDir, overlays)), docDir, docBase, nil
}

func overlaysForRoot(root string, overlays map[string]string) map[string]string {
	rootOverlays := make(map[string]string, len(overlays))
	for path, content := range overlays {
		relPath, ok := fsRelativePath(root, path)
		if !ok {
			continue
		}
		rootOverlays[relPath] = content
	}
	return rootOverlays
}

func fsRelativePath(root, path string) (string, bool) {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}

	relPath = filepath.Clean(relPath)
	if relPath == "." {
		return "", false
	}

	relPath = filepath.ToSlash(relPath)
	if relPath == "." || strings.HasPrefix(relPath, "../") || relPath == ".." {
		return "", false
	}
	if !fs.ValidPath(relPath) {
		return "", false
	}
	return relPath, true
}

func filesystemRoot(path string) string {
	cleanPath := filepath.Clean(path)
	if volume := filepath.VolumeName(cleanPath); volume != "" {
		return volume + string(filepath.Separator)
	}
	return string(filepath.Separator)
}
