package ridl

import (
	"io/fs"
	"strings"
	"time"
)

// overlayFS wraps a base fs.FS and overlays in-memory file contents.
type overlayFS struct {
	base     fs.FS
	overlays map[string]string
}

func newOverlayFS(base fs.FS, overlays map[string]string) *overlayFS {
	return &overlayFS{base: base, overlays: overlays}
}

func (ofs *overlayFS) Open(name string) (fs.File, error) {
	if content, ok := ofs.overlays[name]; ok {
		return &memFile{
			name:    name,
			content: strings.NewReader(content),
			size:    int64(len(content)),
		}, nil
	}
	return ofs.base.Open(name)
}

func (ofs *overlayFS) ReadFile(name string) ([]byte, error) {
	if content, ok := ofs.overlays[name]; ok {
		return []byte(content), nil
	}
	return fs.ReadFile(ofs.base, name)
}

type memFile struct {
	name    string
	content *strings.Reader
	size    int64
}

func (f *memFile) Read(b []byte) (int, error) {
	return f.content.Read(b)
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: f.size}, nil
}

func (f *memFile) Close() error {
	return nil
}

type memFileInfo struct {
	name string
	size int64
}

func (fi *memFileInfo) Name() string       { return fi.name }
func (fi *memFileInfo) Size() int64        { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode  { return 0444 }
func (fi *memFileInfo) ModTime() time.Time { return time.Now() }
func (fi *memFileInfo) IsDir() bool        { return false }
func (fi *memFileInfo) Sys() any           { return nil }

// Ensure overlayFS implements fs.ReadFileFS for the ridl parser.
var _ fs.ReadFileFS = (*overlayFS)(nil)
