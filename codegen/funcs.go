package codegen

import (
	"strings"
	"synovium/ast"
)

// emitFunction generates the LLVM IR for a function declaration or definition.
func (b *Builder) emitFunction(node *ast.FunctionDecl) {
	typeID := b.Pool.NodeTypes[node]
	if typeID == 0 {
		return // Skip unresolved templates
	}

	funcType := b.Pool.Types[typeID]
	retLLVM := b.GetLLVMType(funcType.FuncReturn)

	var params []string
	for _, paramNode := range node.Parameters {
		paramTypeID := b.Pool.NodeTypes[paramNode]
		paramLLVM := b.GetLLVMType(paramTypeID)

		// If it's a declaration, we just need the types.
		// If it's a definition, we need the named virtual registers.
		if node.Body == nil {
			params = append(params, paramLLVM)
		} else {
			params = append(params, paramLLVM+" %"+paramNode.Name.Value)
		}
	}

	// Inject LLVM's variadic token
	if funcType.IsVariadic {
		if len(params) > 0 {
			params = append(params, "...")
		} else {
			params = []string{"..."}
		}
	}

	funcName := node.Name.Value

	// --- THE FIX: Declare vs Define ---
	if node.Body == nil {
		// It's a Foreign C FFI signature!
		b.EmitLine("declare %s @%s(%s)", retLLVM, funcName, strings.Join(params, ", "))
		b.EmitLine("")
		return
	}

	// It's a Native Synovium function!
	b.EmitLine("define %s @%s(%s) {", retLLVM, funcName, strings.Join(params, ", "))

	// Setup the entry block (Stub for now)
	b.EmitLine("entry:")
	if retLLVM == "void" {
		b.EmitLine("  ret void")
	} else {
		b.EmitLine("  ret %s undef", retLLVM)
	}
	b.EmitLine("}")
	b.EmitLine("")
}
