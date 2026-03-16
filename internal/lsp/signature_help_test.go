package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestSignatureHelpReturnsMethodInputs(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

service TestService
  - GetUser(id: uint64, accountID: uint64) => (user: User, ok: bool)
`
	path := filepath.Join(dir, "signature-help-inputs.ridl")
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

	help, err := srv.SignatureHelp(ctx, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "(id: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSignatureHelp(t, help, "GetUser(id: uint64, accountID: uint64) => (user: User, ok: bool)", 0, "id: uint64", "accountID: uint64")
}

func TestSignatureHelpTracksSecondInputParameter(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

service TestService
  - GetUser(id: uint64, accountID: uint64) => (ok: bool)
`
	path := filepath.Join(dir, "signature-help-second-input.ridl")
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

	help, err := srv.SignatureHelp(ctx, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "accountID: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSignatureHelp(t, help, "GetUser(id: uint64, accountID: uint64) => (ok: bool)", 1, "id: uint64", "accountID: uint64")
}

func TestSignatureHelpTracksOutputParameter(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

struct User
  - id: uint64

struct Account
  - id: uint64

service TestService
  - GetUser(id: uint64) => (user: User, account: Account)
`
	path := filepath.Join(dir, "signature-help-outputs.ridl")
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

	help, err := srv.SignatureHelp(ctx, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "account: "),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSignatureHelp(t, help, "GetUser(id: uint64) => (user: User, account: Account)", 1, "user: User", "account: Account")
}

func TestSignatureHelpSkipsOutsideMethodTuple(t *testing.T) {
	srv, _, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1

name = testapp
version = v0.1.0

service TestService
  - GetUser(id: uint64) => (ok: bool)
`
	path := filepath.Join(dir, "signature-help-outside.ridl")
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

	help, err := srv.SignatureHelp(ctx, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
			Position:     positionAfter(t, content, "GetUser"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if help != nil {
		t.Fatalf("expected no signature help outside method tuple, got %#v", help)
	}
}

func assertSignatureHelp(t *testing.T, help *protocol.SignatureHelp, wantLabel string, wantActive uint32, wantParams ...string) {
	t.Helper()

	if help == nil {
		t.Fatal("expected signature help")
	}
	if len(help.Signatures) != 1 {
		t.Fatalf("expected 1 signature, got %#v", help.Signatures)
	}

	signature := help.Signatures[0]
	if signature.Label != wantLabel {
		t.Fatalf("unexpected signature label %q", signature.Label)
	}
	if help.ActiveSignature != 0 {
		t.Fatalf("unexpected active signature %d", help.ActiveSignature)
	}
	if help.ActiveParameter != wantActive {
		t.Fatalf("unexpected active parameter %d", help.ActiveParameter)
	}
	if signature.ActiveParameter != wantActive {
		t.Fatalf("unexpected signature active parameter %d", signature.ActiveParameter)
	}
	if len(signature.Parameters) != len(wantParams) {
		t.Fatalf("expected %d parameters, got %#v", len(wantParams), signature.Parameters)
	}

	for i, want := range wantParams {
		if signature.Parameters[i].Label != want {
			t.Fatalf("unexpected parameter %d label %q", i, signature.Parameters[i].Label)
		}
	}
}
