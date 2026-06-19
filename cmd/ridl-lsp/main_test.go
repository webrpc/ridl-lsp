package main

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestNewLogger(t *testing.T) {
	debug, err := newLogger("debug")
	if err != nil {
		t.Fatalf("newLogger(debug): %v", err)
	}
	if !debug.Core().Enabled(zapcore.DebugLevel) {
		t.Fatal("debug level was not applied")
	}

	def, err := newLogger("")
	if err != nil {
		t.Fatalf("newLogger(empty): %v", err)
	}
	if def.Core().Enabled(zapcore.DebugLevel) {
		t.Fatal("empty level must keep the default (info), not enable debug")
	}

	// An invalid value must not fail startup over a typo, and must keep the default.
	bogus, err := newLogger("not-a-level")
	if err != nil {
		t.Fatalf("invalid level must not error: %v", err)
	}
	if bogus.Core().Enabled(zapcore.DebugLevel) {
		t.Fatal("invalid level must fall back to the default")
	}
}
