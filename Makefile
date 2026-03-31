.PHONY: all build install lint test install-claude-plugin uninstall-claude-plugin

all: build test lint

build:
	go build -ldflags="-s -w -X github.com/webrpc/ridl-lsp.VERSION=$$(git describe --tags)" -o ./bin/ridl-lsp ./cmd/ridl-lsp

install:
	go install -ldflags="-s -w -X github.com/webrpc/ridl-lsp.VERSION=$$(git describe --tags)" ./cmd/ridl-lsp

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./... --fix -c .golangci.yml

test:
	go test ./...

install-claude-plugin:
	@bash scripts/install-claude-plugin.sh

uninstall-claude-plugin:
	@bash scripts/install-claude-plugin.sh --uninstall
