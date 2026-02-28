package sema

import (
	"fmt"
	"synovium/ast"
	"synovium/lexer"
)

type Evaluator struct {
	Pool               *TypePool
	Errors             []string
	CachedPrimitives   map[string]TypeID
	LoopDepth          int
	ExpectedReturnType TypeID
	ExpectedYieldType  TypeID
}

func NewEvaluator(pool *TypePool) *Evaluator {
	return &Evaluator{
		Pool:             pool,
		Errors:           make([]string, 0),
		CachedPrimitives: make(map[string]TypeID),
	}
}

func (e *Evaluator) error(span lexer.Span, msg string) TypeID {
	e.Errors = append(e.Errors, fmt.Sprintf("Error at bytes %d-%d: %s", span.Start, span.End, msg))
	return 0
}

func (e *Evaluator) Evaluate(node ast.Node, scope *Scope) TypeID {
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
		return e.CachedPrimitives["chr"] // ADDED!

	// --- 2. IDENTIFIERS & VARIABLES ---
	case *ast.Identifier:
		sym, exists := scope.Resolve(n.Value)
		if !exists {
			return e.error(n.Span(), fmt.Sprintf("undeclared identifier '%s'", n.Value))
		}
		if !sym.IsResolved {
			return e.error(n.Span(), fmt.Sprintf("identifier '%s' is trapped in a comptime cycle or unresolved", n.Value))
		}
		return sym.TypeID

	case *ast.VariableDecl:
		rhsType := e.Evaluate(n.Value, scope)
		if rhsType == 0 {
			return 0
		}

		lhsType := e.resolveTypeSignature(n.Type, scope)
		if lhsType != 0 && !e.typesMatch(lhsType, rhsType) {
			return e.error(n.Span(), "type mismatch in variable declaration")
		}

		isMut := n.Operator == "~="

		// THE FIX: Check if the DAG hoisted this as a global variable.
		// If it exists in the EXACT current scope and is unresolved, patch it!
		if sym, exists := scope.Symbols[n.Name.Value]; exists && !sym.IsResolved {
			sym.TypeID = rhsType
			sym.IsMutable = isMut
			sym.IsResolved = true
		} else {
			// Otherwise, it's a normal local variable (e.g., inside a function)
			_, err := scope.Define(n.Name.Value, rhsType, isMut, n)
			if err != nil {
				return e.error(n.Span(), err.Error())
			}
		}
		return rhsType

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
	}
	return e.error(node.Span(), fmt.Sprintf("unsupported AST node for evaluation: %T", node))
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
		if ident, ok := node.Left.(*ast.Identifier); ok {
			sym, _ := scope.Resolve(ident.Value)
			if sym != nil && !sym.IsMutable {
				return e.error(ident.Span(), fmt.Sprintf("cannot mutate immutable variable '%s'", ident.Value))
			}
		} else {
			return e.error(node.Left.Span(), "invalid assignment target")
		}
		if node.Operator == "=" && !e.typesMatch(leftID, rightID) {
			return e.error(node.Span(), "type mismatch in assignment")
		}
	}

	// --- RANGE OPERATOR (0...10) ---
	if node.Operator == "..." {
		if (e.Pool.Types[leftID].Mask&MaskIsNumeric) == 0 || (e.Pool.Types[rightID].Mask&MaskIsNumeric) == 0 {
			return e.error(node.Span(), "range bounds must be numeric")
		}
		return leftID
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

func (e *Evaluator) routeToDispatchTable(op string, left, right TypeID, span lexer.Span) TypeID {
	return e.error(span, "operator overloading / dispatch tables not yet implemented")
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
