package main

import (
	"fmt"
	"os"
	"reflect"
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

	rawCode := string(content)

	// ==========================================
	// 🔎 STAGE 1: LEXICAL ANALYSIS VISUALIZER
	// ==========================================
	fmt.Println("🔎 STAGE 1: LEXICAL ANALYSIS")
	debugLexer := lexer.New(rawCode)
	tokenCount := 0
	for {
		tok := debugLexer.NextToken()
		if tok.Type == lexer.EOF {
			break
		}
		// Print a clean, formatted grid of tokens
		fmt.Printf("[%03d:%03d] %-12s '%s'\n", tok.Line, tok.Column, tok.Type, tok.Literal)
		tokenCount++
	}
	fmt.Printf("✅ Lexed %d tokens.\n\n", tokenCount)

	// ==========================================
	// 🌳 STAGE 2: SYNTACTIC ANALYSIS (AST)
	// ==========================================
	fmt.Println("🌳 STAGE 2: ABSTRACT SYNTAX TREE")
	l := lexer.New(rawCode)
	p := parser.New(l)
	program := p.ParseSourceFile()

	if len(p.Errors()) != 0 {
		fmt.Println("❌ PARSE ERRORS:")
		for _, msg := range p.Errors() {
			fmt.Printf("  - %s\n", msg)
		}
		os.Exit(1)
	}

	for _, decl := range program.Declarations {
		printNode(decl, "", true, "")
	}
	fmt.Println("\n✅ Parsing Successful.")

	// ==========================================
	// 🧠 STAGE 3: SEMANTIC ANALYSIS
	// ==========================================
	fmt.Println("🧠 STAGE 3: SEMANTIC ANALYSIS (DAG & TYPE EXECUTION)")
	pool := sema.NewTypePool()
	globalScope := sema.NewScope(nil)

	evaluator := sema.NewEvaluator(pool)
	evaluator.InjectBuiltins(globalScope)

	dag := sema.NewDAG(globalScope)
	sortedDecls, err := dag.BuildAndSort(program)
	if err != nil {
		fmt.Printf("❌ COMPTIME DAG ERROR:\n  - %v\n", err)
		os.Exit(1)
	}

	for _, decl := range sortedDecls {
		evaluator.Evaluate(decl, globalScope)
	}

	if len(evaluator.Errors) > 0 {
		fmt.Println("❌ SEMANTIC ERRORS:")
		for _, err := range evaluator.Errors {
			fmt.Printf("  - %s\n", err)
		}
	} else {
		fmt.Println("✅ Execution and Layouts verified.")
	}

	// ==========================================
	// 🗺️ STAGE 4: THE GLOBAL SYMBOL TABLE
	// ==========================================
	fmt.Println("🗺️  THE GLOBAL SYMBOL TABLE (Lexical Scope):")
	for name, sym := range globalScope.Symbols {
		mutStr := "Immutable"
		if sym.IsMutable {
			mutStr = "Mutable"
		}

		// Map the TypeID back to its string name for readability
		typeName := "<unresolved>"
		if int(sym.TypeID) < len(pool.Types) {
			typeName = pool.Types[sym.TypeID].Name
		}

		fmt.Printf("  • %-15s -> Type: %-20s [%s]\n", name, typeName, mutStr)
	}
	fmt.Println()

	// ==========================================
	// 🏊 STAGE 5: THE TYPE POOL
	// ==========================================
	fmt.Println("🏊 THE TYPE POOL (Computed Memory Layouts):")
	for _, t := range pool.Types {
		fmt.Printf("[%02d] %s (Size: %d bits)\n", t.ID, t.Name, t.TrueSizeBits)

		if t.Mask&sema.MaskIsFunction != 0 {
			paramNames := make([]string, len(t.FuncParams))
			for i, pID := range t.FuncParams {
				paramNames[i] = pool.Types[pID].Name
			}
			retName := pool.Types[t.FuncReturn].Name
			fmt.Printf("     Params: [%s] -> Ret: %s\n", strings.Join(paramNames, ", "), retName)

		} else if t.Mask&sema.MaskIsStruct != 0 && len(t.Fields) > 0 {
			fieldNames := []string{}
			for fName, fID := range t.Fields {
				fieldNames = append(fieldNames, fmt.Sprintf("%s:%s", fName, pool.Types[fID].Name))
			}
			fmt.Printf("     Fields: map[%s]\n", strings.Join(fieldNames, " "))

			if len(t.Methods) > 0 {
				methodNames := []string{}
				for mName, mID := range t.Methods {
					methodNames = append(methodNames, fmt.Sprintf("%s():%s", mName, pool.Types[mID].Name))
				}
				fmt.Printf("     Methods: %s\n", strings.Join(methodNames, ", "))
			}
		} else if t.Mask&sema.MaskIsStruct != 0 && len(t.Variants) > 0 {
			variantStrs := []string{}
			for vName, payloads := range t.Variants {
				pNames := []string{}
				for _, pID := range payloads {
					pNames = append(pNames, pool.Types[pID].Name)
				}
				if len(pNames) > 0 {
					variantStrs = append(variantStrs, fmt.Sprintf("%s(%s)", vName, strings.Join(pNames, ",")))
				} else {
					variantStrs = append(variantStrs, vName)
				}
			}
			fmt.Printf("     Variants: %s\n", strings.Join(variantStrs, " | "))
		} else if t.Mask&sema.MaskIsArray != 0 {
			fmt.Printf("     Base: %s, Capacity: %d\n", pool.Types[t.BaseType].Name, t.Capacity)
		} else if t.Mask&sema.MaskIsPointer != 0 {
			fmt.Printf("     Points to: %s\n", pool.Types[t.BaseType].Name)
		}
	}
}

// --- AST PRINTING HELPERS ---
type astChild struct {
	name string
	val  any
}

func printNode(node any, prefix string, isLast bool, name string) {
	if node == nil {
		return
	}

	v := reflect.ValueOf(node)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	branch := "├── "
	if isLast {
		branch = "└── "
	}

	nodeType := v.Type().Name()
	display := nodeType
	if name != "" {
		display = name + ": " + nodeType
	}

	if v.Kind() == reflect.Struct {
		if valField := v.FieldByName("Value"); valField.IsValid() && valField.Kind() == reflect.String {
			display += fmt.Sprintf(" (%s)", valField.String())
		} else if valField.IsValid() && valField.Kind() == reflect.Int64 {
			display += fmt.Sprintf(" (%d)", valField.Int())
		} else if opField := v.FieldByName("Operator"); opField.IsValid() && opField.Kind() == reflect.String {
			display += fmt.Sprintf(" [ %s ]", opField.String())
		}
	}

	fmt.Printf("%s%s%s\n", prefix, branch, display)

	childPrefix := prefix
	if isLast {
		childPrefix += "    "
	} else {
		childPrefix += "│   "
	}

	if v.Kind() != reflect.Struct {
		return
	}

	var children []astChild
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		if !field.IsExported() || field.Name == "Token" || strings.Contains(field.Name, "Span") {
			continue
		}

		fieldVal := v.Field(i)
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

	for i, child := range children {
		printNode(child.val, childPrefix, i == len(children)-1, child.name)
	}
}
