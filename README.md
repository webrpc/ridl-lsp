# ridl-lsp

A standalone [Language Server](https://microsoft.github.io/language-server-protocol/)
for [webrpc](https://github.com/webrpc/webrpc) **RIDL** schema files (`.ridl`).
It gives any LSP-capable editor rich, schema-aware editing for RIDL: navigation,
completion, diagnostics, refactoring, and more — backed by the real webrpc parser
so what the editor shows matches what `webrpc-gen` will accept.

It speaks LSP over **stdio** and is intentionally lean — closer to a focused
language server like `sqls` than to the much heavier `gopls`.

## Features

- **Navigation** — go-to-definition, declaration, type-definition, find
  references, document highlight (resolves across `import`s).
- **Completion & signatures** — context-aware completion (keywords, types,
  fields, imports) and signature help for method arguments.
- **Diagnostics** — parse errors with precise line/column positions, plus
  `unused import` and `import can be narrowed` / wrong-source warnings.
- **Refactoring** — rename (with linked editing) and quick-fix code actions
  such as adding a missing import.
- **Formatting** — full-document and range formatting, on-type formatting, and
  a "format document" command.
- **Structure** — document symbols, workspace symbols, folding ranges, and
  selection ranges.
- **Insight** — hover, code lens, document links, color decorators, monikers,
  and semantic tokens (full, delta, and range).

## Install

**Homebrew** (macOS/Linux):

```sh
brew tap webrpc/tap
brew install ridl-lsp
```

**Go** (builds from source):

```sh
go install github.com/webrpc/ridl-lsp/cmd/ridl-lsp@latest
```

**Prebuilt binaries** — macOS / Linux / Windows (amd64 & arm64) are attached to
each [GitHub release](https://github.com/webrpc/ridl-lsp/releases).

**Docker** — `ghcr.io/webrpc/ridl-lsp:latest` (primarily for CI/containers; the
server runs on stdio).

Verify the install:

```sh
ridl-lsp --version
```

## Editor setup

`ridl-lsp` communicates over stdio — point your editor's LSP client at the
`ridl-lsp` binary for the `ridl` language / `.ridl` files.

**VS Code** — use the companion [ridl-vscode](https://github.com/webrpc/ridl-vscode)
extension, which bundles syntax highlighting and launches `ridl-lsp`.

**Neovim** (`nvim-lspconfig`, manual registration):

```lua
local configs = require("lspconfig.configs")
local lspconfig = require("lspconfig")

if not configs.ridl_lsp then
  configs.ridl_lsp = {
    default_config = {
      cmd = { "ridl-lsp" },
      filetypes = { "ridl" },
      root_dir = lspconfig.util.root_pattern(".git", "*.ridl"),
    },
  }
end

lspconfig.ridl_lsp.setup({})
```

**Claude Code** — install the bundled plugin from a checkout:

```sh
make install-claude-plugin
```

## Configuration

| Variable | Values | Default | Purpose |
|----------|--------|---------|---------|
| `RIDL_LSP_LOG_LEVEL` | `debug` / `info` / `warn` / `error` | `info` | Server log verbosity. Logs go to **stderr** (stdout is the LSP transport). An unrecognized value is reported and ignored. |

The server handles `SIGINT`/`SIGTERM` for a clean shutdown and follows the LSP
exit-code contract (exit `0` if `shutdown` precedes `exit`, otherwise `1`).

## Architecture

```text
cmd/ridl-lsp       — binary entrypoint and jsonrpc2/stdio transport wiring
internal/lsp       — LSP method handlers and capability declarations
internal/documents — open-document state, versions, and in-memory overlays
internal/workspace — workspace root management and path helpers
internal/ridl      — parser boundary and semantic analysis for RIDL
```

`internal/ridl` wraps the upstream `github.com/webrpc/webrpc` parser through an
overlay-aware filesystem so unsaved editor buffers are parsed as-is. To expose
token positions the upstream parser keeps private, it reads a few unexported
types via `go:linkname` and `unsafe.Pointer`; `TestUpstreamLayoutCanary` guards
those memory-layout assumptions so an upstream change fails CI instead of
silently corrupting positions. Request handlers run behind a panic-recovery
middleware (one bad document degrades a single request, never the whole server)
and honor request cancellation on the diagnostics path.

## Building & contributing

```sh
make build    # build ./bin/ridl-lsp
make test     # go test ./...
make lint     # golangci-lint
```

CI gates `go test -race ./...` and `golangci-lint`. When bumping the upstream
`webrpc` dependency, re-run the tests — `TestUpstreamLayoutCanary` and the
upstream error-format canaries will flag any parser-internal drift.

## License

[MIT](./LICENSE)
