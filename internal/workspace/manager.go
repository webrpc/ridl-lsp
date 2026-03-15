package workspace

import (
	"net/url"
	"path/filepath"
	"strings"

	"go.lsp.dev/protocol"
)

type Manager struct {
	root string
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) SetRootFromURI(uri string) {
	m.root = URIToPath(uri)
}

func (m *Manager) SetRoot(path string) {
	m.root = path
}

func (m *Manager) Root() string {
	return m.root
}

func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return uri
	}

	path := u.Path
	if u.Host != "" {
		path = "//" + u.Host + path
	}

	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	return filepath.FromSlash(path)
}

func PathToURI(p string) protocol.DocumentURI {
	if p == "" {
		return ""
	}

	if absPath, err := filepath.Abs(p); err == nil {
		p = absPath
	}

	slashPath := filepath.ToSlash(p)
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}

	return protocol.DocumentURI((&url.URL{
		Scheme: "file",
		Path:   slashPath,
	}).String())
}
