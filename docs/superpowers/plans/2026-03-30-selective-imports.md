# Selective Imports & Auto-Import Improvements — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve auto-imports to prefer original defining files over re-exporters, and add diagnostics/quick-fixes for narrowing full imports to selective imports and fixing transitive re-imports.

**Architecture:** Two features built on shared infrastructure. Feature 1 modifies `uniqueImportCandidatePath` to rank defining files above re-exporters. Feature 2 adds a new `importDiagnostics` pass in `parseDocument` that emits Warning-level diagnostics, with corresponding code actions in `CodeAction`. Both features share a `locallyDefinedNames` helper that extracts names from a file's Root AST without following imports.

**Tech Stack:** Go, go.lsp.dev/protocol (LSP types), existing ridl-lsp test infrastructure (setupServer/mockClient)

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/lsp/code_action.go` | Modify | Improve `uniqueImportCandidatePath`; add narrowing + re-source code actions |
| `internal/lsp/diagnostics.go` | Modify | Add `importDiagnostics` for narrowing/unused/transitive warnings |
| `internal/lsp/code_action_test.go` | Modify | Tests for improved auto-import, narrowing quick fix, re-source quick fix |
| `internal/lsp/diagnostics_test.go` | Modify | Tests for warning diagnostics |

---

## Task 1: Prefer defining files in auto-import candidates

**Files:**
- Modify: `internal/lsp/code_action.go:380-419` (`uniqueImportCandidatePath`)
- Test: `internal/lsp/code_action_test.go`

### Concept

`uniqueImportCandidatePath` currently gives up when >1 workspace file matches a symbol. The fix: track whether each match is a "local definition" (the name appears in the file's own `type`/`struct`/`enum`/`error` declarations) vs a "re-export" (the name only appears after import resolution). Prefer local definers. Fall back to re-exporters only if no definer is found.

The existing `findTypeDefinitionToken(result.Root, name)` already checks only the Root AST (local declarations). When parsing via `parsePathForNavigation`, the Root is the file's own AST. So a re-exporter's Root won't contain the type — it only appears in its Schema. We leverage this: a match from `findTypeDefinitionToken` on Root is a local definition.

The current code already uses `findTypeDefinitionToken` on Root, so re-exporters are already excluded in most cases. The change handles the edge case where `parsePathForNavigation` returns a cached result whose Schema has been populated — we ensure we're always checking Root, and we relax the "exactly 1 match" constraint when we can distinguish definers from re-exporters.

- [ ] **Step 1: Write failing test — auto-import prefers definer over re-exporter**

Add to `internal/lsp/code_action_test.go`:

```go
func TestCodeActionPrefersDefinerOverReExporter(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	// organization.ridl defines OrgID
	orgContent := `webrpc = v1

struct OrgID
  - value: string
`
	orgPath := filepath.Join(dir, "organization.ridl")
	if err := os.WriteFile(orgPath, []byte(orgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// user.ridl imports organization.ridl (re-exports OrgID)
	userContent := `webrpc = v1

import
  - organization.ridl

struct User
  - id: uint64
  - orgID: OrgID
`
	userPath := filepath.Join(dir, "user.ridl")
	if err := os.WriteFile(userPath, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// project.ridl uses OrgID without importing anything
	content := `webrpc = v1

name = testapp
version = v0.1.0

struct Project
  - id: uint64
  - orgID: OrgID
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - organization.ridl

struct Project
  - id: uint64
  - orgID: OrgID
`

	path := filepath.Join(dir, "project.ridl")
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

	diagnostics := client.getDiagnostics(uri)
	if len(diagnostics) == 0 {
		t.Fatal("expected diagnostics for unresolved type")
	}

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context: protocol.CodeActionContext{
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
			Diagnostics: diagnostics,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should suggest organization.ridl (definer), NOT user.ridl (re-exporter)
	action := findCodeActionByTitle(actions, `Import "organization.ridl" for "OrgID"`)
	if action == nil {
		t.Fatalf("missing auto-import for OrgID from organization.ridl in %#v", actions)
	}

	if wrongAction := findCodeActionByTitle(actions, `Import "user.ridl" for "OrgID"`); wrongAction != nil {
		t.Fatalf("should not suggest re-exporter user.ridl")
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	got := applyTextEdit(t, content, edits[0])
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestCodeActionPrefersDefinerOverReExporter -v`

Expected: FAIL — the current code sees multiple workspace files exposing OrgID and returns no candidate (or the test may already pass if Root-only checking works — either way, proceed to make the logic explicit).

- [ ] **Step 3: Implement improved `uniqueImportCandidatePath`**

Replace the `uniqueImportCandidatePath` method in `internal/lsp/code_action.go`:

```go
func (s *Server) uniqueImportCandidatePath(docPath string, kind referenceKind, name string) (string, bool) {
	var definers, reExporters []string
	seen := map[string]struct{}{}

	for _, path := range s.referenceCandidatePaths() {
		if path == "" || path == docPath {
			continue
		}

		result := s.parsePathForNavigation(path)
		if result == nil || result.Root == nil {
			continue
		}

		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		// Check if name is locally defined in this file's AST (not via imports).
		var token *ridl.TokenNode
		switch kind {
		case referenceKindType:
			token = findTypeDefinitionToken(result.Root, name)
		case referenceKindError:
			token = findErrorDefinitionToken(result.Root, name)
		}

		if token != nil {
			definers = append(definers, path)
			continue
		}

		// Check if the name is available through this file's resolved schema
		// (i.e., it re-exports the name via imports).
		if result.Schema != nil && schemaHasName(result.Schema, kind, name) {
			reExporters = append(reExporters, path)
		}
	}

	// Prefer the unique definer.
	if len(definers) == 1 {
		return definers[0], true
	}
	if len(definers) > 1 {
		return "", false
	}

	// Fall back to re-exporters only if no definer found.
	if len(reExporters) == 1 {
		return reExporters[0], true
	}
	return "", false
}
```

Add the `schemaHasName` helper right below:

```go
func schemaHasName(s *schema.WebRPCSchema, kind referenceKind, name string) bool {
	switch kind {
	case referenceKindType:
		return s.GetTypeByName(name) != nil
	case referenceKindError:
		for _, e := range s.Errors {
			if strings.EqualFold(e.Name, name) {
				return true
			}
		}
	}
	return false
}
```

Add the `schema` import to the file's import block:

```go
"github.com/webrpc/webrpc/schema"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestCodeActionPrefersDefinerOverReExporter -v`

Expected: PASS

- [ ] **Step 5: Run all existing tests to verify no regressions**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -v`

Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp
git add internal/lsp/code_action.go internal/lsp/code_action_test.go
git commit -m "feat(lsp): prefer defining file over re-exporters in auto-import"
```

---

## Task 2: Collect locally defined and referenced names (shared infrastructure)

**Files:**
- Modify: `internal/lsp/code_action.go`
- Test: `internal/lsp/code_action_test.go`

### Concept

Feature 2 needs two capabilities:
1. **`locallyDefinedNames(root)`** — names declared in a file's own AST (enums, structs, errors)
2. **`referencedNames(doc)`** — all type/error names referenced in the current file's AST

These are reusable building blocks for narrowing diagnostics, unused import detection, and transitive re-import checks.

- [ ] **Step 1: Write tests for `locallyDefinedNames`**

Add to `internal/lsp/code_action_test.go`:

```go
func TestLocallyDefinedNames(t *testing.T) {
	srv, _, dir := setupServer(t)

	content := `webrpc = v1

enum Status: uint8
  - Active
  - Inactive

struct User
  - id: uint64
  - name: string

error 100 NotFound "not found" HTTP 404
`
	path := filepath.Join(dir, "defs.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := srv.parsePathForNavigation(path)
	if result == nil || result.Root == nil {
		t.Fatal("expected valid parse result")
	}

	names := locallyDefinedNames(result.Root)
	expected := map[string]struct{}{
		"Status":   {},
		"User":     {},
		"NotFound": {},
	}

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}
	for name := range expected {
		if _, ok := names[name]; !ok {
			t.Errorf("missing expected name %q in %v", name, names)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestLocallyDefinedNames -v`

Expected: FAIL — `locallyDefinedNames` not defined

- [ ] **Step 3: Implement `locallyDefinedNames`**

Add to `internal/lsp/code_action.go`:

```go
func locallyDefinedNames(root *ridl.RootNode) map[string]struct{} {
	names := map[string]struct{}{}
	if root == nil {
		return names
	}
	for _, enumNode := range root.Enums() {
		if enumNode != nil && enumNode.Name() != nil && enumNode.Name().String() != "" {
			names[enumNode.Name().String()] = struct{}{}
		}
	}
	for _, structNode := range root.Structs() {
		if structNode != nil && structNode.Name() != nil && structNode.Name().String() != "" {
			names[structNode.Name().String()] = struct{}{}
		}
	}
	for _, errorNode := range root.Errors() {
		if errorNode != nil && errorNode.Name() != nil && errorNode.Name().String() != "" {
			names[errorNode.Name().String()] = struct{}{}
		}
	}
	return names
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestLocallyDefinedNames -v`

Expected: PASS

- [ ] **Step 5: Write tests for `referencedNames`**

Add to `internal/lsp/code_action_test.go`:

```go
func TestReferencedNames(t *testing.T) {
	srv, _, dir := setupServer(t)

	typesContent := `webrpc = v1

struct User
  - id: uint64

struct Account
  - id: uint64

struct Org
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
  - GetAccount() => (account: Account)
`
	path := filepath.Join(dir, "main.ridl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := srv.parsePathForNavigation(path)
	if result == nil || result.Root == nil {
		t.Fatal("expected valid parse result")
	}

	doc := newSemanticDocument(path, content, result)
	names := doc.referencedNames()

	if _, ok := names["User"]; !ok {
		t.Error("expected User in referenced names")
	}
	if _, ok := names["Account"]; !ok {
		t.Error("expected Account in referenced names")
	}
	// Org is imported but not referenced
	if _, ok := names["Org"]; ok {
		t.Error("Org should not be in referenced names (not used)")
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestReferencedNames -v`

Expected: FAIL — `referencedNames` not defined

- [ ] **Step 7: Implement `referencedNames`**

Add to `internal/lsp/code_action.go` (on `semanticDocument`):

```go
func (d *semanticDocument) referencedNames() map[string]struct{} {
	names := map[string]struct{}{}
	if d == nil || !d.valid() {
		return names
	}

	addTypeRefs := func(token *ridl.TokenNode) {
		if token == nil {
			return
		}
		for _, name := range unresolvedTypeNames(token.String()) {
			if !isBuiltInRIDLType(name) {
				names[name] = struct{}{}
			}
		}
	}

	addErrorRef := func(token *ridl.TokenNode) {
		if token != nil && token.String() != "" {
			names[token.String()] = struct{}{}
		}
	}

	for _, enumNode := range d.result.Root.Enums() {
		addTypeRefs(enumNode.TypeName())
		for _, value := range enumNode.Values() {
			addTypeRefs(value.Right())
		}
	}

	for _, structNode := range d.result.Root.Structs() {
		for _, field := range structNode.Fields() {
			addTypeRefs(field.Right())
		}
	}

	for _, serviceNode := range d.result.Root.Services() {
		for _, methodNode := range serviceNode.Methods() {
			for _, input := range methodNode.Inputs() {
				addTypeRefs(argumentTypeToken(input))
			}
			for _, output := range methodNode.Outputs() {
				addTypeRefs(argumentTypeToken(output))
			}
			for _, errorToken := range methodNode.Errors() {
				addErrorRef(errorToken)
			}
		}
	}

	// Remove locally defined names — we only want external references.
	for name := range locallyDefinedNames(d.result.Root) {
		delete(names, name)
	}

	return names
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run "TestLocallyDefinedNames|TestReferencedNames" -v`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp
git add internal/lsp/code_action.go internal/lsp/code_action_test.go
git commit -m "feat(lsp): add locallyDefinedNames and referencedNames helpers"
```

---

## Task 3: Add import narrowing diagnostic and quick fix

**Files:**
- Modify: `internal/lsp/diagnostics.go`
- Modify: `internal/lsp/code_action.go`
- Test: `internal/lsp/diagnostics_test.go`
- Test: `internal/lsp/code_action_test.go`

### Concept

When a file does `import "types.ridl"` (no member list) but only uses a subset of the types it exports, emit a Warning diagnostic. Attach a code action that narrows the import to a selective form with only the used names.

- [ ] **Step 1: Write failing test — narrowing diagnostic emitted**

Add to `internal/lsp/diagnostics_test.go`:

```go
func TestNarrowImportDiagnostic(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64

struct Account
  - id: uint64

struct Org
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "narrow-import.ridl")
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

	diags := client.getDiagnostics(uri)
	var narrowDiag *protocol.Diagnostic
	for i := range diags {
		if diags[i].Severity == protocol.DiagnosticSeverityWarning &&
			strings.Contains(diags[i].Message, "can be narrowed") {
			narrowDiag = &diags[i]
			break
		}
	}

	if narrowDiag == nil {
		t.Fatalf("expected narrowing warning diagnostic, got: %#v", diags)
	}

	if !strings.Contains(narrowDiag.Message, "User") {
		t.Errorf("expected diagnostic to mention User, got: %s", narrowDiag.Message)
	}
}
```

Need to add `"strings"` to the import block of `diagnostics_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestNarrowImportDiagnostic -v`

Expected: FAIL — no narrowing diagnostic emitted

- [ ] **Step 3: Write test — no warning when all types are used**

Add to `internal/lsp/diagnostics_test.go`:

```go
func TestNoNarrowDiagnosticWhenAllTypesUsed(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "all-used.ridl")
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

	diags := client.getDiagnostics(uri)
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning {
			t.Fatalf("unexpected warning diagnostic: %s", d.Message)
		}
	}
}
```

- [ ] **Step 4: Write test — unused import diagnostic (zero refs)**

Add to `internal/lsp/diagnostics_test.go`:

```go
func TestUnusedImportDiagnostic(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl
`
	path := filepath.Join(dir, "unused-import.ridl")
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

	diags := client.getDiagnostics(uri)
	var unusedDiag *protocol.Diagnostic
	for i := range diags {
		if diags[i].Severity == protocol.DiagnosticSeverityWarning &&
			strings.Contains(diags[i].Message, "unused") {
			unusedDiag = &diags[i]
			break
		}
	}

	if unusedDiag == nil {
		t.Fatalf("expected unused import warning, got: %#v", diags)
	}
}
```

- [ ] **Step 5: Implement `importDiagnostics` in `diagnostics.go`**

Add the following to `internal/lsp/diagnostics.go`:

```go
func severityWarning() protocol.DiagnosticSeverity {
	return protocol.DiagnosticSeverityWarning
}

func (s *Server) importDiagnostics(doc *documents.Document) []protocol.Diagnostic {
	if doc == nil || doc.Result == nil || doc.Result.Root == nil || doc.Result.Schema == nil {
		return nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, doc.Result)
	if !semanticDoc.valid() {
		return nil
	}

	referencedNames := semanticDoc.referencedNames()
	var diagnostics []protocol.Diagnostic

	for _, importNode := range doc.Result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil || importNode.Path().String() == "" {
			continue
		}

		// Only check full imports (no member list).
		if len(importNode.Members()) > 0 {
			continue
		}

		importPath := workspace.ResolveImportPath(doc.Path, importNode.Path().String())
		importResult := s.parsePathForNavigation(importPath)
		if importResult == nil || importResult.Root == nil || importResult.Schema == nil {
			continue
		}

		// Collect all names this import brings into scope.
		exportedNames := exportedSchemaNames(importResult.Schema)
		if len(exportedNames) == 0 {
			continue
		}

		// Find which exported names are actually used.
		var usedNames []string
		for name := range exportedNames {
			if _, ok := referencedNames[name]; ok {
				usedNames = append(usedNames, name)
			}
		}

		sort.Strings(usedNames)

		importLine := ridl.TokenLine(importNode.Path()) - 1
		diagRange := lineRange(importLine + 1)

		if len(usedNames) == 0 {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range:    diagRange,
				Severity: severityWarning(),
				Message:  `Import "` + importNode.Path().String() + `" is unused`,
				Source:   "ridl",
			})
			continue
		}

		if len(usedNames) < len(exportedNames) {
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range:    diagRange,
				Severity: severityWarning(),
				Message:  `Import "` + importNode.Path().String() + `" can be narrowed to: ` + strings.Join(usedNames, ", "),
				Source:   "ridl",
			})
		}
	}

	return diagnostics
}

func exportedSchemaNames(s *schema.WebRPCSchema) map[string]struct{} {
	names := map[string]struct{}{}
	if s == nil {
		return names
	}
	for _, typ := range s.Types {
		if typ != nil && typ.Name != "" {
			names[typ.Name] = struct{}{}
		}
	}
	for _, e := range s.Errors {
		if e != nil && e.Name != "" {
			names[e.Name] = struct{}{}
		}
	}
	return names
}
```

Add the required imports to `diagnostics.go`:

```go
"sort"
"github.com/webrpc/ridl-lsp/internal/workspace"
"github.com/webrpc/webrpc/schema"
ridl "github.com/webrpc/ridl-lsp/internal/ridl"
```

- [ ] **Step 6: Wire `importDiagnostics` into `parseDocument`**

In `internal/lsp/diagnostics.go`, modify `parseDocument` to append import warnings after a successful parse. Change the block starting at line 51:

Replace:
```go
	if len(result.Errors) == 0 {
		doc.Result = result
		return []protocol.Diagnostic{}
	}
```

With:
```go
	if len(result.Errors) == 0 {
		doc.Result = result
		return s.importDiagnostics(doc)
	}
```

- [ ] **Step 7: Run diagnostic tests**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run "TestNarrowImportDiagnostic|TestNoNarrowDiagnosticWhenAllTypesUsed|TestUnusedImportDiagnostic" -v`

Expected: All 3 PASS

- [ ] **Step 8: Run all tests to verify no regressions**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -v`

Expected: All PASS. Note: `TestValidDocumentNoDiagnostics` should still pass because a valid document with no imports produces no warnings.

- [ ] **Step 9: Commit**

```bash
cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp
git add internal/lsp/diagnostics.go internal/lsp/diagnostics_test.go internal/lsp/code_action.go
git commit -m "feat(lsp): add narrowing and unused import warning diagnostics"
```

---

## Task 4: Add narrowing quick fix code action

**Files:**
- Modify: `internal/lsp/code_action.go`
- Test: `internal/lsp/code_action_test.go`

### Concept

When the narrowing diagnostic is present, offer a code action that rewrites the full import to a selective import with only the used names. This uses the existing `unresolvedImportEdit` pattern for rewriting import lines.

- [ ] **Step 1: Write failing test — narrowing code action**

Add to `internal/lsp/code_action_test.go`:

```go
func TestCodeActionNarrowImport(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64

struct Account
  - id: uint64

struct Org
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl

service TestService
  - GetUser() => (user: User)
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl
    - User

service TestService
  - GetUser() => (user: User)
`

	path := filepath.Join(dir, "narrow-action.ridl")
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

	diagnostics := client.getDiagnostics(uri)

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context: protocol.CodeActionContext{
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
			Diagnostics: diagnostics,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	action := findCodeActionByTitle(actions, `Narrow import "types.ridl" to: User`)
	if action == nil {
		t.Fatalf("missing narrow-import quick fix in %#v", actions)
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	got := applyTextEdits(t, content, edits)
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestCodeActionNarrowImport -v`

Expected: FAIL — no narrow-import code action

- [ ] **Step 3: Implement `narrowImportCodeActions`**

Add to `internal/lsp/code_action.go`:

```go
func (s *Server) narrowImportCodeActions(doc *documents.Document, diagnostics []protocol.Diagnostic) []protocol.CodeAction {
	if doc == nil || doc.Result == nil || doc.Result.Root == nil {
		return nil
	}

	// Only act on narrowing/unused warning diagnostics.
	var warningDiags []protocol.Diagnostic
	for _, d := range diagnostics {
		if d.Source == "ridl" && d.Severity == protocol.DiagnosticSeverityWarning &&
			(strings.Contains(d.Message, "can be narrowed") || strings.Contains(d.Message, "is unused")) {
			warningDiags = append(warningDiags, d)
		}
	}
	if len(warningDiags) == 0 {
		return nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, doc.Result)
	if !semanticDoc.valid() {
		return nil
	}

	referencedNames := semanticDoc.referencedNames()
	lines := splitContentLines(doc.Content)
	var actions []protocol.CodeAction

	for _, importNode := range doc.Result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil || len(importNode.Members()) > 0 {
			continue
		}

		importPath := workspace.ResolveImportPath(doc.Path, importNode.Path().String())
		importResult := s.parsePathForNavigation(importPath)
		if importResult == nil || importResult.Schema == nil {
			continue
		}

		exportedNames := exportedSchemaNames(importResult.Schema)
		var usedNames []string
		for name := range exportedNames {
			if _, ok := referencedNames[name]; ok {
				usedNames = append(usedNames, name)
			}
		}
		sort.Strings(usedNames)

		importLine := ridl.TokenLine(importNode.Path()) - 1
		if importLine < 0 || importLine >= len(lines) {
			continue
		}

		if len(usedNames) == 0 {
			// Unused import — offer removal.
			edit, ok := unresolvedImportEdit(semanticDoc, importLine)
			if !ok {
				continue
			}
			actions = append(actions, protocol.CodeAction{
				Title:       `Remove unused import "` + importNode.Path().String() + `"`,
				Kind:        protocol.QuickFix,
				Diagnostics: warningDiags,
				Edit: &protocol.WorkspaceEdit{
					Changes: map[protocol.DocumentURI][]protocol.TextEdit{
						protocol.DocumentURI(doc.URI): {edit},
					},
				},
			})
			continue
		}

		if len(usedNames) >= len(exportedNames) {
			continue
		}

		// Build selective import edit: add member lines after the import path line.
		var memberLines string
		for _, name := range usedNames {
			memberLines += "    - " + name + "\n"
		}

		// Replace the import path line with path + member lines.
		edit := protocol.TextEdit{
			Range:   lineDeletionRange(semanticDoc, lines, importLine, importLine+1),
			NewText: lines[importLine][:len(lines[importLine])-1] + "\n" + memberLines,
		}

		// Handle lines that don't end with \n
		rawLine := lines[importLine]
		if len(rawLine) > 0 && rawLine[len(rawLine)-1] != '\n' {
			edit.NewText = rawLine + "\n" + memberLines
		}

		actions = append(actions, protocol.CodeAction{
			Title:       `Narrow import "` + importNode.Path().String() + `" to: ` + strings.Join(usedNames, ", "),
			Kind:        protocol.QuickFix,
			Diagnostics: warningDiags,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					protocol.DocumentURI(doc.URI): {edit},
				},
			},
		})
	}

	return actions
}
```

- [ ] **Step 4: Wire `narrowImportCodeActions` into `CodeAction`**

In `internal/lsp/code_action.go`, inside the `CodeAction` method, add the call after the existing quick fix actions (around line 31):

Replace:
```go
	if codeActionKindRequested(params.Context.Only, protocol.QuickFix) {
		if doc, ok := s.docs.Get(string(params.TextDocument.URI)); ok {
			actions = append(actions, s.unresolvedImportCodeActions(doc, params.Context.Diagnostics)...)
			actions = append(actions, s.missingImportCodeActions(doc, params.Context.Diagnostics)...)
		}
	}
```

With:
```go
	if codeActionKindRequested(params.Context.Only, protocol.QuickFix) {
		if doc, ok := s.docs.Get(string(params.TextDocument.URI)); ok {
			actions = append(actions, s.unresolvedImportCodeActions(doc, params.Context.Diagnostics)...)
			actions = append(actions, s.missingImportCodeActions(doc, params.Context.Diagnostics)...)
			actions = append(actions, s.narrowImportCodeActions(doc, params.Context.Diagnostics)...)
		}
	}
```

Add the `workspace` import if not already present:

```go
"github.com/webrpc/ridl-lsp/internal/workspace"
```

- [ ] **Step 5: Run narrowing code action test**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestCodeActionNarrowImport -v`

Expected: PASS

If the selective import format differs from expected (e.g. indentation), adjust the `memberLines` format or the `want` string in the test. The RIDL selective import syntax uses `- Name` indented under the import path. Check the parser's expectations for member indentation (4 spaces: `    - Name`).

- [ ] **Step 6: Run all tests**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -v`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp
git add internal/lsp/code_action.go internal/lsp/code_action_test.go
git commit -m "feat(lsp): add quick fix to narrow full imports to selective imports"
```

---

## Task 5: Add transitive re-import diagnostic and quick fix

**Files:**
- Modify: `internal/lsp/diagnostics.go`
- Modify: `internal/lsp/code_action.go`
- Test: `internal/lsp/diagnostics_test.go`
- Test: `internal/lsp/code_action_test.go`

### Concept

When a selective import pulls a type from a file that didn't define it (e.g., `import "user.ridl" - OrgID` when OrgID is defined in `organization.ridl`), emit a warning with a quick fix to rewrite the import to use the original source.

- [ ] **Step 1: Write failing test — transitive re-import diagnostic**

Add to `internal/lsp/diagnostics_test.go`:

```go
func TestTransitiveReImportDiagnostic(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	orgContent := `webrpc = v1

struct OrgID
  - value: string
`
	orgPath := filepath.Join(dir, "organization.ridl")
	if err := os.WriteFile(orgPath, []byte(orgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	userContent := `webrpc = v1

import
  - organization.ridl

struct User
  - id: uint64
  - orgID: OrgID
`
	userPath := filepath.Join(dir, "user.ridl")
	if err := os.WriteFile(userPath, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - user.ridl
    - OrgID

struct Project
  - orgID: OrgID
`
	path := filepath.Join(dir, "transitive.ridl")
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

	diags := client.getDiagnostics(uri)
	var reImportDiag *protocol.Diagnostic
	for i := range diags {
		if diags[i].Severity == protocol.DiagnosticSeverityWarning &&
			strings.Contains(diags[i].Message, "defined in") {
			reImportDiag = &diags[i]
			break
		}
	}

	if reImportDiag == nil {
		t.Fatalf("expected transitive re-import warning, got: %#v", diags)
	}

	if !strings.Contains(reImportDiag.Message, "organization.ridl") {
		t.Errorf("expected diagnostic to mention organization.ridl, got: %s", reImportDiag.Message)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestTransitiveReImportDiagnostic -v`

Expected: FAIL

- [ ] **Step 3: Write test — no warning when selective import is correct**

Add to `internal/lsp/diagnostics_test.go`:

```go
func TestNoTransitiveDiagnosticForCorrectSelectiveImport(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	typesContent := `webrpc = v1

struct User
  - id: uint64
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - types.ridl
    - User

service TestService
  - GetUser() => (user: User)
`
	path := filepath.Join(dir, "correct-selective.ridl")
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

	diags := client.getDiagnostics(uri)
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning {
			t.Fatalf("unexpected warning: %s", d.Message)
		}
	}
}
```

- [ ] **Step 4: Implement transitive re-import diagnostics**

Add to the `importDiagnostics` method in `internal/lsp/diagnostics.go`, after the full-import narrowing loop (before the final `return diagnostics`):

```go
	// Check selective imports for transitive re-imports.
	for _, importNode := range doc.Result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil || len(importNode.Members()) == 0 {
			continue
		}

		importPath := workspace.ResolveImportPath(doc.Path, importNode.Path().String())
		importResult := s.parsePathForNavigation(importPath)
		if importResult == nil || importResult.Root == nil {
			continue
		}

		localNames := locallyDefinedNames(importResult.Root)

		for _, member := range importNode.Members() {
			if member == nil || member.String() == "" {
				continue
			}
			name := member.String()

			// If the imported file locally defines this name, it's fine.
			if _, ok := localNames[name]; ok {
				continue
			}

			// Find the original source file.
			originalPath, ok := s.uniqueImportCandidatePath(doc.Path, referenceKindType, name)
			if !ok {
				// Try error kind.
				originalPath, ok = s.uniqueImportCandidatePath(doc.Path, referenceKindError, name)
			}
			if !ok {
				continue
			}

			relOriginal, ok := relativeImportPath(doc.Path, originalPath)
			if !ok {
				continue
			}

			memberLine := ridl.TokenLine(member) - 1
			diagnostics = append(diagnostics, protocol.Diagnostic{
				Range:    lineRange(memberLine + 1),
				Severity: severityWarning(),
				Message:  name + ` is defined in "` + relOriginal + `", not "` + importNode.Path().String() + `"`,
				Source:   "ridl",
			})
		}
	}
```

Add `relativeImportPath` and `locallyDefinedNames` imports — these are in `code_action.go` in the same package, so no import needed.

- [ ] **Step 5: Run transitive diagnostic tests**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run "TestTransitiveReImportDiagnostic|TestNoTransitiveDiagnosticForCorrectSelectiveImport" -v`

Expected: PASS

- [ ] **Step 6: Write failing test — transitive re-import quick fix**

Add to `internal/lsp/code_action_test.go`:

```go
func TestCodeActionFixTransitiveReImport(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	orgContent := `webrpc = v1

struct OrgID
  - value: string
`
	orgPath := filepath.Join(dir, "organization.ridl")
	if err := os.WriteFile(orgPath, []byte(orgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	userContent := `webrpc = v1

import
  - organization.ridl

struct User
  - id: uint64
  - orgID: OrgID
`
	userPath := filepath.Join(dir, "user.ridl")
	if err := os.WriteFile(userPath, []byte(userContent), 0o644); err != nil {
		t.Fatal(err)
	}

	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - user.ridl
    - OrgID
    - User

struct Project
  - orgID: OrgID
  - owner: User
`
	want := `webrpc = v1

name = testapp
version = v0.1.0

import
  - user.ridl
    - User
  - organization.ridl
    - OrgID

struct Project
  - orgID: OrgID
  - owner: User
`

	path := filepath.Join(dir, "fix-transitive.ridl")
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

	diagnostics := client.getDiagnostics(uri)

	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context: protocol.CodeActionContext{
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
			Diagnostics: diagnostics,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	action := findCodeActionByTitle(actions, `Import OrgID from "organization.ridl" instead of "user.ridl"`)
	if action == nil {
		t.Fatalf("missing re-source quick fix in %#v", actions)
	}

	edits := action.Edit.Changes[protocol.DocumentURI(uri)]
	got := applyTextEdits(t, content, edits)
	if got != want {
		t.Fatalf("unexpected result:\nwant:\n%s\ngot:\n%s", want, got)
	}
}
```

- [ ] **Step 7: Implement `transitiveReImportCodeActions`**

Add to `internal/lsp/code_action.go`:

```go
func (s *Server) transitiveReImportCodeActions(doc *documents.Document, diagnostics []protocol.Diagnostic) []protocol.CodeAction {
	if doc == nil || doc.Result == nil || doc.Result.Root == nil {
		return nil
	}

	// Only act on transitive re-import warnings.
	var warningDiags []protocol.Diagnostic
	for _, d := range diagnostics {
		if d.Source == "ridl" && d.Severity == protocol.DiagnosticSeverityWarning &&
			strings.Contains(d.Message, "is defined in") {
			warningDiags = append(warningDiags, d)
		}
	}
	if len(warningDiags) == 0 {
		return nil
	}

	semanticDoc := newSemanticDocument(doc.Path, doc.Content, doc.Result)
	if !semanticDoc.valid() {
		return nil
	}

	lines := splitContentLines(doc.Content)
	var actions []protocol.CodeAction

	for _, importNode := range doc.Result.Root.Imports() {
		if importNode == nil || importNode.Path() == nil || len(importNode.Members()) == 0 {
			continue
		}

		importPath := workspace.ResolveImportPath(doc.Path, importNode.Path().String())
		importResult := s.parsePathForNavigation(importPath)
		if importResult == nil || importResult.Root == nil {
			continue
		}

		localNames := locallyDefinedNames(importResult.Root)

		for _, member := range importNode.Members() {
			if member == nil || member.String() == "" {
				continue
			}
			name := member.String()
			if _, ok := localNames[name]; ok {
				continue
			}

			// Find original source.
			originalPath, ok := s.uniqueImportCandidatePath(doc.Path, referenceKindType, name)
			if !ok {
				originalPath, ok = s.uniqueImportCandidatePath(doc.Path, referenceKindError, name)
			}
			if !ok {
				continue
			}

			relOriginal, ok := relativeImportPath(doc.Path, originalPath)
			if !ok {
				continue
			}

			// Build edit: remove the member line from current import, add new selective import.
			memberLine := ridl.TokenLine(member) - 1
			if memberLine < 0 || memberLine >= len(lines) {
				continue
			}

			var edits []protocol.TextEdit

			// Remove the member line from the current import.
			edits = append(edits, protocol.TextEdit{
				Range:   lineDeletionRange(semanticDoc, lines, memberLine, memberLine+1),
				NewText: "",
			})

			// If this was the only member, we'd need to also remove the import header.
			// Count remaining members after removal.
			remainingMembers := 0
			for _, m := range importNode.Members() {
				if m != nil && m.String() != name {
					remainingMembers++
				}
			}

			if remainingMembers == 0 {
				// Remove the entire import block (header + member line).
				headerLine := ridl.TokenLine(importNode.Path()) - 1
				endLine := memberLine + 1
				if endLine < len(lines) && trimmedLine(lines[endLine]) == "" {
					endLine++
				}
				edits = []protocol.TextEdit{{
					Range:   lineDeletionRange(semanticDoc, lines, headerLine, endLine),
					NewText: "",
				}}
			}

			// Add new selective import for the original source.
			newImportEdit, ok := missingImportEdit(semanticDoc, relOriginal)
			if !ok {
				continue
			}

			// Modify the new import to be selective (add member line).
			newImportEdit.NewText = strings.TrimRight(newImportEdit.NewText, "\n") + "\n    - " + name + "\n"

			edits = append(edits, newImportEdit)

			actions = append(actions, protocol.CodeAction{
				Title:       `Import ` + name + ` from "` + relOriginal + `" instead of "` + importNode.Path().String() + `"`,
				Kind:        protocol.QuickFix,
				Diagnostics: warningDiags,
				Edit: &protocol.WorkspaceEdit{
					Changes: map[protocol.DocumentURI][]protocol.TextEdit{
						protocol.DocumentURI(doc.URI): edits,
					},
				},
			})
		}
	}

	return actions
}
```

- [ ] **Step 8: Wire `transitiveReImportCodeActions` into `CodeAction`**

In the `CodeAction` method's QuickFix block, add after the `narrowImportCodeActions` call:

```go
			actions = append(actions, s.transitiveReImportCodeActions(doc, params.Context.Diagnostics)...)
```

- [ ] **Step 9: Run transitive re-import tests**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run "TestCodeActionFixTransitiveReImport|TestTransitiveReImportDiagnostic" -v`

Expected: PASS (may need adjustments to the expected output format — iterate on the test's `want` string and the edit generation logic until they match)

- [ ] **Step 10: Run all tests**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -v`

Expected: All PASS

- [ ] **Step 11: Commit**

```bash
cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp
git add internal/lsp/diagnostics.go internal/lsp/code_action.go internal/lsp/code_action_test.go internal/lsp/diagnostics_test.go
git commit -m "feat(lsp): add transitive re-import warning diagnostic and quick fix"
```

---

## Task 6: Final integration test and cleanup

**Files:**
- Test: `internal/lsp/code_action_test.go`

### Concept

Add one integration test that exercises the full flow: a workspace with multiple files, imports at various levels, and verify all diagnostics and code actions work together.

- [ ] **Step 1: Write integration test**

Add to `internal/lsp/code_action_test.go`:

```go
func TestSelectiveImportIntegration(t *testing.T) {
	srv, client, dir := setupServer(t)
	ctx := context.Background()

	// common.ridl defines Page and EmptyResponse
	commonContent := `webrpc = v1

struct Page
  - number: uint32
  - size: uint32

struct EmptyResponse
`
	commonPath := filepath.Join(dir, "common.ridl")
	if err := os.WriteFile(commonPath, []byte(commonContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// types.ridl defines User and imports common.ridl
	typesContent := `webrpc = v1

import
  - common.ridl

struct User
  - id: uint64
  - name: string
`
	typesPath := filepath.Join(dir, "types.ridl")
	if err := os.WriteFile(typesPath, []byte(typesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// main.ridl imports common.ridl (full) but only uses Page
	content := `webrpc = v1

name = testapp
version = v0.1.0

import
  - common.ridl

service TestService
  - ListUsers(page: Page) => (users: []User)
`
	path := filepath.Join(dir, "main.ridl")
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

	diags := client.getDiagnostics(uri)

	// Should have narrowing warning for common.ridl (only Page used, not EmptyResponse)
	hasNarrowWarning := false
	for _, d := range diags {
		if d.Severity == protocol.DiagnosticSeverityWarning && strings.Contains(d.Message, "can be narrowed") {
			hasNarrowWarning = true
		}
	}
	if !hasNarrowWarning {
		t.Errorf("expected narrowing warning for common.ridl, got: %#v", diags)
	}

	// Should have auto-import suggestion for User from types.ridl
	actions, err := srv.CodeAction(ctx, &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.DocumentURI(uri)},
		Range:        fullDocumentRange(content),
		Context: protocol.CodeActionContext{
			Only:        []protocol.CodeActionKind{protocol.QuickFix},
			Diagnostics: diags,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if action := findCodeActionByTitle(actions, `Import "types.ridl" for "User"`); action == nil {
		t.Errorf("expected auto-import for User from types.ridl, got actions: %#v", actions)
	}
}
```

- [ ] **Step 2: Run integration test**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./internal/lsp/ -run TestSelectiveImportIntegration -v`

Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp && go test ./... -v`

Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/aguerrieri/Workspace/webrpc/ridl-lsp
git add internal/lsp/code_action_test.go
git commit -m "test(lsp): add selective imports integration test"
```

---

## Implementation Notes

### Import member syntax

RIDL selective import members are indented under the import path with `- Name`:

```ridl
import
  - types.ridl
    - User
    - Account
```

The parser expects 4-space indent for members under the import path item. The `missingImportEdit` function handles import path additions; the narrowing edit adds member lines with `    - Name` (4-space indent).

### Diagnostic severity

All new diagnostics use `DiagnosticSeverityWarning` — full imports work fine, these are style lints. They should NOT block editing or cause parse failures.

### Edge cases to be aware of

- A file that imports nothing: no diagnostics emitted (no imports to check)
- A selective import that's already correct: no warning
- A file that defines and uses its own types: `referencedNames` excludes locally defined names
- Circular imports: the parser already handles visited sets; diagnostics follow the same pattern
- Multiple imports from the same file: each import is checked independently
