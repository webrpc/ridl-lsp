# RIDL LSP

This repository hosts a standalone Go language server for webrpc RIDL files.

The initial layout is intentionally small:

```text
.
├── cmd/ridl-lsp
│   └── main.go
├── internal/documents
│   └── store.go
├── internal/lsp
│   ├── server.go
│   └── stubs.go
├── internal/ridl
│   └── parser.go
├── internal/workspace
│   └── manager.go
└── go.mod
```

The package split follows the shape we want for a RIDL-specific server:

- `cmd/ridl-lsp`: binary entrypoint and transport wiring.
- `internal/lsp`: LSP method handlers and capability declarations.
- `internal/documents`: open-document state, versions, and in-memory overlays.
- `internal/workspace`: workspace root management and path helpers.
- `internal/ridl`: parser and semantic-analysis boundary for RIDL-specific logic.

This is meant to stay closer to a focused custom-language server such as `sqls`
than to the much heavier `gopls` architecture. If we need snapshots, indexing,
or background analysis later, we can grow into that deliberately.

Current assumption:

- module path: `github.com/webrpc/ridl-lsp`

If you want a different repo/module name, we should rename it early before we
start adding imports across packages.
