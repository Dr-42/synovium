package main

import (
	"fmt"
	"synovium/lexer"
)

func main() {
	input := `
struct Person {
    name : str,
    id : i64,
}

enum Gender {
    Male,
    NonBinary(NonBinaryData),
}

a : i32 = 78;
b : i32 ~= 90;
c : i90 := 40;
`
	l := lexer.New(input)

	fmt.Printf("%-15s %-15s %-10s %-10s\n", "TYPE", "LITERAL", "LINE", "COLUMN")
	fmt.Println("-----------------------------------------------------")

	for {
		tok := l.NextToken()
		fmt.Printf("%-15s %-15s %-10d %-10d\n", tok.Type, tok.Literal, tok.Line, tok.Column)
		if tok.Type == lexer.EOF {
			break
		}
	}
}
