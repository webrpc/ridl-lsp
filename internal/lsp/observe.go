package lsp

import (
	"context"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.uber.org/zap"
)

// ObserveHandler wraps a jsonrpc2.Handler to log per-request observability for
// every LSP request: the method, server processing time, and any error.
//
// It times from when the request body runs to when the reply is sent — server
// processing time, not transport or queue wait. It logs at Debug so it stays
// silent unless RIDL_LSP_LOG_LEVEL=debug, and writes only to the server logger
// (stderr): stdout is the JSON-RPC transport and must never be logged to.
func ObserveHandler(handler jsonrpc2.Handler, logger *zap.Logger) jsonrpc2.Handler {
	return func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		start := time.Now()
		method := req.Method()

		timedReply := func(ctx context.Context, result any, replyErr error) error {
			fields := []zap.Field{
				zap.String("method", method),
				zap.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			}
			if replyErr != nil {
				fields = append(fields, zap.Error(replyErr))
			}
			logger.Debug("lsp request", fields...)
			return reply(ctx, result, replyErr)
		}

		return handler(ctx, timedReply, req)
	}
}
