package main

import (
	"fmt"
	"os"
	"synovium/lexer"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <file.syn>\n", os.Args[0])
		os.Exit(1)
	}

	content, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("Error: could not read file '%s'\n%v\n", os.Args[1], err)
		os.Exit(1)
	}

	fmt.Printf("Lexing: %s\n", os.Args[1])
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Printf("%-5s | %-5s | %-11s | %-15s | %s\n", "Line", "Col", "Span[S:E]", "Type", "Literal")
	fmt.Println("-------------------------------------------------------------------------")

	l := lexer.New(string(content))

	for {
		tok := l.NextToken()
		spanStr := fmt.Sprintf("[%d:%d]", tok.Span.Start, tok.Span.End)
		fmt.Printf("%-5d | %-5d | %-11s | %-15s | '%s'\n", tok.Line, tok.Column, spanStr, tok.Type, tok.Literal)
		if tok.Type == lexer.EOF {
			break
		}
	}
}
