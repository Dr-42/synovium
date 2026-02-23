package tree_sitter_tree_sitter_synovium_test

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_tree_sitter_synovium "github.com/tree-sitter/tree-sitter-tree_sitter_synovium/bindings/go"
)

func TestCanLoadGrammar(t *testing.T) {
	language := tree_sitter.NewLanguage(tree_sitter_tree_sitter_synovium.Language())
	if language == nil {
		t.Errorf("Error loading TreeSitter Synovium grammar")
	}
}
