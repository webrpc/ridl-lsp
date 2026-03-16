.PHONY: install lint test

install:
	go install ./cmd/ridl-lsp

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./... --fix -c .golangci.yml

test:
	go test ./...
