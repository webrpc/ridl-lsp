#!/usr/bin/env bash

set -euo pipefail

PLUGIN_NAME="ridl-lsp"
PLUGIN_DIR="$HOME/.claude/plugins/local"
MARKETPLACE_DIR="$PLUGIN_DIR/.claude-plugin"
MARKETPLACE_FILE="$MARKETPLACE_DIR/marketplace.json"
PLUGIN_DEST="$PLUGIN_DIR/plugins/$PLUGIN_NAME/.claude-plugin"
PLUGIN_FILE="$PLUGIN_DEST/plugin.json"

if [[ -t 1 ]]; then
  COLOR_RESET=$'\033[0m'
  COLOR_INFO=$'\033[1;34m'
  COLOR_STEP=$'\033[1;36m'
  COLOR_SUCCESS=$'\033[1;32m'
  COLOR_WARN=$'\033[1;33m'
  COLOR_ERROR=$'\033[1;31m'
else
  COLOR_RESET='' COLOR_INFO='' COLOR_STEP='' COLOR_SUCCESS='' COLOR_WARN='' COLOR_ERROR=''
fi

log_info()    { printf "%s==>%s %s\n" "$COLOR_INFO"    "$COLOR_RESET" "$1"; }
log_step()    { printf "%s  ->%s %s\n" "$COLOR_STEP"    "$COLOR_RESET" "$1"; }
log_success() { printf "%ssuccess:%s %s\n" "$COLOR_SUCCESS" "$COLOR_RESET" "$1"; }
log_warn()    { printf "%swarn:%s %s\n"    "$COLOR_WARN"    "$COLOR_RESET" "$1" >&2; }
log_error()   { printf "%serror:%s %s\n"   "$COLOR_ERROR"   "$COLOR_RESET" "$1" >&2; }

resolve_binary() {
  local gopath
  gopath="$(go env GOPATH 2>/dev/null || true)"
  if [[ -z "$gopath" ]]; then
    log_error "go env GOPATH is empty — is Go installed?"
    exit 1
  fi
  # Take the first entry if GOPATH has multiple paths.
  local first="${gopath%%:*}"
  printf "%s/bin/%s" "$first" "$PLUGIN_NAME"
}

do_install() {
  local binary_path
  binary_path="$(resolve_binary)"

  if [[ ! -x "$binary_path" ]]; then
    log_error "$binary_path not found — run 'make install' or 'go install ./cmd/ridl-lsp' first"
    exit 1
  fi

  log_info "Installing Claude Code plugin: $PLUGIN_NAME"
  log_step "binary: $binary_path"

  # --- marketplace.json ---
  mkdir -p "$MARKETPLACE_DIR"

  local entry
  entry=$(cat <<'ENTRY'
{
  "name": "ridl-lsp",
  "description": "RIDL language server for .ridl schema files",
  "source": "./plugins/ridl-lsp",
  "category": "development"
}
ENTRY
)

  if [[ -f "$MARKETPLACE_FILE" ]]; then
    if command -v jq >/dev/null 2>&1; then
      # Merge: update existing entry or append.
      local updated
      updated=$(jq --argjson entry "$entry" '
        if (.plugins | map(.name) | index("ridl-lsp")) then
          .plugins |= map(if .name == "ridl-lsp" then $entry else . end)
        else
          .plugins += [$entry]
        end
      ' "$MARKETPLACE_FILE")
      printf "%s\n" "$updated" > "$MARKETPLACE_FILE"
      log_step "updated existing marketplace.json"
    else
      if jq_free_has_plugin "$MARKETPLACE_FILE"; then
        log_step "marketplace.json already contains $PLUGIN_NAME (skipped)"
      else
        log_warn "marketplace.json exists but jq is not installed — overwriting (other plugins may be lost)"
        write_marketplace "$MARKETPLACE_FILE"
      fi
    fi
  else
    write_marketplace "$MARKETPLACE_FILE"
    log_step "created marketplace.json"
  fi

  # --- plugin.json ---
  mkdir -p "$PLUGIN_DEST"
  cat > "$PLUGIN_FILE" <<EOF
{
  "name": "ridl-lsp",
  "version": "1.0.0",
  "description": "RIDL language server for .ridl schema files",
  "author": { "name": "webrpc" },
  "lspServers": {
    "ridl": {
      "command": "$binary_path",
      "args": [],
      "extensionToLanguage": { ".ridl": "ridl" },
      "transport": "stdio"
    }
  }
}
EOF
  log_step "wrote plugin.json"

  # --- Register with Claude CLI (optional) ---
  if command -v claude >/dev/null 2>&1; then
    log_info "Registering plugin with Claude Code"
    claude plugin marketplace add "$PLUGIN_DIR" 2>/dev/null || true
    claude plugin install "$PLUGIN_NAME@local" --scope user 2>/dev/null || true
    log_step "registered via claude CLI"
  else
    log_warn "claude CLI not found — plugin files written but not registered"
    log_warn "run 'claude plugin marketplace add $PLUGIN_DIR' and 'claude plugin install $PLUGIN_NAME@local --scope user' later"
  fi

  log_success "Claude Code plugin installed"
}

do_uninstall() {
  log_info "Uninstalling Claude Code plugin: $PLUGIN_NAME"

  if command -v claude >/dev/null 2>&1; then
    claude plugin uninstall "$PLUGIN_NAME" 2>/dev/null || true
    log_step "unregistered via claude CLI"
  fi

  if [[ -d "$PLUGIN_DIR/plugins/$PLUGIN_NAME" ]]; then
    rm -rf "$PLUGIN_DIR/plugins/$PLUGIN_NAME"
    log_step "removed plugin directory"
  fi

  if [[ -f "$MARKETPLACE_FILE" ]] && command -v jq >/dev/null 2>&1; then
    local updated
    updated=$(jq '.plugins |= map(select(.name != "ridl-lsp"))' "$MARKETPLACE_FILE")
    printf "%s\n" "$updated" > "$MARKETPLACE_FILE"
    log_step "removed entry from marketplace.json"
  fi

  log_success "Claude Code plugin uninstalled"
}

write_marketplace() {
  cat > "$1" <<'EOF'
{
  "name": "local",
  "description": "Local Claude Code plugins",
  "owner": { "name": "webrpc" },
  "plugins": [
    {
      "name": "ridl-lsp",
      "description": "RIDL language server for .ridl schema files",
      "source": "./plugins/ridl-lsp",
      "category": "development"
    }
  ]
}
EOF
}

jq_free_has_plugin() {
  grep -q '"ridl-lsp"' "$1" 2>/dev/null
}

case "${1:-}" in
  --uninstall) do_uninstall ;;
  *)           do_install ;;
esac
