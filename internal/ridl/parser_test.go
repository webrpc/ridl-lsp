package ridl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseImportedServiceSchemaDoesNotRequireVersion(t *testing.T) {
	dir := t.TempDir()

	mainPath := filepath.Join(dir, "main.ridl")
	mainContent := `webrpc = v1

name = Main
version = v0.1.0

import
  - shared.ridl

service MainService
  - Ping() => (ok: bool)
`

	sharedPath := filepath.Join(dir, "shared.ridl")
	sharedContent := `webrpc = v1

service SharedService
  - Hello() => (ok: bool)
`

	writeTestFile(t, mainPath, mainContent)
	writeTestFile(t, sharedPath, sharedContent)

	result, err := NewParser().Parse(context.Background(), dir, mainPath, nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected no parse errors, got %v", result.Errors)
	}
	if result.Schema == nil {
		t.Fatal("expected schema result")
	}
	if result.Schema.GetServiceByName("SharedService") == nil {
		t.Fatal("expected imported service to be present in merged schema")
	}
}

func TestParseTopLevelServiceSchemaStillRequiresVersion(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "service.ridl")
	content := `webrpc = v1

name = Main

service MainService
  - Ping() => (ok: bool)
`

	writeTestFile(t, path, content)

	result, err := NewParser().Parse(context.Background(), dir, path, nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected parse errors for top-level service schema without version")
	}
	if !strings.Contains(result.Errors[0].Error(), "version is required when services are defined") {
		t.Fatalf("expected missing version error, got %v", result.Errors[0])
	}
}

func TestParseHonorsCanceledContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.ridl")
	writeTestFile(t, path, "webrpc = v1\n\nname = x\nversion = v1.0.0\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewParser().Parse(ctx, dir, path, nil); err == nil {
		t.Fatal("expected Parse to return the context error when the context is already cancelled")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
