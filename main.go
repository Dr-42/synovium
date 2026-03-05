package main

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"

	"synovium/ast"
	"synovium/codegen"
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
	// 🌳 STAGE 1 & 2: SYNTACTIC ANALYSIS
	// ==========================================
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

	// ==========================================
	// 🧠 STAGE 3: SEMANTIC ANALYSIS & DAG
	// ==========================================
	pool := sema.NewTypePool()
	globalScope := sema.NewScope(nil)

	evaluator := sema.NewEvaluator(pool, rawCode)
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
		os.Exit(1)
	}

	// ==========================================
	// 🌲 STAGE 4: PRINT TYPED AST (TAST)
	// ==========================================
	fmt.Println("🌲 TYPED ABSTRACT SYNTAX TREE (TAST)")
	for _, decl := range sortedDecls {
		printNode(decl, "", true, "", pool)
	}
	fmt.Println()

	// ==========================================
	// 📜 STAGE 7: LLVM IR CODE GENERATION
	// ==========================================
	fmt.Println("📜 STAGE 7: LLVM IR OUTPUT")
	fmt.Println("--------------------------------------------------")

	builder := codegen.NewBuilder(pool)
	llvmIR := builder.Generate(sortedDecls)

	fmt.Println(llvmIR)
	fmt.Println("--------------------------------------------------")

	// ==========================================
	// 🚀 STAGE 8: NATIVE COMPILATION & EXECUTION
	// ==========================================
	fmt.Println("🚀 COMPILING TO NATIVE BINARY...")

	// 1. Write the LLVM IR to a file
	llFilename := "out.ll"
	err = os.WriteFile(llFilename, []byte(llvmIR), 0644)
	if err != nil {
		fmt.Printf("❌ Failed to write LLVM IR: %v\n", err)
		os.Exit(1)
	}

	// 2. Invoke Clang to compile the LLVM IR into a native executable
	exeFilename := "./app"
	cmd := exec.Command("clang", "-Wno-override-module", "-o", exeFilename, llFilename)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	err = cmd.Run()
	if err != nil {
		fmt.Printf("❌ Clang compilation failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Compilation successful! Executing binary...\n")
	fmt.Println("================ PROGRAM OUTPUT ================")

	// 3. Run the compiled binary!
	runCmd := exec.Command(exeFilename)
	runCmd.Stderr = os.Stderr
	runCmd.Stdout = os.Stdout
	runCmd.Stdin = os.Stdin

	err = runCmd.Run()
	if err != nil {
		fmt.Printf("\n❌ Execution failed: %v\n", err)
	}

	fmt.Println("\n================================================")

	// Optional: Clean up the generated files
	os.Remove(llFilename)
	// os.Remove(exeFilename)
}

// --- AST PRINTING HELPERS ---
type astChild struct {
	name string
	val  any
}

func printNode(node any, prefix string, isLast bool, name string, pool *sema.TypePool) {
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

	// 🔬 THE TAST SIDE-TABLE LOOKUP
	if pool != nil {
		if astNode, ok := node.(ast.Node); ok {
			if typeID, exists := pool.NodeTypes[astNode]; exists {
				if int(typeID) < len(pool.Types) {
					// Append the proven type in Cyan!
					display += fmt.Sprintf("  ->  \033[36m%s\033[0m", pool.Types[typeID].Name)
				}
			}
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
		printNode(child.val, childPrefix, i == len(children)-1, child.name, pool)
	}
}
