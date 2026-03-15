package ridl

// Parser is the seam between generic LSP plumbing and RIDL-specific analysis.
// This lets us swap in the existing webrpc RIDL parser without coupling the
// transport layer to its concrete types too early.
type Parser interface {
	Parse(path string, content string) (*Result, error)
}

type Result struct {
	Diagnostics int
}
