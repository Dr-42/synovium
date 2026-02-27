package main

import (
	"fmt"
	"os"
	"strings"
	"synovium/lexer"
	"synovium/parser"
	"synovium/sema"
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

	// 1. Frontend: Lex and Parse
	l := lexer.New(string(content))
	p := parser.New(l)
	program := p.ParseSourceFile()

	if len(p.Errors()) != 0 {
		fmt.Println("\n❌ PARSE ERRORS:")
		for _, msg := range p.Errors() {
			fmt.Printf("  - %s\n", msg)
		}
		os.Exit(1)
	}
	fmt.Println("✅ Parsing Successful.")

	// 2. Semantic Initialization
	pool := sema.NewTypePool()
	globalScope := sema.NewScope(nil)

	evaluator := sema.NewEvaluator(pool)
	evaluator.InjectBuiltins(globalScope) // Load i32, f64, bln, etc.

	// 3. Top-Level Hoisting & DAG Sort
	dag := sema.NewDAG(globalScope)
	sortedDecls, err := dag.BuildAndSort(program)
	if err != nil {
		fmt.Printf("\n❌ COMPTIME DAG ERROR:\n  - %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ DAG Topological Sort Successful.")

	// 4. The Type Executor
	for _, decl := range sortedDecls {
		evaluator.Evaluate(decl, globalScope)
	}

	// 5. Diagnostics
	if len(evaluator.Errors) > 0 {
		fmt.Println("\n❌ SEMANTIC ERRORS:")
		for _, err := range evaluator.Errors {
			fmt.Printf("  - %s\n", err)
		}
	} else {
		fmt.Println("\n✅ SEMANTIC ANALYSIS COMPLETE.")
	}

	// 6. Inspect the Boolean Engine's Memory Layouts
	fmt.Println("\n🏊 THE TYPE POOL (Computed Memory Layouts):")
	for _, t := range pool.Types {
		fmt.Printf("[%d] %s (Size: %d bits)\n", t.ID, t.Name, t.TrueSizeBits)

		if t.Mask&sema.MaskIsFunction != 0 {
			// Map parameter IDs to Names
			paramNames := make([]string, len(t.FuncParams))
			for i, pID := range t.FuncParams {
				paramNames[i] = pool.Types[pID].Name
			}
			retName := pool.Types[t.FuncReturn].Name

			fmt.Printf("    Params: [%s] -> Ret: %s\n", strings.Join(paramNames, " "), retName)

		} else if t.Mask&sema.MaskIsStruct != 0 && len(t.Fields) > 0 {
			// Map field IDs to Names
			fieldNames := []string{}
			for fName, fID := range t.Fields {
				fieldNames = append(fieldNames, fmt.Sprintf("%s:%s", fName, pool.Types[fID].Name))
			}
			fmt.Printf("    Fields: map[%s]\n", strings.Join(fieldNames, " "))
		}
	}
}
