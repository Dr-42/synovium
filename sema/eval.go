package sema

import (
	"fmt"
	"synovium/ast"
	"synovium/lexer"
)

// Evaluator is the Abstract Interpreter that type-checks and executes comptime logic.
type Evaluator struct {
	Pool   *TypePool
	Errors []string

	// Pre-cached primitives for fast lookup during AST walking
	CachedPrimitives map[string]TypeID

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
	return 0 // 0 acts as our 'Error/Void' TypeID
}

// Evaluate is the master switch statement. It recursively type-executes the AST.
func (e *Evaluator) Evaluate(node ast.Node, scope *Scope) TypeID {
	if node == nil {
		return 0
	}

	switch n := node.(type) {

	// --- 1. LITERALS ---
	case *ast.IntLiteral:
		// Default to i32 for integer literals unless constrained otherwise
		return e.CachedPrimitives["i32"]
	case *ast.FloatLiteral:
		return e.CachedPrimitives["f64"]
	case *ast.BoolLiteral:
		return e.CachedPrimitives["bln"]
	case *ast.StringLiteral:
		return e.CachedPrimitives["str"]

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
		// 1. Evaluate the right-hand side to get the concrete layout
		rhsType := e.Evaluate(n.Value, scope)
		if rhsType == 0 {
			return 0
		} // Cascade error

		// 2. Resolve the left-hand explicit type signature
		// (Assuming we have a helper to parse `ast.Type` into a `TypeID`)
		lhsType := e.resolveTypeSignature(n.Type, scope)

		// 3. Type Check!
		if lhsType != 0 && lhsType != rhsType {
			// In the future, we can check for valid implicit casts here
			return e.error(n.Span(), "type mismatch in variable declaration")
		}

		// 4. Register in Scope
		isMut := n.Operator == "~="
		_, err := scope.Define(n.Name.Value, rhsType, isMut, n)
		if err != nil {
			return e.error(n.Span(), err.Error())
		}

		// Declarations technically evaluate to Void/0, but returning the type helps testing
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
	}

	return e.error(node.Span(), fmt.Sprintf("unsupported AST node for evaluation: %T", node))
}

// evaluateInfix handles binary operators, primitive promotion, and mutability validation.
func (e *Evaluator) evaluateInfix(node *ast.InfixExpr, scope *Scope) TypeID {
	leftID := e.Evaluate(node.Left, scope)
	rightID := e.Evaluate(node.Right, scope)

	if leftID == 0 || rightID == 0 {
		return 0 // Cascade error
	}

	// --- ASSIGNMENT & MUTABILITY CHECK (~=, =, +=) ---
	isAssignment := node.Operator == "=" || node.Operator == "~=" ||
		node.Operator == "+=" || node.Operator == "-="

	if isAssignment {
		// Ensure the left side is an identifier (or field access) that was declared mutable
		if ident, ok := node.Left.(*ast.Identifier); ok {
			sym, _ := scope.Resolve(ident.Value)
			if sym != nil && !sym.IsMutable {
				return e.error(ident.Span(), fmt.Sprintf("cannot mutate immutable variable '%s'", ident.Value))
			}
		} else {
			return e.error(node.Left.Span(), "invalid assignment target")
		}

		// If it's a strict reassignment, types must match
		if node.Operator == "=" && leftID != rightID {
			return e.error(node.Span(), "type mismatch in assignment")
		}
	}

	// --- ARITHMETIC PRIMITIVE PROMOTION ---
	isMath := node.Operator == "+" || node.Operator == "-" ||
		node.Operator == "*" || node.Operator == "/"

	if isMath {
		leftType := e.Pool.Types[leftID]
		rightType := e.Pool.Types[rightID]

		// 1. Are they both hardware math primitives?
		if (leftType.Mask&MaskIsNumeric) == 0 || (rightType.Mask&MaskIsNumeric) == 0 {
			// If not, we route to the 2D Dispatch Table for operator overloading
			return e.routeToDispatchTable(node.Operator, leftID, rightID, node.Span())
		}

		// 2. Hardware Promotion using our high-speed bitwise engine
		promotedMask := PromoteNumeric(leftType.Mask, rightType.Mask)

		// Find the TypeID in the pool that matches this exact promoted primitive mask
		for _, t := range e.Pool.Types {
			if t.Mask == promotedMask && t.IsFundamental {
				return t.ID
			}
		}
	}

	return leftID // Fallback for logical operators (&&, ||) which will eventually return `bln`
}

func (e *Evaluator) routeToDispatchTable(op string, left, right TypeID, span lexer.Span) TypeID {
	return e.error(span, "operator overloading / dispatch tables not yet implemented")
}
