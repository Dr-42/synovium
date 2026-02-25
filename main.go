package main

import (
	"fmt"
	"os"
	"synovium/lexer"
	"synovium/parser"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <file.syn>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error: could not read file '%s'\n%v\n", filename, err)
		os.Exit(1)
	}

	fmt.Println("======================================================")
	fmt.Printf(" COMPILING: %s\n", filename)
	fmt.Println("======================================================")

	// 1. Init Lexer & Parser
	l := lexer.New(string(content))
	p := parser.New(l)

	// 2. Build the AST
	program := p.ParseSourceFile()

	// 3. Print Errors if any
	if len(p.Errors()) != 0 {
		fmt.Println("\n❌ PARSE ERRORS:")
		for _, msg := range p.Errors() {
			fmt.Printf("  - %s\n", msg)
		}
		fmt.Println("\n⚠️  Showing partial AST up to the point of failure:")
	} else {
		fmt.Println("\n✅ PARSING SUCCESSFUL. AST GENERATED:")
	}

	// 4. Print the AST
	if program != nil {
		for i, decl := range program.Declarations {
			// %#v prints the Go structs with their field names so we can inspect the tree
			fmt.Printf("\n[Node %d] %#v\n", i, decl)
		}
	}
}
