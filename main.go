package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"
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

	l := lexer.New(string(content))
	p := parser.New(l)
	program := p.ParseSourceFile()

	if len(p.Errors()) != 0 {
		fmt.Println("\n❌ PARSE ERRORS:")
		for _, msg := range p.Errors() {
			fmt.Printf("  - %s\n", msg)
		}
	} else {
		fmt.Println("\n✅ PARSING SUCCESSFUL.")
	}

	fmt.Println("\n🌳 ABSTRACT SYNTAX TREE:")
	if program != nil {
		for _, decl := range program.Declarations {
			printNode(decl, "", true, "")
			fmt.Println()
		}
	}
}

type astChild struct {
	name string
	val  any
}

// printNode recursively reflects over AST structs and prints them nicely
func printNode(node any, prefix string, isLast bool, name string) {
	if node == nil {
		return
	}

	// Determine the branch character
	branch := "├── "
	if isLast {
		branch = "└── "
	}

	// Extract value to inspect via reflection
	v := reflect.ValueOf(node)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	nodeType := v.Type().Name()
	display := nodeType
	if name != "" {
		display = name + ": " + nodeType
	}

	// Extract Literal value if it's a leaf node to make output cleaner
	if v.Kind() == reflect.Struct {
		if valField := v.FieldByName("Value"); valField.IsValid() && valField.Kind() == reflect.String {
			display += fmt.Sprintf(" (%s)", valField.String())
		} else if opField := v.FieldByName("Operator"); opField.IsValid() && opField.Kind() == reflect.String {
			display += fmt.Sprintf(" [ %s ]", opField.String())
		}
	}

	fmt.Printf("%s%s%s\n", prefix, branch, display)

	// Update prefix for children
	childPrefix := prefix
	if isLast {
		childPrefix += "    "
	} else {
		childPrefix += "│   "
	}

	if v.Kind() != reflect.Struct {
		return
	}

	// Collect valid children using our new clean struct
	var children []astChild

	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		// Skip unexported fields or Token/Span metadata to reduce noise
		if !field.IsExported() || field.Name == "Token" || strings.Contains(field.Name, "Span") {
			continue
		}

		fieldVal := v.Field(i)

		// Handle slices (like Statements, Fields, Declarations)
		if fieldVal.Kind() == reflect.Slice {
			for j := 0; j < fieldVal.Len(); j++ {
				children = append(children, astChild{
					name: fmt.Sprintf("%s[%d]", field.Name, j),
					val:  fieldVal.Index(j).Interface(),
				})
			}
		} else if fieldVal.Kind() == reflect.Ptr || fieldVal.Kind() == reflect.Interface {
			if !fieldVal.IsNil() {
				children = append(children, astChild{
					name: field.Name,
					val:  fieldVal.Interface(),
				})
			}
		}
	}

	// Print children
	for i, child := range children {
		printNode(child.val, childPrefix, i == len(children)-1, child.name)
	}
}
