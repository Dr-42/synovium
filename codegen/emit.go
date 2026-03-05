package codegen

import (
	"fmt"
	"strings"

	"synovium/ast"
	"synovium/sema"
)

func (b *Builder) emitBlock(block *ast.Block) string {
	var lastVal string
	for _, stmt := range block.Statements {
		switch n := stmt.(type) {
		case *ast.VariableDecl:
			varName := n.Name.Value
			llvmType := b.GetLLVMType(b.Pool.NodeTypes[n.Type])

			reg := "%" + varName
			b.EmitLine("  %s = alloca %s", reg, llvmType)

			valReg := b.emitExpression(n.Value)
			b.EmitLine("  store %s %s, %s* %s", llvmType, valReg, llvmType, reg)

			b.Locals[varName] = reg

		case *ast.ExprStmt:
			lastVal = b.emitExpression(n.Value)
		}
	}
	return lastVal
}

// emitLValue resolves the memory address (pointer) of a variable or field.
// Returns (pointer_register, llvm_base_type)
func (b *Builder) emitLValue(node ast.Expr) (string, string) {
	switch n := node.(type) {
	case *ast.Identifier:
		return b.Locals[n.Value], b.GetLLVMType(b.Pool.NodeTypes[n])

	case *ast.FieldAccessExpr:
		leftReg := b.emitExpression(n.Left)
		leftTypeID := b.Pool.NodeTypes[n.Left]
		leftType := b.Pool.Types[leftTypeID]

		var structPtr string
		var structLLVM string

		// If the left side is a pointer (like `self` in take_damage), we already have the memory address!
		if (leftType.Mask & sema.MaskIsPointer) != 0 {
			structPtr = leftReg
			structLLVM = b.GetLLVMType(leftType.BaseType)
			leftTypeID = leftType.BaseType
		} else {
			// Otherwise, recursively get the memory address of the struct
			structPtr, structLLVM = b.emitLValue(n.Left)
		}

		fieldIdx := b.Pool.Types[leftTypeID].FieldIndices[n.Field.Value]
		ptrReg := b.NextReg()
		b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d", ptrReg, structLLVM, structLLVM, structPtr, fieldIdx)
		return ptrReg, b.GetLLVMType(b.Pool.Types[leftTypeID].FieldLayout[fieldIdx])

	case *ast.PrefixExpr:
		if n.Operator == "*" {
			// Dereferencing a pointer literally yields the pointer's value as an address
			ptrReg := b.emitExpression(n.Right)
			return ptrReg, b.GetLLVMType(b.Pool.NodeTypes[n])
		}
	}
	return "", ""
}

func (b *Builder) emitExpression(node ast.Expr) string {
	if node == nil {
		return ""
	}

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
		strLen := len(n.Value) + 1
		globalName := fmt.Sprintf("@.str.%d", b.nextStringID)
		b.nextStringID++

		llvmStr := strings.ReplaceAll(n.Value, "\n", "\\0A")
		b.StringConstants = append(b.StringConstants, fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"", globalName, strLen, llvmStr))

		reg := b.NextReg()
		b.EmitLine("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0", reg, strLen, strLen, globalName)
		return reg

	case *ast.Identifier:
		ptrReg := b.Locals[n.Value]
		llvmType := b.GetLLVMType(b.Pool.NodeTypes[n])
		reg := b.NextReg()
		b.EmitLine("  %s = load %s, %s* %s", reg, llvmType, llvmType, ptrReg)
		return reg

	case *ast.StructInitExpr:
		typeID := b.Pool.NodeTypes[n]
		llvmType := b.GetLLVMType(typeID)
		structType := b.Pool.Types[typeID]

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

		valReg := b.NextReg()
		b.EmitLine("  %s = load %s, %s* %s", valReg, llvmType, llvmType, tmpReg)
		return valReg

	case *ast.PrefixExpr:
		if n.Operator == "&" {
			ptrReg, _ := b.emitLValue(n.Right)
			return ptrReg
		}
		if n.Operator == "*" {
			ptrReg := b.emitExpression(n.Right)
			llvmType := b.GetLLVMType(b.Pool.NodeTypes[n])
			reg := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", reg, llvmType, llvmType, ptrReg)
			return reg
		}

	case *ast.InfixExpr:
		// --- ASSIGNMENTS ---
		isAssign := n.Operator == "=" || n.Operator == "+=" || n.Operator == "-="
		if isAssign {
			ptrReg, llvmType := b.emitLValue(n.Left)
			rightReg := b.emitExpression(n.Right)

			valReg := rightReg
			if n.Operator != "=" {
				currReg := b.NextReg()
				b.EmitLine("  %s = load %s, %s* %s", currReg, llvmType, llvmType, ptrReg)
				valReg = b.NextReg()
				isFloat := strings.Contains(llvmType, "double") || strings.Contains(llvmType, "float")
				op := n.Operator[:len(n.Operator)-1]
				if op == "+" {
					if isFloat {
						b.EmitLine("  %s = fadd %s %s, %s", valReg, llvmType, currReg, rightReg)
					} else {
						b.EmitLine("  %s = add %s %s, %s", valReg, llvmType, currReg, rightReg)
					}
				} else if op == "-" {
					if isFloat {
						b.EmitLine("  %s = fsub %s %s, %s", valReg, llvmType, currReg, rightReg)
					} else {
						b.EmitLine("  %s = sub %s %s, %s", valReg, llvmType, currReg, rightReg)
					}
				}
			}
			b.EmitLine("  store %s %s, %s* %s", llvmType, valReg, llvmType, ptrReg)
			return ""
		}

		// --- STANDARD MATH & LOGIC ---
		leftReg := b.emitExpression(n.Left)
		rightReg := b.emitExpression(n.Right)
		llvmType := b.GetLLVMType(b.Pool.NodeTypes[n.Left])
		reg := b.NextReg()
		isFloat := strings.Contains(llvmType, "double") || strings.Contains(llvmType, "float")

		switch n.Operator {
		case "+":
			if isFloat {
				b.EmitLine("  %s = fadd %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = add %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "-":
			if isFloat {
				b.EmitLine("  %s = fsub %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = sub %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "*":
			if isFloat {
				b.EmitLine("  %s = fmul %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = mul %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "/":
			if isFloat {
				b.EmitLine("  %s = fdiv %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = sdiv %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "==":
			if isFloat {
				b.EmitLine("  %s = fcmp oeq %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp eq %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "<=":
			if isFloat {
				b.EmitLine("  %s = fcmp ole %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp sle %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		}
		return reg

	case *ast.FieldAccessExpr:
		// Reading a field as an R-value. Get its pointer, then load it!
		ptrReg, llvmType := b.emitLValue(n)
		reg := b.NextReg()
		b.EmitLine("  %s = load %s, %s* %s", reg, llvmType, llvmType, ptrReg)
		return reg

	case *ast.CallExpr:
		var funcName string
		var args []string

		if id, ok := n.Function.(*ast.Identifier); ok {
			funcName = id.Value
		} else if fa, ok := n.Function.(*ast.FieldAccessExpr); ok {
			funcName = fa.Field.Value

			// THE FIX: Implicit 'self' Injection!
			leftTypeID := b.Pool.NodeTypes[fa.Left]
			leftType := b.Pool.Types[leftTypeID]
			funcTypeID := b.Pool.NodeTypes[n.Function]
			expectedSelfTypeID := b.Pool.Types[funcTypeID].FuncParams[0]
			expectedSelfMask := b.Pool.Types[expectedSelfTypeID].Mask

			var selfReg string
			if (expectedSelfMask&sema.MaskIsPointer) != 0 && (leftType.Mask&sema.MaskIsPointer) == 0 {
				// Auto-reference: Function wants a pointer, we have a value
				selfReg, _ = b.emitLValue(fa.Left)
			} else if (expectedSelfMask&sema.MaskIsPointer) == 0 && (leftType.Mask&sema.MaskIsPointer) != 0 {
				// Auto-dereference: Function wants a value, we have a pointer
				ptrReg := b.emitExpression(fa.Left)
				selfReg = b.NextReg()
				b.EmitLine("  %s = load %s, %s* %s", selfReg, b.GetLLVMType(expectedSelfTypeID), b.GetLLVMType(expectedSelfTypeID), ptrReg)
			} else {
				// Exact match (e.g., boss_ptr.take_damage)
				selfReg = b.emitExpression(fa.Left)
			}

			selfLLVM := b.GetLLVMType(expectedSelfTypeID)
			args = append(args, fmt.Sprintf("%s %s", selfLLVM, selfReg))
		}

		funcTypeID := b.Pool.NodeTypes[n.Function]
		retLLVM := b.GetLLVMType(b.Pool.Types[funcTypeID].FuncReturn)

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

	default:
		// Gracefully skip unimplemented AST nodes (like Arrays, Loops, Match)
		// so the compiler doesn't panic while we test!
		b.EmitLine("  ; TODO: codegen for %T", n)
		return ""
	}

	return ""
}
