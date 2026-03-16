package lsp

import ridl "github.com/webrpc/ridl-lsp/internal/ridl"

func argumentTypeToken(arg *ridl.ArgumentNode) *ridl.TokenNode {
	return ridl.ArgumentTypeToken(arg)
}
