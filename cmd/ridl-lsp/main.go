package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.uber.org/zap"

	ridllsp "github.com/webrpc/ridl-lsp"
	"github.com/webrpc/ridl-lsp/internal/lsp"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("ridl-lsp %s\n", ridllsp.VERSION)
		os.Exit(0)
	}

	log.Println("ridl-lsp starting on stdio")

	ctx := context.Background()
	stream := jsonrpc2.NewStream(stdrwc{})

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("create logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush on shutdown

	server := lsp.NewServer(logger)
	_, conn, client := protocol.NewServer(ctx, server, stream, logger)
	server.SetClient(client)

	<-conn.Done()
}

type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdrwc) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdrwc) Close() error                { return nil }
