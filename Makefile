lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./... --fix -c .golangci.yml

lint-ci:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint run ./... -c .golangci.yml

test:
	go test ./...
