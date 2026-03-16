package lsp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"
)

func TestExecuteCommandFormatsDocumentViaApplyEdit(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc= v1
name= testapp
version =v0.1.0
`
	want := `webrpc = v1
name = testapp
version = v0.1.0
`

	path := filepath.Join(dir, "execute-command-format.ridl")
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

	result, err := srv.ExecuteCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   executeCommandFormatDocument,
		Arguments: []any{protocol.DocumentURI(uri)},
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, ok := result.(bool)
	if !ok || !applied {
		t.Fatalf("expected applied result, got %#v", result)
	}

	edit := client.lastAppliedEdit()
	if edit == nil {
		t.Fatal("expected executeCommand to apply a workspace edit")
	}
	if edit.Label != "Format RIDL document" {
		t.Fatalf("unexpected apply-edit label %q", edit.Label)
	}

	edits := edit.Edit.Changes[protocol.DocumentURI(uri)]
	if len(edits) != 1 {
		t.Fatalf("expected one applied text edit, got %#v", edits)
	}
	if edits[0].NewText != want {
		t.Fatalf("unexpected execute-command formatted output:\nwant:\n%s\ngot:\n%s", want, edits[0].NewText)
	}
}

func TestExecuteCommandSkipsAlreadyFormattedDocument(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	content := `webrpc = v1
name = testapp
version = v0.1.0
`
	path := filepath.Join(dir, "execute-command-noop.ridl")
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

	result, err := srv.ExecuteCommand(ctx, &protocol.ExecuteCommandParams{
		Command:   executeCommandFormatDocument,
		Arguments: []any{uri},
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, ok := result.(bool)
	if !ok || applied {
		t.Fatalf("expected false applied result, got %#v", result)
	}
	if client.lastAppliedEdit() != nil {
		t.Fatalf("expected no workspace edit for formatted document, got %#v", client.lastAppliedEdit())
	}
}

func TestExecuteCommandRejectsUnknownCommand(t *testing.T) {
	srv, _, _ := setupServer(t)
	ctx := context.Background()

	_, err := srv.ExecuteCommand(ctx, &protocol.ExecuteCommandParams{Command: "ridl.unknown"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
}
