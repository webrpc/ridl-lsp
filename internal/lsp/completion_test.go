package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestCompletionSuggestsTopLevelKeywords(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := "ser"
	path := filepath.Join(dir, "completion-top-level.ridl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	list, err := srv.Completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position: protocol.Position{
				Line:      0,
				Character: 3,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertCompletionLabelsContain(t, list, "service")
	assertCompletionLabelsDoNotContain(t, list, "struct")
}

func TestCompletionSuggestsCoreAndSchemaTypesInTypePosition(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

struct LocalUser
  - id: uint64

import
  - types.ridl

service TestService
  - GetUser(id: uint64) => (user: )
`
	mainPath := filepath.Join(dir, "completion-main.ridl")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	typesContent := `webrpc = v1

struct ImportedUser
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0644); err != nil {
		t.Fatal(err)
	}

	mainURI := fileURI(mainPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(mainURI),
			Text:    mainContent,
			Version: 1,
		},
	})

	typesURI := fileURI(typesPath)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(typesURI),
			Text:    typesContent,
			Version: 1,
		},
	})

	list, err := srv.Completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
			Position:     positionAfter(t, mainContent, "(user: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertCompletionLabelsContain(t, list, "string")
	assertCompletionLabelsContain(t, list, "LocalUser")
	assertCompletionLabelsContain(t, list, "ImportedUser")
}

func TestCompletionFiltersTypePrefix(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - GetUser() => (user: Us)
`
	path := filepath.Join(dir, "completion-prefix.ridl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	list, err := srv.Completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfterOccurrence(t, content, "Us", 2),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertCompletionLabelsContain(t, list, "User")
	assertCompletionLabelsDoNotContain(t, list, "string")
}

func TestCompletionDoesNotTreatLaterArgumentNamesAsTypeContext(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - GetUser(id: uint64, na)
`
	path := filepath.Join(dir, "completion-arg-name.ridl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	list, err := srv.Completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position: func() protocol.Position {
				pos := positionAt(t, content, "na)")
				pos.Character += 2
				return pos
			}(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertCompletionLabelsDoNotContain(t, list, "string")
	assertCompletionLabelsDoNotContain(t, list, "User")
}

func TestCompletionRestrictsEnumBaseTypes(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

enum Kind: 
`
	path := filepath.Join(dir, "completion-enum-base.ridl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	uri := fileURI(path)
	_ = srv.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:     protocol.DocumentURI(uri),
			Text:    content,
			Version: 1,
		},
	})

	list, err := srv.Completion(ctx, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "enum Kind: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertCompletionLabelsContain(t, list, "uint32")
	assertCompletionLabelsContain(t, list, "string")
	assertCompletionLabelsDoNotContain(t, list, "User")
	assertCompletionLabelsDoNotContain(t, list, "map")
	assertCompletionLabelsDoNotContain(t, list, "timestamp")
}

func assertCompletionLabelsContain(t *testing.T, list *protocol.CompletionList, want string) {
	t.Helper()
	for _, item := range list.Items {
		if item.Label == want {
			return
		}
	}
	t.Fatalf("expected completion %q in %#v", want, completionLabels(list))
}

func assertCompletionLabelsDoNotContain(t *testing.T, list *protocol.CompletionList, unwanted string) {
	t.Helper()
	for _, item := range list.Items {
		if item.Label == unwanted {
			t.Fatalf("did not expect completion %q in %#v", unwanted, completionLabels(list))
		}
	}
}

func completionLabels(list *protocol.CompletionList) []string {
	if list == nil {
		return nil
	}
	labels := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		labels = append(labels, item.Label)
	}
	return labels
}

func positionAfter(t *testing.T, content, needle string) protocol.Position {
	t.Helper()

	pos := positionAt(t, content, needle)
	return protocol.Position{
		Line:      pos.Line,
		Character: pos.Character + uint32(len([]rune(needle))),
	}
}

func positionAfterOccurrence(t *testing.T, content, needle string, occurrence int) protocol.Position {
	t.Helper()

	pos := positionAtOccurrence(t, content, needle, occurrence)
	return protocol.Position{
		Line:      pos.Line,
		Character: pos.Character + uint32(len([]rune(needle))),
	}
}
