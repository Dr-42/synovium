package codegen

import (
	"strings"

	"synovium/ast"
	"synovium/sema"
)

func (b *Builder) emitFunction(node *ast.FunctionDecl, typeID sema.TypeID) {
	if typeID == 0 {
		return
	}

	b.nextRegID = 1
	funcType := b.Pool.Types[typeID]

	// THE FIX: Skip Generic Templates (but allow instantiations to pass!)
	if !strings.Contains(funcType.Name, "_inst_") {
		for _, p := range funcType.FuncParams {
			if b.Pool.Types[p].Name == "type" {
				return
			}
		}
	}

	retLLVM := b.GetLLVMType(funcType.FuncReturn)
	var params []string

	for i, paramNode := range node.Parameters {
		paramTypeID := funcType.FuncParams[i]

		// THE FIX: Strip Ghost Parameters
		if b.Pool.Types[paramTypeID].Name == "type" {
			continue
		}

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
	if strings.Contains(funcType.Name, "_inst_") {
		funcName = funcType.Name
	}

	opNames := map[string]string{"+": "add", "-": "sub", "*": "mul", "/": "div", "%": "mod", "==": "eq", "!=": "neq", "<": "lt", ">": "gt", "<=": "lte", ">=": "gte"}
	if safeOp, isOp := opNames[node.Name.Value]; isOp {
		// Clean struct name mapping: Vec_op_add
		selfType := b.Pool.Types[funcType.FuncParams[0]]
		if (selfType.Mask & sema.MaskIsPointer) != 0 {
			selfType = b.Pool.Types[selfType.BaseType]
		}
		funcName = selfType.Name + "_op_" + safeOp
	}

	if node.Body == nil {
		b.EmitLine("declare %s @%s(%s)", retLLVM, funcName, strings.Join(params, ", "))
		b.EmitLine("")
		return
	}

	b.EmitLine("define %s @%s(%s) {", retLLVM, funcName, strings.Join(params, ", "))
	b.EmitLine("entry:")

	b.Locals = make(map[string]string)
	for i, paramNode := range node.Parameters {
		paramTypeID := funcType.FuncParams[i]
		if b.Pool.Types[paramTypeID].Name == "type" {
			continue
		}
		pName := paramNode.Name.Value
		pLLVM := b.GetLLVMType(paramTypeID)
		allocName := pName + ".addr"

		b.EmitLine("  %%%s = alloca %s", allocName, pLLVM)
		b.EmitLine("  store %s %%%s, %s* %%%s", pLLVM, pName, pLLVM, allocName)
		b.Locals[pName] = "%" + allocName
	}

	bodyVal := b.emitBlock(node.Body)

	isMain := funcType.Name == "main"
	if bodyVal != "<terminated>" {
		if isMain {
			b.EmitLine("  ret i32 0")
		} else if retLLVM == "void" {
			b.EmitLine("  ret void")
		} else if bodyVal != "" {
			b.EmitLine("  ret %s %s", retLLVM, bodyVal) // Return the bubbled register!
		} else {
			b.EmitLine("  ret %s undef", retLLVM)
		}
	}

	b.EmitLine("}")
	b.EmitLine("")
}
