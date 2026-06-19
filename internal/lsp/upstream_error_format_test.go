package lsp

import (
	"context"
	"testing"

	ridlparser "github.com/webrpc/ridl-lsp/internal/ridl"
)

// TestUpstreamErrorFormatCanary guards the string-shape coupling to the upstream
// parser. errorToDiagnostic regex-extracts "line:col:" out of upstream error text
// to position diagnostics; if upstream changes that format, the regex silently
// stops matching and every diagnostic collapses to a full-line-1 smear. This pins
// the current format so such a drift fails CI instead of degrading positions.
func TestUpstreamErrorFormatCanary(t *testing.T) {
	const positioned = `webrpc = v1

name = test
version = v0.1.0

struct User
  - id: uint64
  - bad field here
`
	result, err := ridlparser.NewParser().Parse(context.Background(), t.TempDir(), "canary.ridl", map[string]string{"canary.ridl": positioned})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Fatal("fixture should have produced a positioned parse error")
	}

	diag := errorToDiagnostic(result.Errors[0])

	// The regex-matched path yields a narrow one-character range; the unmatched
	// fallback yields a full-line smear (Character 0..1000). A narrow range proves
	// the "line:col:" prefix was parsed out of the upstream message.
	if diag.Range.End.Character-diag.Range.Start.Character != 1 {
		t.Fatalf("expected a narrow position parsed from the upstream error format, got range %+v for message %q — upstream error format may have drifted", diag.Range, result.Errors[0].Error())
	}
}
