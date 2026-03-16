package ridl

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/webrpc/webrpc/schema/ridl"
)

// ParseResult wraps the upstream ridl.ParseResult for use across the LSP.
type ParseResult = ridl.ParseResult

// Parser wraps the upstream RIDL parser, providing an overlay-aware filesystem
// so that unsaved editor buffers are visible during parsing.
type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

// Parse parses the RIDL file at path using workspace as the preferred fs root.
// overlays maps document paths to in-memory content (open editor buffers).
func (p *Parser) Parse(workspace, path string, overlays map[string]string) (*ParseResult, error) {
	fsys, root, relPath, err := parserFS(workspace, path, overlays)
	if err != nil {
		return nil, err
	}

	parser := ridl.NewParser(fsys, root, relPath)
	return parser.ParseForLSP()
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
