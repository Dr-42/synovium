package lexer

import (
	"os"
	"testing"
)

func TestLexerSpans(t *testing.T) {
	data, err := os.ReadFile("../tree-sitter-synovium/test.syn")
	if err != nil {
		panic(err)
	}

	// Convert bytes to string
	input := string(data)
	l := New(input)

	for {
		tok := l.NextToken()
		if tok.Type == EOF {
			break
		}

		// MATHEMATICAL ASSERTION:
		// Slice the raw source file using our exact byte span.
		spanSlice := input[tok.Span.Start:tok.Span.End]

		// If the span doesn't perfectly match the literal, fail the test instantly.
		if spanSlice != tok.Literal {
			t.Fatalf("Span mismatch for token %s at [%d:%d].\nExpected Literal: '%s'\nGot Span Slice: '%s'",
				tok.Type, tok.Span.Start, tok.Span.End, tok.Literal, spanSlice)
		}
	}
}
