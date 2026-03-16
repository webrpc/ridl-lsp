package workspace

import "path/filepath"

func ResolveImportPath(sourcePath, importPath string) string {
	if sourcePath == "" || importPath == "" {
		return filepath.Clean(importPath)
	}

	return filepath.Clean(filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(importPath)))
}
