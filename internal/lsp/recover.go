package lsp

import (
	"context"
	"fmt"
	"runtime/debug"

	"go.lsp.dev/jsonrpc2"
	"go.uber.org/zap"
)

// RecoverHandler wraps a jsonrpc2.Handler so a panic in any request handler is
// recovered and turned into a JSON-RPC error instead of crashing the process.
//
// go.lsp.dev runs each request in its own goroutine (jsonrpc2.AsyncHandler), and
// neither jsonrpc2 nor go.lsp.dev/protocol recovers panics. An unrecovered panic
// in a handler goroutine therefore terminates the whole language server — one
// malformed document would take down every editor feature until the client
// respawns it. This middleware degrades a panic to a single failed request.
//
// It must wrap the handler that does the work (the protocol ServerHandler), so it
// sits inside jsonrpc2.ReplyHandler: on panic it replies with an error, satisfying
// ReplyHandler's "must reply exactly once" contract. A reply already sent before
// the panic is not sent twice.
func RecoverHandler(handler jsonrpc2.Handler, logger *zap.Logger) jsonrpc2.Handler {
	return func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) (err error) {
		replied := false
		tracked := func(ctx context.Context, result any, replyErr error) error {
			replied = true
			return reply(ctx, result, replyErr)
		}

		defer func() {
			r := recover()
			if r == nil {
				return
			}

			logger.Error("recovered from panic in LSP handler",
				zap.String("method", req.Method()),
				zap.Any("panic", r),
				zap.ByteString("stack", debug.Stack()),
			)

			// A recovered panic must never become the handler's returned error:
			// a non-nil return is jsonrpc2's signal to fail (close) the
			// connection, which would defeat the purpose of recovering. Deliver
			// the failure to the client via reply when one hasn't been sent yet,
			// and always leave err nil.
			err = nil
			if replied {
				// Reply already sent; replying again would violate the
				// exactly-once contract. The panic is already logged.
				return
			}
			_ = tracked(ctx, nil, fmt.Errorf("panic recovered in %s: %v", req.Method(), r))
		}()

		return handler(ctx, tracked, req)
	}
}
