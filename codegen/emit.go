package codegen

import (
	"fmt"
	"sort"
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

			// --- NEW: Hard Return Statements ---
		case *ast.ReturnStmt:
			if n.Value != nil {
				valReg := b.emitExpression(n.Value)
				llvmType := b.GetLLVMType(b.Pool.NodeTypes[n.Value])
				b.EmitLine("  ret %s %s", llvmType, valReg)
			} else {
				b.EmitLine("  ret void")
			}
			return "<terminated>" // <-- THE FIX
		case *ast.BreakStmt:
			if len(b.LoopExits) > 0 {
				b.EmitLine("  br label %%%s", b.LoopExits[len(b.LoopExits)-1])
			}
			return "<terminated>" // <-- THE FIX
		}
	}

	if block.Value != nil {
		lastVal = b.emitExpression(block.Value)
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

	case *ast.IndexExpr:
		arrPtr, arrLLVM := b.emitLValue(n.Left)
		idxReg := b.emitExpression(n.Index)
		idxLLVM := b.GetLLVMType(b.Pool.NodeTypes[n.Index])

		arrTypeID := b.Pool.NodeTypes[n.Left]
		elemTypeID := b.Pool.Types[arrTypeID].BaseType
		elemLLVM := b.GetLLVMType(elemTypeID)

		ptrReg := b.NextReg()
		// i32 0 steps through the array pointer, the second offset steps to the index!
		b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, %s %s", ptrReg, arrLLVM, arrLLVM, arrPtr, idxLLVM, idxReg)
		return ptrReg, elemLLVM

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
		// THE FIX: Escape '%' so Go's fmt.Sprintf doesn't panic!
		llvmStr = strings.ReplaceAll(llvmStr, "%", "%%")

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

	case *ast.IndexExpr:
		ptrReg, llvmType := b.emitLValue(n)
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
		// --- 1. OPERATOR OVERLOADING HOOK ---
		leftTypeID := b.Pool.NodeTypes[n.Left]
		leftType := b.Pool.Types[leftTypeID]
		isAssign := n.Operator == "=" || n.Operator == "~=" || n.Operator == "+=" || n.Operator == "-="

		if !leftType.IsFundamental && !isAssign {
			actualLeft := leftType
			if (actualLeft.Mask & sema.MaskIsPointer) != 0 {
				actualLeft = b.Pool.Types[actualLeft.BaseType]
			}

			if methodID, exists := actualLeft.Methods[n.Operator]; exists {
				expectedSelf := b.Pool.Types[methodID].FuncParams[0]
				expectedOther := b.Pool.Types[methodID].FuncParams[1]

				// Coerce Left Operand
				var selfReg string
				if (b.Pool.Types[expectedSelf].Mask&sema.MaskIsPointer) != 0 && (leftType.Mask&sema.MaskIsPointer) == 0 {
					selfReg, _ = b.emitLValue(n.Left) // Auto-Ref
				} else if (b.Pool.Types[expectedSelf].Mask&sema.MaskIsPointer) == 0 && (leftType.Mask&sema.MaskIsPointer) != 0 {
					ptrReg := b.emitExpression(n.Left)
					selfReg = b.NextReg()
					b.EmitLine("  %s = load %s, %s* %s", selfReg, b.GetLLVMType(expectedSelf), b.GetLLVMType(expectedSelf), ptrReg) // Auto-Deref
				} else {
					selfReg = b.emitExpression(n.Left)
				}

				// Coerce Right Operand
				rightTypeID := b.Pool.NodeTypes[n.Right]
				rightType := b.Pool.Types[rightTypeID]
				var rightReg string

				if (b.Pool.Types[expectedOther].Mask&sema.MaskIsPointer) != 0 && (rightType.Mask&sema.MaskIsPointer) == 0 {
					rightReg, _ = b.emitLValue(n.Right) // Auto-Ref
				} else if (b.Pool.Types[expectedOther].Mask&sema.MaskIsPointer) == 0 && (rightType.Mask&sema.MaskIsPointer) != 0 {
					ptrReg := b.emitExpression(n.Right)
					rightReg = b.NextReg()
					b.EmitLine("  %s = load %s, %s* %s", rightReg, b.GetLLVMType(expectedOther), b.GetLLVMType(expectedOther), ptrReg) // Auto-Deref
				} else {
					rightReg = b.emitExpression(n.Right)
				}

				retLLVM := b.GetLLVMType(b.Pool.Types[methodID].FuncReturn)
				args := []string{
					fmt.Sprintf("%s %s", b.GetLLVMType(expectedSelf), selfReg),
					fmt.Sprintf("%s %s", b.GetLLVMType(expectedOther), rightReg),
				}

				opNames := map[string]string{"+": "add", "-": "sub", "*": "mul", "/": "div", "%": "mod", "==": "eq", "!=": "neq", "<": "lt", ">": "gt", "<=": "lte", ">=": "gte"}
				safeOp := opNames[n.Operator]
				if safeOp == "" {
					safeOp = "op"
				}
				funcName := actualLeft.Name + "_op_" + safeOp

				reg := b.NextReg()
				b.EmitLine("  %s = call %s @%s(%s)", reg, retLLVM, funcName, strings.Join(args, ", "))
				return reg
			}
		}
		// --- ASSIGNMENTS ---
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
		case "%":
			if isFloat {
				b.EmitLine("  %s = frem %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = srem %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "&":
			b.EmitLine("  %s = and %s %s, %s", reg, llvmType, leftReg, rightReg)
		case "|":
			b.EmitLine("  %s = or %s %s, %s", reg, llvmType, leftReg, rightReg)
		case "^":
			b.EmitLine("  %s = xor %s %s, %s", reg, llvmType, leftReg, rightReg)
		case "<<":
			b.EmitLine("  %s = shl %s %s, %s", reg, llvmType, leftReg, rightReg)
		case ">>":
			b.EmitLine("  %s = ashr %s %s, %s", reg, llvmType, leftReg, rightReg)
		case "==":
			if isFloat {
				b.EmitLine("  %s = fcmp oeq %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp eq %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "!=":
			if isFloat {
				b.EmitLine("  %s = fcmp one %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp ne %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "<":
			if isFloat {
				b.EmitLine("  %s = fcmp olt %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp slt %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "<=":
			if isFloat {
				b.EmitLine("  %s = fcmp ole %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp sle %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case ">":
			if isFloat {
				b.EmitLine("  %s = fcmp ogt %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp sgt %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case ">=":
			if isFloat {
				b.EmitLine("  %s = fcmp oge %s %s, %s", reg, llvmType, leftReg, rightReg)
			} else {
				b.EmitLine("  %s = icmp sge %s %s, %s", reg, llvmType, leftReg, rightReg)
			}
		case "&&":
			b.EmitLine("  %s = and i1 %s, %s", reg, leftReg, rightReg)
		case "||":
			b.EmitLine("  %s = or i1 %s, %s", reg, leftReg, rightReg)
		}
		return reg

	case *ast.FieldAccessExpr:
		leftTypeID := b.Pool.NodeTypes[n.Left]
		leftType := b.Pool.Types[leftTypeID]

		// 1. Is it a Payload-less Enum Variant? (e.g. State.Invincible)
		if payloadTypes, isVariant := leftType.Variants[n.Field.Value]; isVariant && len(payloadTypes) == 0 {
			enumLLVM := b.GetLLVMType(leftType.ID)

			var variantNames []string
			for k := range leftType.Variants {
				variantNames = append(variantNames, k)
			}
			sort.Strings(variantNames)
			tagIndex := 0
			for idx, name := range variantNames {
				if name == n.Field.Value {
					tagIndex = idx
					break
				}
			}

			enumReg := b.NextReg()
			b.EmitLine("  %s = alloca %s", enumReg, enumLLVM)
			tagPtr := b.NextReg()
			b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0", tagPtr, enumLLVM, enumLLVM, enumReg)
			b.EmitLine("  store i8 %d, i8* %s", tagIndex, tagPtr)

			valReg := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", valReg, enumLLVM, enumLLVM, enumReg)
			return valReg
		}

		// 2. Otherwise, reading a standard struct field as an R-value.
		ptrReg, llvmType := b.emitLValue(n)
		reg := b.NextReg()
		b.EmitLine("  %s = load %s, %s* %s", reg, llvmType, llvmType, ptrReg)
		return reg

	case *ast.CallExpr:
		var funcName string
		var args []string
		var argOffset int = 0

		if id, ok := n.Function.(*ast.Identifier); ok {
			funcName = id.Value
		} else if fa, ok := n.Function.(*ast.FieldAccessExpr); ok {
			funcName = fa.Field.Value
			leftTypeID := b.Pool.NodeTypes[fa.Left]
			leftType := b.Pool.Types[leftTypeID]

			actualObjType := leftType
			if (actualObjType.Mask & sema.MaskIsPointer) != 0 {
				actualObjType = b.Pool.Types[actualObjType.BaseType]
			}

			if _, isMethod := actualObjType.Methods[fa.Field.Value]; isMethod {
				argOffset = 1
				funcTypeID := b.Pool.NodeTypes[n.Function]
				expectedSelfTypeID := b.Pool.Types[funcTypeID].FuncParams[0]
				expectedSelfMask := b.Pool.Types[expectedSelfTypeID].Mask

				var selfReg string
				if (expectedSelfMask&sema.MaskIsPointer) != 0 && (leftType.Mask&sema.MaskIsPointer) == 0 {
					selfReg, _ = b.emitLValue(fa.Left)
				} else if (expectedSelfMask&sema.MaskIsPointer) == 0 && (leftType.Mask&sema.MaskIsPointer) != 0 {
					ptrReg := b.emitExpression(fa.Left)
					selfReg = b.NextReg()
					b.EmitLine("  %s = load %s, %s* %s", selfReg, b.GetLLVMType(expectedSelfTypeID), b.GetLLVMType(expectedSelfTypeID), ptrReg)
				} else {
					selfReg = b.emitExpression(fa.Left)
				}
				selfLLVM := b.GetLLVMType(expectedSelfTypeID)
				args = append(args, fmt.Sprintf("%s %s", selfLLVM, selfReg))
			} else {
				// Enum Constructor
				funcName = actualObjType.Name + "::" + fa.Field.Value
			}
		}

		funcTypeID := b.Pool.NodeTypes[n.Function]
		funcType := b.Pool.Types[funcTypeID]
		retLLVM := b.GetLLVMType(funcType.FuncReturn)

		if strings.Contains(funcType.Name, "_inst_") {
			funcName = funcType.Name
		}

		// --- THE FIX: INLINE ENUM MEMORY PACKING ---
		if strings.Contains(funcName, "::") {
			parts := strings.Split(funcName, "::")
			vName := parts[len(parts)-1]

			enumTypeID := funcType.FuncReturn
			enumLLVM := b.GetLLVMType(enumTypeID)
			actualEnum := b.Pool.Types[enumTypeID]

			// 1. Generate Deterministic Tag ID
			var variantNames []string
			for k := range actualEnum.Variants {
				variantNames = append(variantNames, k)
			}
			sort.Strings(variantNames)
			tagIndex := 0
			for i, name := range variantNames {
				if name == vName {
					tagIndex = i
					break
				}
			}

			// 2. Allocate the empty buffer and write the Tag
			enumReg := b.NextReg()
			b.EmitLine("  %s = alloca %s", enumReg, enumLLVM)
			tagPtr := b.NextReg()
			b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0", tagPtr, enumLLVM, enumLLVM, enumReg)
			b.EmitLine("  store i8 %d, i8* %s", tagIndex, tagPtr)

			// 3. Bitcast and Pack the Payload Data!
			if len(n.Arguments) > 0 {
				var payloadLLVMTypes []string
				payloadLLVMTypes = append(payloadLLVMTypes, "i8") // The tag offset
				var argRegs []string

				// Extract argument values
				for _, arg := range n.Arguments {
					argRegs = append(argRegs, b.emitExpression(arg))
					payloadLLVMTypes = append(payloadLLVMTypes, b.GetLLVMType(b.Pool.NodeTypes[arg]))
				}

				// Forge the dynamic struct and cast the pointer
				variantStructLLVM := fmt.Sprintf("{ %s }", strings.Join(payloadLLVMTypes, ", "))
				castPtr := b.NextReg()
				b.EmitLine("  %s = bitcast %s* %s to %s*", castPtr, enumLLVM, enumReg, variantStructLLVM)

				// Write values directly into the buffer!
				for i, argReg := range argRegs {
					fieldPtr := b.NextReg()
					b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d", fieldPtr, variantStructLLVM, variantStructLLVM, castPtr, i+1)
					b.EmitLine("  store %s %s, %s* %s", payloadLLVMTypes[i+1], argReg, payloadLLVMTypes[i+1], fieldPtr)
				}
			}

			valReg := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", valReg, enumLLVM, enumLLVM, enumReg)
			return valReg
		}

		for i, arg := range n.Arguments {
			paramIndex := i + argOffset
			if paramIndex < len(funcType.FuncParams) {
				expectedParamTypeID := funcType.FuncParams[paramIndex]
				if b.Pool.Types[expectedParamTypeID].Name == "type" {
					continue
				}
			}

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

	case *ast.ArrayInitExpr:
		typeID := b.Pool.NodeTypes[n]
		llvmType := b.GetLLVMType(typeID) // e.g. [3 x i32]

		arrReg := b.NextReg()
		b.EmitLine("  %s = alloca %s", arrReg, llvmType)

		elemTypeID := b.Pool.Types[typeID].BaseType
		elemLLVM := b.GetLLVMType(elemTypeID)

		if n.Count != nil {
			// --- NEW: Generate an LLVM loop for repeat initialization ---
			valReg := b.emitExpression(n.Elements[0])
			capacity := b.Pool.Types[typeID].Capacity

			idxPtr := b.NextReg()
			b.EmitLine("  %s = alloca i32", idxPtr)
			b.EmitLine("  store i32 0, i32* %s", idxPtr)

			condLbl := b.NextLabel()
			bodyLbl := b.NextLabel()
			exitLbl := b.NextLabel()

			b.EmitLine("  br label %%%s", condLbl)
			b.EmitLine("\n%s:", condLbl)

			idxVal := b.NextReg()
			b.EmitLine("  %s = load i32, i32* %s", idxVal, idxPtr)
			cmpReg := b.NextReg()
			b.EmitLine("  %s = icmp slt i32 %s, %d", cmpReg, idxVal, capacity)
			b.EmitLine("  br i1 %s, label %%%s, label %%%s", cmpReg, bodyLbl, exitLbl)

			b.EmitLine("\n%s:", bodyLbl)
			ptrReg := b.NextReg()
			b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %s", ptrReg, llvmType, llvmType, arrReg, idxVal)
			b.EmitLine("  store %s %s, %s* %s", elemLLVM, valReg, elemLLVM, ptrReg)

			nextIdx := b.NextReg()
			b.EmitLine("  %s = add i32 %s, 1", nextIdx, idxVal)
			b.EmitLine("  store i32 %s, i32* %s", nextIdx, idxPtr)
			b.EmitLine("  br label %%%s", condLbl)

			b.EmitLine("\n%s:", exitLbl)
		} else {
			// Standard Population
			for i, el := range n.Elements {
				valReg := b.emitExpression(el)
				ptrReg := b.NextReg()
				b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d", ptrReg, llvmType, llvmType, arrReg, i)
				b.EmitLine("  store %s %s, %s* %s", elemLLVM, valReg, elemLLVM, ptrReg)
			}
		}

		valReg := b.NextReg()
		b.EmitLine("  %s = load %s, %s* %s", valReg, llvmType, llvmType, arrReg)
		return valReg

	case *ast.IfExpr:
		typeID := b.Pool.NodeTypes[n]
		llvmType := b.GetLLVMType(typeID)

		// If the `if` block bubbles a value, allocate a hidden stack variable for it!
		var resultPtr string
		if llvmType != "void" {
			resultPtr = b.NextReg()
			b.EmitLine("  %s = alloca %s", resultPtr, llvmType)
		}

		mergeLbl := b.NextLabel()

		// We will loop through the main If and all Elifs.
		conds := append([]ast.Expr{n.Condition}, n.ElifConds...)
		bodies := append([]*ast.Block{n.Body}, n.ElifBodies...)

		nextCondLbl := b.NextLabel()
		b.EmitLine("  br label %%%s", nextCondLbl)

		for i := 0; i < len(conds); i++ {
			b.EmitLine("\n%s:", nextCondLbl)
			condReg := b.emitExpression(conds[i])

			bodyLbl := b.NextLabel()
			nextCondLbl = b.NextLabel() // Setup the next fallback label

			b.EmitLine("  br i1 %s, label %%%s, label %%%s", condReg, bodyLbl, nextCondLbl)

			// Generate the true branch
			b.EmitLine("\n%s:", bodyLbl)
			bodyVal := b.emitBlock(bodies[i])

			// THE FIX: Do not emit a merge jump if the block has a return/break!
			if bodyVal != "<terminated>" {
				if llvmType != "void" && bodyVal != "" {
					b.EmitLine("  store %s %s, %s* %s", llvmType, bodyVal, llvmType, resultPtr)
				}
				b.EmitLine("  br label %%%s", mergeLbl)
			}
		}

		b.EmitLine("\n%s:", nextCondLbl)
		if n.ElseBody != nil {
			elseVal := b.emitBlock(n.ElseBody)
			if elseVal != "<terminated>" {
				if llvmType != "void" && elseVal != "" {
					b.EmitLine("  store %s %s, %s* %s", llvmType, elseVal, llvmType, resultPtr)
				}
				b.EmitLine("  br label %%%s", mergeLbl)
			}
		} else {
			b.EmitLine("  br label %%%s", mergeLbl)
		}

		// The Merge Block!
		b.EmitLine("\n%s:", mergeLbl)
		if llvmType != "void" {
			valReg := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", valReg, llvmType, llvmType, resultPtr)
			return valReg
		}
		return ""

	case *ast.LoopExpr:
		condLbl := b.NextLabel()
		bodyLbl := b.NextLabel()
		exitLbl := b.NextLabel()

		b.LoopExits = append(b.LoopExits, exitLbl)

		var loopVar string
		var loopEndReg string
		var loopLLVM string
		isRange := false

		// 1. Intercept `i = 0...3` and initialize `i = 0`
		if n.Condition != nil {
			if vDecl, ok := n.Condition.(*ast.VariableDecl); ok {
				if inf, ok := vDecl.Value.(*ast.InfixExpr); ok && inf.Operator == "..." {
					isRange = true
					b.emitBlock(&ast.Block{Statements: []ast.Stmt{&ast.VariableDecl{
						Name: vDecl.Name, Type: vDecl.Type, Value: inf.Left,
					}}})
					loopVar = b.Locals[vDecl.Name.Value]
					loopLLVM = b.GetLLVMType(b.Pool.NodeTypes[vDecl.Type])
					loopEndReg = b.emitExpression(inf.Right)
				} else {
					b.emitBlock(&ast.Block{Statements: []ast.Stmt{n.Condition.(ast.Stmt)}})
				}
			}
		}

		b.EmitLine("  br label %%%s", condLbl)
		b.EmitLine("\n%s:", condLbl)

		// 2. Evaluate Loop Bounds or Boolean Condition dynamically!
		if isRange {
			currVal := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", currVal, loopLLVM, loopLLVM, loopVar)
			cmpReg := b.NextReg()
			b.EmitLine("  %s = icmp slt %s %s, %s", cmpReg, loopLLVM, currVal, loopEndReg)
			b.EmitLine("  br i1 %s, label %%%s, label %%%s", cmpReg, bodyLbl, exitLbl)
		} else if n.Condition != nil {
			condExpr := n.Condition.(ast.Expr)
			condReg := b.emitExpression(condExpr)
			b.EmitLine("  br i1 %s, label %%%s, label %%%s", condReg, bodyLbl, exitLbl)
		} else {
			// Infinite loop fallback
			b.EmitLine("  br label %%%s", bodyLbl)
		}

		b.EmitLine("\n%s:", bodyLbl)
		bodyVal := b.emitBlock(n.Body)

		if bodyVal != "<terminated>" {
			// 3. Loop Increment (`i++`) ONLY for range loops
			if isRange {
				rawName := strings.TrimPrefix(loopVar, "%")
				currVal := "%" + rawName + "_curr"
				nextVal := "%" + rawName + "_next"

				b.EmitLine("  %s = load %s, %s* %s", currVal, loopLLVM, loopLLVM, loopVar)
				b.EmitLine("  %s = add %s %s, 1", nextVal, loopLLVM, currVal)
				b.EmitLine("  store %s %s, %s* %s", loopLLVM, nextVal, loopLLVM, loopVar)
			}
			b.EmitLine("  br label %%%s", condLbl) // Loop back
		}

		b.EmitLine("\n%s:", exitLbl) // Break target
		b.LoopExits = b.LoopExits[:len(b.LoopExits)-1]
		return ""

	case *ast.MatchExpr:
		typeID := b.Pool.NodeTypes[n]
		llvmType := b.GetLLVMType(typeID)

		var resultPtr string
		if llvmType != "void" {
			resultPtr = b.NextReg()
			b.EmitLine("  %s = alloca %s", resultPtr, llvmType)
		}

		// 1. Evaluate Enum Value and store it on the stack so we can bitcast its pointer
		targetReg := b.emitExpression(n.Value)
		targetLLVM := b.GetLLVMType(b.Pool.NodeTypes[n.Value])

		targetPtr := b.NextReg()
		b.EmitLine("  %s = alloca %s", targetPtr, targetLLVM)
		b.EmitLine("  store %s %s, %s* %s", targetLLVM, targetReg, targetLLVM, targetPtr)

		// 2. Extract Tag
		tagReg := b.NextReg()
		b.EmitLine("  %s = extractvalue %s %s, 0", tagReg, targetLLVM, targetReg)

		// Get deterministic tags mapping
		enumType := b.Pool.Types[b.Pool.NodeTypes[n.Value]]
		var variantNames []string
		for k := range enumType.Variants {
			variantNames = append(variantNames, k)
		}
		sort.Strings(variantNames)

		mergeLbl := b.NextLabel()
		defaultLbl := b.NextLabel()

		// 3. Emit correct Tag Switching!
		b.EmitLine("  switch i8 %s, label %%%s [", tagReg, defaultLbl)
		armLabels := make([]string, len(n.Arms))
		for i, arm := range n.Arms {
			armLabels[i] = b.NextLabel()
			variantNameStr := arm.Pattern.Value
			parts := strings.Split(variantNameStr, ".")
			vName := parts[len(parts)-1]

			tagIndex := 0
			for idx, name := range variantNames {
				if name == vName {
					tagIndex = idx
					break
				}
			}
			b.EmitLine("    i8 %d, label %%%s", tagIndex, armLabels[i])
		}
		b.EmitLine("  ]")

		// 4. Emit the Arms (Unboxing the Payload!)
		for i, arm := range n.Arms {
			b.EmitLine("\n%s:", armLabels[i])

			variantNameStr := arm.Pattern.Value
			parts := strings.Split(variantNameStr, ".")
			vName := parts[len(parts)-1]
			payloadTypes := enumType.Variants[vName]

			if len(arm.Params) > 0 {
				var payloadLLVMTypes []string
				payloadLLVMTypes = append(payloadLLVMTypes, "i8") // tag offset
				for _, pt := range payloadTypes {
					payloadLLVMTypes = append(payloadLLVMTypes, b.GetLLVMType(pt))
				}
				variantStructLLVM := fmt.Sprintf("{ %s }", strings.Join(payloadLLVMTypes, ", "))

				// Bitcast the raw buffer into our specific variant struct
				castPtr := b.NextReg()
				b.EmitLine("  %s = bitcast %s* %s to %s*", castPtr, targetLLVM, targetPtr, variantStructLLVM)

				// Pull values out into standard local registers
				for j, param := range arm.Params {
					paramLLVM := b.GetLLVMType(payloadTypes[j])
					b.EmitLine("  %%%s = alloca %s", param.Value, paramLLVM)
					b.Locals[param.Value] = "%" + param.Value

					fieldPtr := b.NextReg()
					b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d", fieldPtr, variantStructLLVM, variantStructLLVM, castPtr, j+1)

					valReg := b.NextReg()
					b.EmitLine("  %s = load %s, %s* %s", valReg, paramLLVM, paramLLVM, fieldPtr)
					b.EmitLine("  store %s %s, %s* %%%s", paramLLVM, valReg, paramLLVM, param.Value)
				}
			}

			bodyVal := b.emitBlock(arm.Body)
			if bodyVal != "<terminated>" {
				if llvmType != "void" && bodyVal != "" {
					b.EmitLine("  store %s %s, %s* %s", llvmType, bodyVal, llvmType, resultPtr)
				}
				b.EmitLine("  br label %%%s", mergeLbl)
			}
		}

		b.EmitLine("\n%s:", defaultLbl)
		b.EmitLine("  br label %%%s", mergeLbl)

		b.EmitLine("\n%s:", mergeLbl)
		if llvmType != "void" {
			valReg := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", valReg, llvmType, llvmType, resultPtr)
			return valReg
		}
		return ""

	default:
		// Gracefully skip unimplemented AST nodes (like Arrays, Loops, Match)
		// so the compiler doesn't panic while we test!
		b.EmitLine("  ; TODO: codegen for %T", n)
		return ""
	}

	return ""
}
