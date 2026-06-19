package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	ridllsp "github.com/webrpc/ridl-lsp"
	"github.com/webrpc/ridl-lsp/internal/lsp"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("ridl-lsp %s\n", ridllsp.VERSION)
		os.Exit(0)
	}

	log.Println("ridl-lsp starting on stdio")

	// Cancel on SIGINT/SIGTERM so a supervised/containerized server (the Docker
	// ENTRYPOINT) shuts the connection down cleanly instead of being killed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stream := jsonrpc2.NewStream(stdrwc{})

	logger, err := newLogger(os.Getenv("RIDL_LSP_LOG_LEVEL"))
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

	select {
	case <-conn.Done():
		// Client closed the stream. Per the LSP spec, exit 0 only if shutdown was
		// received first; a drop without shutdown is abnormal and exits non-zero.
		if !server.ShutdownReceived() {
			_ = logger.Sync() //nolint:errcheck // best-effort flush before non-zero exit
			os.Exit(1)
		}
	case <-ctx.Done():
		// SIGINT/SIGTERM: close the connection and exit cleanly.
		log.Println("ridl-lsp: signal received, shutting down")
		_ = conn.Close()
	}
}

// newLogger builds the production logger at the level named by level (debug,
// info, warn, error). An empty string keeps the default, and an unparseable value
// is reported and ignored rather than failing startup over a typo'd env var.
func newLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if level != "" {
		var l zapcore.Level
		if err := l.UnmarshalText([]byte(level)); err != nil {
			log.Printf("ridl-lsp: ignoring invalid RIDL_LSP_LOG_LEVEL %q: %v", level, err)
		} else {
			cfg.Level = zap.NewAtomicLevelAt(l)
		}
	}
	return cfg.Build()
}

type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdrwc) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdrwc) Close() error                { return nil }
