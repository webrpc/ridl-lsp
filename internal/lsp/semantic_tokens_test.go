package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

func TestSemanticTokensClassifyRIDLDeclarationsAndReferences(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

enum Kind: uint32
  - ADMIN = 1

struct User
  - id: uint64

error 1 UserNotFound "user not found" HTTP 404

service TestService
  - GetUser(id: uint64) => (user: []User) errors UserNotFound
`
	path := filepath.Join(dir, "semantic-tokens.ridl")
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

	tokens, err := srv.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded := decodeSemanticTokens(t, content, tokens)
	assertSemanticTokenSorted(t, decoded)
	assertSemanticToken(t, decoded, "enum", protocol.SemanticTokenKeyword, nil)
	assertSemanticToken(t, decoded, "Kind", protocol.SemanticTokenEnum, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertNoSemanticToken(t, decoded, "ADMIN", protocol.SemanticTokenEnumMember)
	assertSemanticToken(t, decoded, "struct", protocol.SemanticTokenKeyword, nil)
	assertSemanticToken(t, decoded, "User", protocol.SemanticTokenStruct, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertNoSemanticToken(t, decoded, "id", protocol.SemanticTokenProperty)
	assertSemanticToken(t, decoded, "uint64", protocol.SemanticTokenType, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDefaultLibrary})
	assertSemanticToken(t, decoded, "error", protocol.SemanticTokenKeyword, nil)
	assertSemanticToken(t, decoded, "UserNotFound", protocol.SemanticTokenClass, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertSemanticToken(t, decoded, "service", protocol.SemanticTokenKeyword, nil)
	assertSemanticToken(t, decoded, "TestService", protocol.SemanticTokenInterface, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertSemanticToken(t, decoded, "GetUser", protocol.SemanticTokenMethod, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertSemanticToken(t, decoded, "id", protocol.SemanticTokenParameter, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertSemanticToken(t, decoded, "user", protocol.SemanticTokenParameter, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertSemanticTokenCount(t, decoded, "User", protocol.SemanticTokenStruct, 2)
	assertSemanticTokenCount(t, decoded, "UserNotFound", protocol.SemanticTokenClass, 2)
}

func TestSemanticTokensClassifyImportKeywordAndImportedTypes(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	mainContent := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: ImportedUser)
`
	mainPath := filepath.Join(dir, "semantic-imports-main.ridl")
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

	tokens, err := srv.SemanticTokensFull(ctx, &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(mainURI)},
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded := decodeSemanticTokens(t, mainContent, tokens)
	assertSemanticToken(t, decoded, "import", protocol.SemanticTokenKeyword, nil)
	assertSemanticToken(t, decoded, "ImportedUser", protocol.SemanticTokenStruct, nil)
}

func TestSemanticTokensRangeFiltersToRequestedLines(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - GetUser(id: uint64) => (user: User)
`
	path := filepath.Join(dir, "semantic-tokens-range.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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

	tokens, err := srv.SemanticTokensRange(ctx, &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range: protocol.Range{
			Start: protocol.Position{Line: 7, Character: 0},
			End:   protocol.Position{Line: 8, Character: 100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded := decodeSemanticTokens(t, content, tokens)
	assertSemanticTokenSorted(t, decoded)
	assertSemanticToken(t, decoded, "service", protocol.SemanticTokenKeyword, nil)
	assertSemanticToken(t, decoded, "TestService", protocol.SemanticTokenInterface, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertSemanticToken(t, decoded, "GetUser", protocol.SemanticTokenMethod, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertSemanticToken(t, decoded, "user", protocol.SemanticTokenParameter, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
	assertNoSemanticToken(t, decoded, "struct", protocol.SemanticTokenKeyword)
	assertNoSemanticToken(t, decoded, "User", protocol.SemanticTokenStruct)
}

func TestSemanticTokensFullDeltaFallsBackToFullTokens(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64
`
	path := filepath.Join(dir, "semantic-tokens-delta.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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

	result, err := srv.SemanticTokensFullDelta(ctx, &protocol.SemanticTokensDeltaParams{
		TextDocument:     protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		PreviousResultID: "previous",
	})
	if err != nil {
		t.Fatal(err)
	}

	tokens, ok := result.(*protocol.SemanticTokens)
	if !ok {
		t.Fatalf("expected full semantic tokens fallback, got %#v", result)
	}

	decoded := decodeSemanticTokens(t, content, tokens)
	assertSemanticToken(t, decoded, "struct", protocol.SemanticTokenKeyword, nil)
	assertSemanticToken(t, decoded, "User", protocol.SemanticTokenStruct, []protocol.SemanticTokenModifiers{protocol.SemanticTokenModifierDeclaration})
}

type decodedSemanticToken struct {
	text      string
	rng       protocol.Range
	tokenType protocol.SemanticTokenTypes
	modifiers []protocol.SemanticTokenModifiers
}

func decodeSemanticTokens(t *testing.T, content string, tokens *protocol.SemanticTokens) []decodedSemanticToken {
	t.Helper()
	if tokens == nil {
		t.Fatal("expected semantic tokens response")
	}

	out := make([]decodedSemanticToken, 0, len(tokens.Data)/5)
	var line uint32
	var character uint32

	for i := 0; i+4 < len(tokens.Data); i += 5 {
		deltaLine := tokens.Data[i]
		deltaStart := tokens.Data[i+1]
		length := tokens.Data[i+2]
		typeIndex := tokens.Data[i+3]
		modifierBits := tokens.Data[i+4]

		line += deltaLine
		if deltaLine == 0 {
			character += deltaStart
		} else {
			character = deltaStart
		}

		rng := protocol.Range{
			Start: protocol.Position{Line: line, Character: character},
			End:   protocol.Position{Line: line, Character: character + length},
		}

		out = append(out, decodedSemanticToken{
			text:      textAtRange(t, content, rng),
			rng:       rng,
			tokenType: semanticTokenLegendTypes[typeIndex],
			modifiers: decodeSemanticTokenModifiers(modifierBits),
		})
	}

	return out
}

func decodeSemanticTokenModifiers(bits uint32) []protocol.SemanticTokenModifiers {
	mods := make([]protocol.SemanticTokenModifiers, 0, len(semanticTokenLegendModifiers))
	for i, modifier := range semanticTokenLegendModifiers {
		if bits&(1<<i) != 0 {
			mods = append(mods, modifier)
		}
	}
	return mods
}

func textAtRange(t *testing.T, content string, rng protocol.Range) string {
	t.Helper()
	lines := strings.Split(content, "\n")
	if int(rng.Start.Line) >= len(lines) {
		t.Fatalf("line %d out of range", rng.Start.Line)
	}
	line := []rune(lines[rng.Start.Line])
	if int(rng.End.Character) > len(line) {
		t.Fatalf("range %+v out of bounds for line %q", rng, lines[rng.Start.Line])
	}
	return string(line[rng.Start.Character:rng.End.Character])
}

func assertSemanticTokenSorted(t *testing.T, tokens []decodedSemanticToken) {
	t.Helper()
	for i := 1; i < len(tokens); i++ {
		prev := tokens[i-1].rng.Start
		curr := tokens[i].rng.Start
		if curr.Line < prev.Line || (curr.Line == prev.Line && curr.Character < prev.Character) {
			t.Fatalf("tokens out of order: %#v before %#v", tokens[i-1], tokens[i])
		}
	}
}

func assertSemanticToken(t *testing.T, tokens []decodedSemanticToken, text string, tokenType protocol.SemanticTokenTypes, modifiers []protocol.SemanticTokenModifiers) {
	t.Helper()
	for _, token := range tokens {
		if token.text == text && token.tokenType == tokenType && sameSemanticModifiers(token.modifiers, modifiers) {
			return
		}
	}
	t.Fatalf("missing semantic token %q type=%s modifiers=%v in %#v", text, tokenType, modifiers, tokens)
}

func assertSemanticTokenCount(t *testing.T, tokens []decodedSemanticToken, text string, tokenType protocol.SemanticTokenTypes, want int) {
	t.Helper()
	count := 0
	for _, token := range tokens {
		if token.text == text && token.tokenType == tokenType {
			count++
		}
	}
	if count != want {
		t.Fatalf("expected %d semantic tokens for %q type=%s, got %d in %#v", want, text, tokenType, count, tokens)
	}
}

func assertNoSemanticToken(t *testing.T, tokens []decodedSemanticToken, text string, tokenType protocol.SemanticTokenTypes) {
	t.Helper()
	for _, token := range tokens {
		if token.text == text && token.tokenType == tokenType {
			t.Fatalf("unexpected semantic token %q type=%s in %#v", text, tokenType, tokens)
		}
	}
}

func sameSemanticModifiers(got, want []protocol.SemanticTokenModifiers) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
