package sema

import (
	"fmt"
	"strings"

	"synovium/ast"
	"synovium/lexer"
)

type Evaluator struct {
	Pool               *TypePool
	Errors             []string
	CachedPrimitives   map[string]TypeID
	ComptimeCache      map[string]TypeID
	LoopDepth          int
	ExpectedReturnType TypeID
	ExpectedYieldType  TypeID
	SourceCode         string
	GlobalDecls        []ast.Decl
	JITCallback        func(expr ast.Expr, expectedType TypeID, pool *TypePool, envScope *Scope, globalDecls []ast.Decl) ([]byte, error)
}

func NewEvaluator(pool *TypePool, sourceCode string) *Evaluator {
	return &Evaluator{
		Pool:             pool,
		Errors:           make([]string, 0),
		CachedPrimitives: make(map[string]TypeID),
		ComptimeCache:    make(map[string]TypeID),
		SourceCode:       sourceCode,
	}
}

// The new Rust-style error formatter!
func (e *Evaluator) error(span lexer.Span, msg string) TypeID {
	lines := strings.Split(e.SourceCode, "\n")

	lineIdx := 0
	colIdx := 0
	errLineIdx := 0
	errColStart := 0
	errColEnd := 0

	// 1. Map the byte span to exact line and column numbers
	for i := 0; i <= len(e.SourceCode); i++ {
		if i == span.Start {
			errLineIdx = lineIdx
			errColStart = colIdx
		}
		if i == span.End {
			errColEnd = colIdx
			// If the error spans multiple lines, cap the squiggly at the end of the first line
			if lineIdx != errLineIdx {
				errColEnd = len(lines[errLineIdx])
			}
			break
		}
		if i < len(e.SourceCode) {
			if e.SourceCode[i] == '\n' {
				lineIdx++
				colIdx = 0
			} else {
				colIdx++
			}
		}
	}

	// 2. Build the visual snippet
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\033[31mError\033[0m: %s\n", msg))
	sb.WriteString(fmt.Sprintf("  --> line %d:%d\n", errLineIdx+1, errColStart+1))

	// Print Previous Line (Context)
	if errLineIdx > 0 {
		sb.WriteString(fmt.Sprintf("%4d | %s\n", errLineIdx, lines[errLineIdx-1]))
	}

	// Print Error Line
	errLine := ""
	if errLineIdx < len(lines) {
		errLine = lines[errLineIdx]
		sb.WriteString(fmt.Sprintf("%4d | %s\n", errLineIdx+1, errLine))
	}

	// Print Squiggly Line (^^^)
	sb.WriteString("     | ")
	// Preserve tabs vs spaces so the squiggly aligns perfectly!
	for i := 0; i < errColStart && i < len(errLine); i++ {
		if errLine[i] == '\t' {
			sb.WriteString("\t")
		} else {
			sb.WriteString(" ")
		}
	}

	squigglyLen := errColEnd - errColStart
	if squigglyLen <= 0 {
		squigglyLen = 1
	}

	sb.WriteString("\033[31;1m") // Bold Red
	for i := 0; i < squigglyLen; i++ {
		sb.WriteString("^")
	}
	sb.WriteString("\033[0m\n") // Reset

	// Print Next Line (Context)
	if errLineIdx < len(lines)-1 {
		sb.WriteString(fmt.Sprintf("%4d | %s\n", errLineIdx+2, lines[errLineIdx+1]))
	}

	e.Errors = append(e.Errors, sb.String())
	return 0
}

func (e *Evaluator) Evaluate(node ast.Node, scope *Scope) TypeID {
	if node == nil {
		return 0
	}

	// 1. Calculate the actual type using our existing robust logic
	result := e.evaluateInternal(node, scope)

	// 2. Stamp the physical AST node in the TAST side-tables
	if result != 0 {
		e.Pool.NodeTypes[node] = result
		e.Pool.NodeScopes[node] = scope
	}

	return result
}

func (e *Evaluator) evaluateInternal(node ast.Node, scope *Scope) TypeID {
	if node == nil {
		return 0
	}

	switch n := node.(type) {
	// --- 1. LITERALS ---
	case *ast.IntLiteral:
		return e.CachedPrimitives["i32"]
	case *ast.FloatLiteral:
		return e.CachedPrimitives["f64"]
	case *ast.BoolLiteral:
		return e.CachedPrimitives["bln"]
	case *ast.StringLiteral:
		return e.CachedPrimitives["str"]
	case *ast.CharLiteral:
		return e.CachedPrimitives["chr"]

	// --- 2. IDENTIFIERS & VARIABLES ---
	case *ast.Identifier:
		return e.evaluateIdentifier(n, scope)

	case *ast.VariableDecl:
		return e.evaluateVariableDecl(n, scope)

	// --- 3. INFIX MATH & ASSIGNMENT ---
	case *ast.InfixExpr:
		return e.evaluateInfix(n, scope)

	// --- 4. CONTROL FLOW ---
	case *ast.Block:
		return e.evaluateBlock(n, scope)
	case *ast.IfExpr:
		return e.evaluateIf(n, scope)
	case *ast.LoopExpr:
		return e.evaluateLoop(n, scope)

	// --- 5. STATEMENTS ---
	case *ast.ExprStmt:
		return e.evaluateExprStmt(n, scope)
	case *ast.BreakStmt:
		return e.evaluateBreak(n)
	case *ast.YieldStmt:
		return e.evaluateYield(n, scope)
	case *ast.ReturnStmt:
		return e.evaluateReturn(n, scope)

	// --- 6. STRUCTS ---
	case *ast.StructDecl:
		return e.evaluateStructDecl(n, scope)
	case *ast.StructInitExpr:
		return e.evaluateStructInit(n, scope)
	case *ast.FieldAccessExpr:
		return e.evaluateFieldAccess(n, scope)

	// --- 7. FUNCTIONS & CALLS ---
	case *ast.FunctionDecl:
		return e.evaluateFunctionDecl(n, scope)
	case *ast.CallExpr:
		return e.evaluateCallExpr(n, scope)

	// --- 8. EXPRESSIONS & OPERATORS ---
	case *ast.PrefixExpr:
		return e.evaluatePrefix(n, scope)
	case *ast.IndexExpr:
		return e.evaluateIndexExpr(n, scope)
	case *ast.CastExpr:
		return e.evaluateCastExpr(n, scope)
	case *ast.BubbleExpr:
		return e.evaluateBubbleExpr(n, scope)

	// --- 9. ENUMS, IMPLS, & MATCH ---
	case *ast.EnumDecl:
		return e.evaluateEnumDecl(n, scope)
	case *ast.ImplDecl:
		return e.evaluateImplDecl(n, scope)
	case *ast.MatchExpr:
		return e.evaluateMatchExpr(n, scope)
	case *ast.ArrayInitExpr:
		return e.evaluateArrayInitExpr(n, scope)
	}
	return e.error(node.Span(), fmt.Sprintf("unsupported AST node for evaluation: %T", node))
}

func (e *Evaluator) evaluateIdentifier(node *ast.Identifier, scope *Scope) TypeID {
	sym, exists := scope.Resolve(node.Value)
	if !exists {
		return e.error(node.Span(), fmt.Sprintf("undeclared identifier '%s'", node.Value))
	}
	if !sym.IsResolved {
		return e.error(node.Span(), fmt.Sprintf("identifier '%s' is trapped in a comptime cycle or unresolved", node.Value))
	}
	return sym.TypeID
}

func (e *Evaluator) evaluateVariableDecl(node *ast.VariableDecl, scope *Scope) TypeID {
	rhsType := e.Evaluate(node.Value, scope)
	if rhsType == 0 {
		return 0
	}

	lhsType := e.resolveTypeSignature(node.Type, scope)
	if lhsType != 0 && !e.typesMatch(lhsType, rhsType) {
		return e.error(node.Span(), "type mismatch in variable declaration")
	}

	isMut := node.Operator == "~="
	var comptimeData []byte

	// --- COMPTIME JIT INTERCEPTOR ---
	if node.Operator == ":=" {
		if e.JITCallback == nil {
			return e.error(node.Span(), "comptime JIT engine is not initialized")
		}

		data, err := e.JITCallback(node.Value, lhsType, e.Pool, scope, e.GlobalDecls)
		if err != nil {
			return e.error(node.Span(), err.Error()) // Pass the raw debug dump up!
		}
		comptimeData = data

		blob := &ast.ComptimeBlob{Token: node.Token, Type: int(lhsType), Data: data}
		node.Value = blob
		e.Pool.NodeTypes[node.Value] = lhsType
		rhsType = lhsType
	}

	// The DAG variable patching
	var sym *Symbol
	if existing, exists := scope.Symbols[node.Name.Value]; exists && !existing.IsResolved {
		existing.TypeID = rhsType
		existing.IsMutable = isMut
		existing.IsResolved = true
		sym = existing
	} else {
		sym, _ = scope.Define(node.Name.Value, rhsType, isMut, node)
	}

	// THE CRITICAL FIX: Lock the memory into the Environment!
	if node.Operator == ":=" {
		sym.ComptimeData = comptimeData
	}

	return rhsType
}

func (e *Evaluator) evaluateInfix(node *ast.InfixExpr, scope *Scope) TypeID {
	leftID := e.Evaluate(node.Left, scope)
	rightID := e.Evaluate(node.Right, scope)

	if leftID == 0 || rightID == 0 {
		return 0
	}

	// --- ASSIGNMENT & MUTABILITY CHECK ---
	isAssignment := node.Operator == "=" || node.Operator == "~=" || node.Operator == "+=" || node.Operator == "-=" || node.Operator == "*=" || node.Operator == "/=" || node.Operator == "%="
	if isAssignment {
		switch leftNode := node.Left.(type) {
		case *ast.Identifier:
			sym, _ := scope.Resolve(leftNode.Value)
			if sym != nil && !sym.IsMutable {
				return e.error(leftNode.Span(), fmt.Sprintf("cannot mutate immutable variable '%s'", leftNode.Value))
			}
		case *ast.FieldAccessExpr, *ast.IndexExpr:
			// Valid L-values
		case *ast.PrefixExpr:
			if leftNode.Operator != "*" {
				return e.error(leftNode.Span(), "invalid prefix assignment target")
			}
		default:
			return e.error(node.Left.Span(), "invalid assignment target")
		}

		if (node.Operator == "=" || node.Operator == "~=") && !e.typesMatch(leftID, rightID) {
			return e.error(node.Span(), "type mismatch in assignment")
		}

		// This prevents them from accidentally bubbling out of blocks!
		return e.CachedPrimitives["void"]
	}

	// --- RANGE OPERATOR (0...10) ---
	if node.Operator == "..." {
		if (e.Pool.Types[leftID].Mask&MaskIsNumeric) == 0 || (e.Pool.Types[rightID].Mask&MaskIsNumeric) == 0 {
			return e.error(node.Span(), "range bounds must be numeric")
		}
		return leftID
	}

	leftType := e.Pool.Types[leftID]
	rightType := e.Pool.Types[rightID]
	if !leftType.IsFundamental || !rightType.IsFundamental {
		return e.routeToDispatchTable(node.Operator, leftID, rightID, node.Span())
	}

	// --- RELATIONAL & LOGICAL (ADDED: Strictly returns `bln`) ---
	isRelational := node.Operator == "==" || node.Operator == "!=" || node.Operator == "<" || node.Operator == "<=" || node.Operator == ">" || node.Operator == ">="
	isLogical := node.Operator == "&&" || node.Operator == "||"
	if isRelational || isLogical {
		return e.CachedPrimitives["bln"]
	}

	// --- ARITHMETIC PRIMITIVE PROMOTION (ADDED %, |, &, ^, <<, >>) ---
	isMath := node.Operator == "+" || node.Operator == "-" || node.Operator == "*" || node.Operator == "/" || node.Operator == "%" || node.Operator == "|" || node.Operator == "&" || node.Operator == "^" || node.Operator == "<<" || node.Operator == ">>"

	if isMath {
		leftType := e.Pool.Types[leftID]
		rightType := e.Pool.Types[rightID]

		if (leftType.Mask&MaskIsNumeric) == 0 || (rightType.Mask&MaskIsNumeric) == 0 {
			return e.routeToDispatchTable(node.Operator, leftID, rightID, node.Span())
		}

		promotedMask := PromoteNumeric(leftType.Mask, rightType.Mask)
		for _, t := range e.Pool.Types {
			if t.Mask == promotedMask && t.IsFundamental {
				return t.ID
			}
		}
	}

	return leftID
}

func (e *Evaluator) routeToDispatchTable(operator string, leftID, rightID TypeID, span lexer.Span) TypeID {
	leftType := e.Pool.Types[leftID]
	actualLeft := leftType
	if (actualLeft.Mask & MaskIsPointer) != 0 {
		actualLeft = e.Pool.Types[actualLeft.BaseType]
	}

	methodID, hasMethod := actualLeft.Methods[operator]
	if !hasMethod {
		return e.error(span, fmt.Sprintf("type '%s' does not implement the '%s' operator", actualLeft.Name, operator))
	}

	methodSignature := e.Pool.Types[methodID]
	if len(methodSignature.FuncParams) != 2 {
		return e.error(span, fmt.Sprintf("operator overload '%s' must take exactly 1 argument (besides self)", operator))
	}

	// We allow Codegen to handle the ptr/value coercions, so we just check base types!
	expectedRightID := methodSignature.FuncParams[1]
	actualExpected := e.Pool.Types[expectedRightID]
	if (actualExpected.Mask & MaskIsPointer) != 0 {
		actualExpected = e.Pool.Types[actualExpected.BaseType]
	}

	actualProvided := e.Pool.Types[rightID]
	if (actualProvided.Mask & MaskIsPointer) != 0 {
		actualProvided = e.Pool.Types[actualProvided.BaseType]
	}

	if actualExpected.ID != actualProvided.ID && expectedRightID != rightID {
		return e.error(span, fmt.Sprintf("type mismatch in operator overload: expects %s, got %s", e.Pool.Types[expectedRightID].Name, e.Pool.Types[rightID].Name))
	}

	return methodSignature.FuncReturn
}

func (e *Evaluator) typesMatch(expected, actual TypeID) bool {
	if expected == actual {
		return true
	}

	expType := e.Pool.Types[expected]
	actType := e.Pool.Types[actual]

	if (expType.Mask&MaskIsFunction) != 0 && (actType.Mask&MaskIsFunction) != 0 {
		if len(expType.FuncParams) != len(actType.FuncParams) {
			return false
		}
		for i, p := range expType.FuncParams {
			if p != actType.FuncParams[i] {
				return false
			}
		}
		return expType.FuncReturn == actType.FuncReturn
	}

	return false
}
