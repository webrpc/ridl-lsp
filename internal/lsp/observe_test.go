package lsp

import (
	"context"
	"errors"
	"testing"

	"go.lsp.dev/jsonrpc2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func observeTestRequest(t *testing.T) jsonrpc2.Request {
	t.Helper()
	req, err := jsonrpc2.NewNotification("textDocument/didChange", nil)
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	return req
}

func TestObserveHandlerLogsMethodAndDuration(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)

	inner := jsonrpc2.Handler(func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		return reply(ctx, "ok", nil)
	})

	var gotResult any
	var gotErr error
	replyCount := 0
	reply := func(ctx context.Context, result any, err error) error {
		replyCount++
		gotResult = result
		gotErr = err
		return nil
	}

	err := ObserveHandler(inner, zap.New(core))(context.Background(), reply, observeTestRequest(t))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	// The wrapped reply must forward the inner result/err unchanged.
	if replyCount != 1 {
		t.Fatalf("reply called %d times, want 1", replyCount)
	}
	if gotResult != "ok" || gotErr != nil {
		t.Fatalf("reply forwarding: got (%v, %v), want (ok, nil)", gotResult, gotErr)
	}

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("log entries: got %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["method"] != "textDocument/didChange" {
		t.Fatalf("method field: got %v, want textDocument/didChange", fields["method"])
	}
	if _, ok := fields["duration_ms"]; !ok {
		t.Fatalf("missing duration_ms field; got %v", fields)
	}
	if _, ok := fields["error"]; ok {
		t.Fatalf("unexpected error field on a successful request: %v", fields["error"])
	}
}

func TestObserveHandlerLogsError(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	wantErr := errors.New("boom")

	inner := jsonrpc2.Handler(func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		return reply(ctx, nil, wantErr)
	})
	reply := func(ctx context.Context, result any, err error) error { return nil }

	_ = ObserveHandler(inner, zap.New(core))(context.Background(), reply, observeTestRequest(t))

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("log entries: got %d, want 1", len(entries))
	}
	if _, ok := entries[0].ContextMap()["error"]; !ok {
		t.Fatalf("expected an error field on a failed request; got %v", entries[0].ContextMap())
	}
}
