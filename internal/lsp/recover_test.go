package lsp

import (
	"context"
	"testing"

	"go.lsp.dev/jsonrpc2"
	"go.uber.org/zap"
)

func newTestRequest(t *testing.T) jsonrpc2.Request {
	t.Helper()
	req, err := jsonrpc2.NewNotification("textDocument/didChange", nil)
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	return req
}

func TestRecoverHandler_PanicBeforeReplyBecomesError(t *testing.T) {
	panicking := jsonrpc2.Handler(func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		panic("boom")
	})

	replyCount := 0
	var replyErr error
	reply := func(ctx context.Context, result any, err error) error {
		replyCount++
		replyErr = err
		return nil
	}

	h := RecoverHandler(panicking, zap.NewNop())

	// Must not propagate the panic.
	err := h(context.Background(), reply, newTestRequest(t))

	if replyCount != 1 {
		t.Fatalf("reply called %d times, want 1", replyCount)
	}
	if replyErr == nil {
		t.Fatal("expected reply to carry an error on panic")
	}
	// A recovered panic must not propagate as a returned error: jsonrpc2 treats a
	// non-nil handler return as connection-fatal.
	if err != nil {
		t.Fatalf("recovered panic must return nil, got %v", err)
	}
}

func TestRecoverHandler_PanicAfterReplyDoesNotReplyTwice(t *testing.T) {
	panicking := jsonrpc2.Handler(func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		_ = reply(ctx, "partial", nil)
		panic("boom after reply")
	})

	replyCount := 0
	reply := func(ctx context.Context, result any, err error) error {
		replyCount++
		return nil
	}

	h := RecoverHandler(panicking, zap.NewNop())

	err := h(context.Background(), reply, newTestRequest(t))

	if replyCount != 1 {
		t.Fatalf("reply called %d times, want 1 (must not double-reply)", replyCount)
	}
	if err != nil {
		t.Fatalf("recovered panic must return nil, got %v", err)
	}
}

func TestRecoverHandler_PassThrough(t *testing.T) {
	called := false
	inner := jsonrpc2.Handler(func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		called = true
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

	h := RecoverHandler(inner, zap.NewNop())

	if err := h(context.Background(), reply, newTestRequest(t)); err != nil {
		t.Fatalf("pass-through returned error: %v", err)
	}
	if !called {
		t.Fatal("inner handler was not called")
	}
	if replyCount != 1 {
		t.Fatalf("reply called %d times, want 1", replyCount)
	}
	if gotResult != "ok" || gotErr != nil {
		t.Fatalf("reply got (%v, %v), want (ok, nil)", gotResult, gotErr)
	}
}
