package codegen

import (
	"fmt"
	"strings"
	"synovium/ast"
)

func (b *Builder) emitBlock(block *ast.Block) string {
	var lastVal string
	for _, stmt := range block.Statements {
		switch n := stmt.(type) {
		case *ast.VariableDecl:
			varName := n.Name.Value
			llvmType := b.GetLLVMType(b.Pool.NodeTypes[n.Type])

			// 1. Stack Allocation
			reg := "%" + varName
			b.EmitLine("  %s = alloca %s", reg, llvmType)

			// 2. Evaluate RHS & Store
			valReg := b.emitExpression(n.Value)
			b.EmitLine("  store %s %s, %s* %s", llvmType, valReg, llvmType, reg)

			b.Locals[varName] = reg // Track it in scope!

		case *ast.ExprStmt:
			lastVal = b.emitExpression(n.Value)
		}
	}
	return lastVal
}

func (b *Builder) emitExpression(node ast.Expr) string {
	switch n := node.(type) {

	case *ast.IntLiteral:
		return fmt.Sprintf("%d", n.Value)

	case *ast.FloatLiteral:
		return n.Value

	case *ast.BoolLiteral:
		if n.Value {
			return "1"
		}
		return "0"

	case *ast.StringLiteral:
		// Hoist the string into global memory
		strLen := len(n.Value) + 1 // +1 for null terminator
		globalName := fmt.Sprintf("@.str.%d", b.nextStringID)
		b.nextStringID++

		llvmStr := strings.ReplaceAll(n.Value, "\n", "\\0A")
		b.StringConstants = append(b.StringConstants, fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"", globalName, strLen, llvmStr))

		// Return a pointer to it
		reg := b.NextReg()
		b.EmitLine("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0", reg, strLen, strLen, globalName)
		return reg

	case *ast.Identifier:
		// Load the variable from the stack
		ptrReg := b.Locals[n.Value]
		llvmType := b.GetLLVMType(b.Pool.NodeTypes[n])
		reg := b.NextReg()
		b.EmitLine("  %s = load %s, %s* %s", reg, llvmType, llvmType, ptrReg)
		return reg

	case *ast.StructInitExpr:
		typeID := b.Pool.NodeTypes[n]
		llvmType := b.GetLLVMType(typeID)
		structType := b.Pool.Types[typeID]

		// Allocate temporary struct
		tmpReg := b.NextReg()
		b.EmitLine("  %s = alloca %s", tmpReg, llvmType)

		for _, field := range n.Fields {
			valReg := b.emitExpression(field.Value)
			fieldIdx := structType.FieldIndices[field.Name.Value]
			fieldLLVM := b.GetLLVMType(structType.FieldLayout[fieldIdx])

			ptrReg := b.NextReg()
			b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d", ptrReg, llvmType, llvmType, tmpReg, fieldIdx)
			b.EmitLine("  store %s %s, %s* %s", fieldLLVM, valReg, fieldLLVM, ptrReg)
		}

		// Return the loaded struct value
		valReg := b.NextReg()
		b.EmitLine("  %s = load %s, %s* %s", valReg, llvmType, llvmType, tmpReg)
		return valReg

	case *ast.FieldAccessExpr:
		leftReg := b.emitExpression(n.Left)
		leftTypeID := b.Pool.NodeTypes[n.Left]
		fieldIdx := b.Pool.Types[leftTypeID].FieldIndices[n.Field.Value]

		// Because leftReg is a loaded value (not a pointer), we use `extractvalue`
		reg := b.NextReg()
		b.EmitLine("  %s = extractvalue %s %s, %d", reg, b.GetLLVMType(leftTypeID), leftReg, fieldIdx)
		return reg

	case *ast.CallExpr:
		funcName := n.Function.(*ast.Identifier).Value
		funcTypeID := b.Pool.NodeTypes[n.Function]
		retLLVM := b.GetLLVMType(b.Pool.Types[funcTypeID].FuncReturn)

		var args []string
		for _, arg := range n.Arguments {
			valReg := b.emitExpression(arg)
			argLLVM := b.GetLLVMType(b.Pool.NodeTypes[arg])
			args = append(args, fmt.Sprintf("%s %s", argLLVM, valReg))
		}

		if retLLVM == "void" {
			b.EmitLine("  call void @%s(%s)", funcName, strings.Join(args, ", "))
			return ""
		}
		reg := b.NextReg()
		b.EmitLine("  %s = call %s @%s(%s)", reg, retLLVM, funcName, strings.Join(args, ", "))
		return reg
	}

	return ""
}
