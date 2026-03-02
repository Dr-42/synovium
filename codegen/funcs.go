package codegen

import (
	"strings"
	"synovium/ast"
)

func (b *Builder) emitFunction(node *ast.FunctionDecl) {
	typeID := b.Pool.NodeTypes[node]
	if typeID == 0 {
		return
	}

	funcType := b.Pool.Types[typeID]
	retLLVM := b.GetLLVMType(funcType.FuncReturn)

	var params []string
	for i, paramNode := range node.Parameters {
		// THE FIX: Directly access the mathematically proven parameters!
		paramTypeID := funcType.FuncParams[i]
		paramLLVM := b.GetLLVMType(paramTypeID)

		if node.Body == nil {
			params = append(params, paramLLVM)
		} else {
			params = append(params, paramLLVM+" %"+paramNode.Name.Value)
		}
	}

	if funcType.IsVariadic {
		if len(params) > 0 {
			params = append(params, "...")
		} else {
			params = []string{"..."}
		}
	}

	funcName := node.Name.Value

	if node.Body == nil {
		b.EmitLine("declare %s @%s(%s)", retLLVM, funcName, strings.Join(params, ", "))
		b.EmitLine("")
		return
	}

	b.EmitLine("define %s @%s(%s) {", retLLVM, funcName, strings.Join(params, ", "))
	b.EmitLine("entry:")

	// --- Push parameters to the stack so they can be mutated! ---
	b.Locals = make(map[string]string)
	for i, paramNode := range node.Parameters {
		pName := paramNode.Name.Value
		pLLVM := b.GetLLVMType(funcType.FuncParams[i])
		b.EmitLine("  %%%s = alloca %s", pName, pLLVM)
		b.EmitLine("  store %s %%%s, %s* %%%s", pLLVM, pName, pLLVM, pName)
		b.Locals[pName] = "%" + pName
	}

	// --- Ignite the Core Engine ---
	b.emitBlock(node.Body)

	if retLLVM == "void" {
		b.EmitLine("  ret void")
	} else {
		b.EmitLine("  ret %s undef", retLLVM)
	}
	b.EmitLine("}")
	b.EmitLine("")
}
