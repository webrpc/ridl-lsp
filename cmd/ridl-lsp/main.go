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

	// Wire the connection like protocol.NewServer, but wrap the server handler in
	// RecoverHandler so a panic in any request degrades to a single failed request
	// instead of crashing the whole language server (handlers run in their own
	// goroutines via jsonrpc2.AsyncHandler, where an unrecovered panic is fatal).
	conn := jsonrpc2.NewConn(stream)
	client := protocol.ClientDispatcher(conn, logger.Named("client"))
	ctx = protocol.WithClient(ctx, client)
	server.SetClient(client)

	handler := lsp.RecoverHandler(
		protocol.ServerHandler(server, jsonrpc2.MethodNotFoundHandler),
		logger,
	)
	conn.Go(ctx, protocol.Handlers(handler))

	<-conn.Done()
}

type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdrwc) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdrwc) Close() error                { return nil }
