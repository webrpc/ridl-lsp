package ridl

import (
	"path/filepath"
	"testing"
)

// TestUpstreamLayoutCanary guards the unsafe.Pointer struct mirrors in parser.go
// against upstream memory-layout drift. The mirrors (upstreamToken,
// upstreamParser, upstreamErrorNode, upstreamArgumentNode, upstreamTokenNode)
// reconstruct the field order of the unexported webrpc/webrpc schema/ridl types
// and read them through unsafe.Pointer. If upstream reorders, retypes, or resizes
// any field ahead of one we read, the casts silently resolve to the wrong offset.
// This test parses a fixture with known positions and asserts every mirrored read
// returns the expected value, so such drift fails CI instead of corrupting hover,
// go-to-definition, and diagnostics in the field.
//
// When this test fails after a webrpc bump, re-diff the mirrored structs in
// parser.go against the upstream definitions (schema/ridl/lexer.go,
// parser.go, parser_node.go) before touching the assertions.
func TestUpstreamLayoutCanary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "canary.ridl")

	// Line numbers are 1-based and hand-counted from this literal. Keep the
	// layout stable if you edit it, or update the expectations below.
	//
	//  1: webrpc = v1
	//  2:
	//  3: name = test
	//  4: version = v1.0.0
	//  5:
	//  6: error 100 NotFound "not found" HTTP 404
	//  7:
	//  8: struct Item
	//  9:   - id: uint64
	// 10:
	// 11: struct FetchReq
	// 12:   - q: string
	// 13:
	// 14: struct FetchResp
	// 15:   - item: Item
	// 16:
	// 17: service API
	// 18:   - Fetch(FetchReq) => (FetchResp)
	content := "webrpc = v1\n" +
		"\n" +
		"name = test\n" +
		"version = v1.0.0\n" +
		"\n" +
		"error 100 NotFound \"not found\" HTTP 404\n" +
		"\n" +
		"struct Item\n" +
		"  - id: uint64\n" +
		"\n" +
		"struct FetchReq\n" +
		"  - q: string\n" +
		"\n" +
		"struct FetchResp\n" +
		"  - item: Item\n" +
		"\n" +
		"service API\n" +
		"  - Fetch(FetchReq) => (FetchResp)\n"

	writeTestFile(t, path, content)

	result, err := NewParser().Parse(dir, path, nil)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected no parse errors, got %v", result.Errors)
	}

	// upstreamParser.root offset: a wrong offset yields a garbage/empty root.
	root := result.Root
	if root == nil {
		t.Fatal("result.Root is nil — upstreamParser.root offset likely drifted")
	}
	if got := len(root.Errors()); got != 1 {
		t.Fatalf("Errors(): got %d, want 1 (root mirror drift?)", got)
	}
	if got := len(root.Structs()); got != 3 {
		t.Fatalf("Structs(): got %d, want 3 (root mirror drift?)", got)
	}
	if got := len(root.Services()); got != 1 {
		t.Fatalf("Services(): got %d, want 1 (root mirror drift?)", got)
	}

	// upstreamErrorNode field offsets (code/message/httpStatus).
	errNode := root.Errors()[0]
	if got := errNode.Name().String(); got != "NotFound" {
		t.Fatalf("error Name(): got %q, want %q", got, "NotFound")
	}
	if got := ErrorCodeToken(errNode).String(); got != "100" {
		t.Fatalf("ErrorCodeToken: got %q, want %q (errorNode mirror drift?)", got, "100")
	}
	if got := ErrorMessageToken(errNode).String(); got != "not found" {
		t.Fatalf("ErrorMessageToken: got %q, want %q (errorNode mirror drift?)", got, "not found")
	}
	if got := ErrorHTTPStatusToken(errNode).String(); got != "404" {
		t.Fatalf("ErrorHTTPStatusToken: got %q, want %q (errorNode mirror drift?)", got, "404")
	}

	// upstreamToken.line/col offsets, read via upstreamTokenNode.tok. The
	// "NotFound" token is on line 6; upstream records col as the token's 1-based
	// END column — "NotFound" spans columns 11-18, so col is 18.
	nameTok := errNode.Name()
	if got := TokenLine(nameTok); got != 6 {
		t.Fatalf("TokenLine(NotFound): got %d, want 6 (token mirror drift?)", got)
	}
	if got := TokenCol(nameTok); got != 18 {
		t.Fatalf("TokenCol(NotFound): got %d, want 18 (token mirror drift?)", got)
	}

	// upstreamArgumentNode.inlineStruct offset: the succinct argument form
	// "Fetch(FetchReq)" stores the type as inlineStruct (no name/argumentType).
	svc := root.Services()[0]
	if got := len(svc.Methods()); got != 1 {
		t.Fatalf("service Methods(): got %d, want 1", got)
	}
	inputs := svc.Methods()[0].Inputs()
	if got := len(inputs); got != 1 {
		t.Fatalf("method Inputs(): got %d, want 1", got)
	}
	if got := ArgumentTypeToken(inputs[0]).String(); got != "FetchReq" {
		t.Fatalf("ArgumentTypeToken (inline struct): got %q, want %q (argumentNode mirror drift?)", got, "FetchReq")
	}
}
