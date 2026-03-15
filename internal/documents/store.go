package documents

import (
	"sync"

	"github.com/webrpc/webrpc/schema/ridl"
)

type Document struct {
	URI     string
	Path    string
	Content string
	Version int32
	Result  *ridl.ParseResult
}

type Store struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

func NewStore() *Store {
	return &Store{
		docs: map[string]*Document{},
	}
}

func (s *Store) Get(uri string) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, ok := s.docs[uri]
	return doc, ok
}

func (s *Store) Set(doc *Document) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.docs[doc.URI] = doc
}

func (s *Store) Delete(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.docs, uri)
}

func (s *Store) All() []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Document, 0, len(s.docs))
	for _, doc := range s.docs {
		out = append(out, doc)
	}
	return out
}

func (s *Store) FindByPath(path string) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, doc := range s.docs {
		if doc.Path == path {
			return doc, true
		}
	}
	return nil, false
}
