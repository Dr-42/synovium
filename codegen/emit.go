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

		// --- NEW: Hard Return Statements ---
		case *ast.ReturnStmt:
			if n.Value != nil {
				valReg := b.emitExpression(n.Value)
				llvmType := b.GetLLVMType(b.Pool.NodeTypes[n.Value])
				b.EmitLine("  ret %s %s", llvmType, valReg)
			} else {
				b.EmitLine("  ret void")
			}
			// LLVM blocks end at 'ret', so any remaining statements in this block are dead code.
			return ""
		case *ast.BreakStmt:
			if len(b.LoopExits) > 0 {
				b.EmitLine("  br label %%%s", b.LoopExits[len(b.LoopExits)-1])
			}
			return ""
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

			// 1. Is it a Method Call or an Enum Constructor?
			if _, isMethod := actualObjType.Methods[fa.Field.Value]; isMethod {
				argOffset = 1
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
			} else {
				// It's an Enum Variant Constructor! (e.g. Action.Spawn)
				funcName = actualObjType.Name + "::" + fa.Field.Value
			}
		}

		funcTypeID := b.Pool.NodeTypes[n.Function]
		funcType := b.Pool.Types[funcTypeID]
		retLLVM := b.GetLLVMType(funcType.FuncReturn)

		if strings.Contains(funcType.Name, "_inst_") {
			funcName = funcType.Name
		}

		// 2. THE VARIADIC PANIC FIX: Bounds-check the parameters array!
		for i, arg := range n.Arguments {
			paramIndex := i + argOffset

			if paramIndex < len(funcType.FuncParams) {
				expectedParamTypeID := funcType.FuncParams[paramIndex]
				// Skip compile-time ghost arguments in the function call!
				if b.Pool.Types[expectedParamTypeID].Name == "type" {
					continue
				}
			}

			valReg := b.emitExpression(arg)
			argLLVM := b.GetLLVMType(b.Pool.NodeTypes[arg])
			args = append(args, fmt.Sprintf("%s %s", argLLVM, valReg))
		}

		// 3. INLINE ENUM CONSTRUCTORS: Do not call missing LLVM functions!
		if strings.Contains(funcName, "::") {
			enumReg := b.NextReg()
			b.EmitLine("  %s = alloca %s", enumReg, retLLVM)

			// Extract the tag pointer and write '0' to satisfy the MatchExpr switch
			tagPtr := b.NextReg()
			b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 0", tagPtr, retLLVM, retLLVM, enumReg)
			b.EmitLine("  store i8 0, i8* %s", tagPtr)

			// Return the forged Enum struct
			valReg := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", valReg, retLLVM, retLLVM, enumReg)
			return valReg
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

		// 1. Allocate the array buffer on the stack
		arrReg := b.NextReg()
		b.EmitLine("  %s = alloca %s", arrReg, llvmType)

		elemTypeID := b.Pool.Types[typeID].BaseType
		elemLLVM := b.GetLLVMType(elemTypeID)

		// 2. Populate the elements
		for i, el := range n.Elements {
			valReg := b.emitExpression(el)
			ptrReg := b.NextReg()
			b.EmitLine("  %s = getelementptr inbounds %s, %s* %s, i32 0, i32 %d", ptrReg, llvmType, llvmType, arrReg, i)
			b.EmitLine("  store %s %s, %s* %s", elemLLVM, valReg, elemLLVM, ptrReg)
		}

		// 3. Return the loaded array
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
			if llvmType != "void" && bodyVal != "" {
				b.EmitLine("  store %s %s, %s* %s", llvmType, bodyVal, llvmType, resultPtr)
			}
			b.EmitLine("  br label %%%s", mergeLbl)
		}

		// The final fallback (Else branch)
		b.EmitLine("\n%s:", nextCondLbl)
		if n.ElseBody != nil {
			elseVal := b.emitBlock(n.ElseBody)
			if llvmType != "void" && elseVal != "" {
				b.EmitLine("  store %s %s, %s* %s", llvmType, elseVal, llvmType, resultPtr)
			}
		}
		b.EmitLine("  br label %%%s", mergeLbl)

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

		// 1. Intercept `i = 0...3` and initialize `i = 0`
		if n.Condition != nil {
			if vDecl, ok := n.Condition.(*ast.VariableDecl); ok {
				if inf, ok := vDecl.Value.(*ast.InfixExpr); ok && inf.Operator == "..." {
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

		// 2. Loop Bounds Check (`i < 3`)
		if loopVar != "" {
			currVal := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", currVal, loopLLVM, loopLLVM, loopVar)
			cmpReg := b.NextReg()
			b.EmitLine("  %s = icmp slt %s %s, %s", cmpReg, loopLLVM, currVal, loopEndReg)
			b.EmitLine("  br i1 %s, label %%%s, label %%%s", cmpReg, bodyLbl, exitLbl)
		} else {
			b.EmitLine("  br label %%%s", bodyLbl)
		}

		b.EmitLine("\n%s:", bodyLbl)
		b.emitBlock(n.Body)

		// 3. Loop Increment (`i++`)
		if loopVar != "" {
			currVal := b.NextReg()
			b.EmitLine("  %s = load %s, %s* %s", currVal, loopLLVM, loopLLVM, loopVar)
			nextVal := b.NextReg()
			b.EmitLine("  %s = add %s %s, 1", nextVal, loopLLVM, currVal)
			b.EmitLine("  store %s %s, %s* %s", loopLLVM, nextVal, loopLLVM, loopVar)
		}

		b.EmitLine("  br label %%%s", condLbl) // Loop back
		b.EmitLine("\n%s:", exitLbl)           // Break target

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

		// 1. Evaluate the Enum Value
		targetReg := b.emitExpression(n.Value)
		targetLLVM := b.GetLLVMType(b.Pool.NodeTypes[n.Value])

		// 2. Extract the i8 Tag!
		tagReg := b.NextReg()
		b.EmitLine("  %s = extractvalue %s %s, 0", tagReg, targetLLVM, targetReg)

		mergeLbl := b.NextLabel()
		defaultLbl := b.NextLabel()

		// 3. Emit the LLVM Switch statement
		b.EmitLine("  switch i8 %s, label %%%s [", tagReg, defaultLbl)
		armLabels := make([]string, len(n.Arms))
		for i := range n.Arms {
			armLabels[i] = b.NextLabel()
			b.EmitLine("    i8 %d, label %%%s", i, armLabels[i]) // Tag variants correspond to index
		}
		b.EmitLine("  ]")

		// 4. Emit the Arms
		for i, arm := range n.Arms {
			b.EmitLine("\n%s:", armLabels[i])

			// THE FIX: arm.Pattern is already an *ast.Identifier! No type assertion needed.
			variantNameStr := arm.Pattern.Value

			parts := strings.Split(variantNameStr, ".")
			vName := parts[len(parts)-1]

			enumType := b.Pool.Types[b.Pool.NodeTypes[n.Value]]
			payloadTypes := enumType.Variants[vName]

			// Allocate the EXACT type from the Enum Variant layout!
			for j, param := range arm.Params {
				paramLLVM := b.GetLLVMType(payloadTypes[j])
				b.EmitLine("  %%%s = alloca %s", param.Value, paramLLVM)
				b.Locals[param.Value] = "%" + param.Value
			}

			bodyVal := b.emitBlock(arm.Body)
			if llvmType != "void" && bodyVal != "" {
				b.EmitLine("  store %s %s, %s* %s", llvmType, bodyVal, llvmType, resultPtr)
			}
			b.EmitLine("  br label %%%s", mergeLbl)
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
