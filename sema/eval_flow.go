package sema

import (
	"fmt"
	"synovium/ast"
)

// evaluateBlock pushes a new scope, executes all statements, and bubbles the final expression.
func (e *Evaluator) evaluateBlock(block *ast.Block, parentScope *Scope) TypeID {
	// 1. Push a fresh lexical scope for this block
	innerScope := NewScope(parentScope)

	// 2. Evaluate all statements
	for _, stmt := range block.Statements {
		e.Evaluate(stmt, innerScope)

		// If an error occurred deep inside, halt execution to prevent cascading failures
		if len(e.Errors) > 0 {
			return 0
		}
	}

	// 3. Synovium Bubbling: If there is a trailing expression without a semicolon, return its type
	if block.Value != nil {
		return e.Evaluate(block.Value, innerScope)
	}

	// 4. Otherwise, it evaluates to void (0)
	return 0
}

// evaluateIf verifies all branches resolve to the exact same TypeID.
func (e *Evaluator) evaluateIf(node *ast.IfExpr, scope *Scope) TypeID {
	// 1. Validate the main condition is a boolean
	condType := e.Evaluate(node.Condition, scope)
	if condType != e.CachedPrimitives["bln"] {
		return e.error(node.Condition.Span(), "if condition must evaluate to a boolean")
	}

	// 2. Evaluate the main body
	baseType := e.evaluateBlock(node.Body, scope)

	// 3. Validate all Elif branches match the base type
	for i, elifCond := range node.ElifConds {
		elifCondType := e.Evaluate(elifCond, scope)
		if elifCondType != e.CachedPrimitives["bln"] {
			return e.error(elifCond.Span(), "elif condition must evaluate to a boolean")
		}

		elifType := e.evaluateBlock(node.ElifBodies[i], scope)
		if elifType != baseType {
			return e.error(node.ElifBodies[i].Span(), "elif branch type does not match the if branch type")
		}
	}

	// 4. Validate the Else branch
	if node.ElseBody != nil {
		elseType := e.evaluateBlock(node.ElseBody, scope)
		if elseType != baseType {
			return e.error(node.ElseBody.Span(), "else branch type does not match the if branch type")
		}
	} else if baseType != 0 {
		// A crucial catch: If an 'if' expression returns a value (e.g., `x := if true { 5 }`),
		// it MUST have an 'else' block. Otherwise, what happens if the condition is false?
		return e.error(node.Span(), "if expression returning a value must have an exhaustive else branch")
	}

	return baseType
}

// evaluateLoop tracks loop depth for valid breaks and checks the bubbled yield types.
func (e *Evaluator) evaluateLoop(node *ast.LoopExpr, scope *Scope) TypeID {
	// Create an inner scope specifically for the loop condition (e.g., `i : i32 = 0...10`)
	loopScope := NewScope(scope)

	if node.Condition != nil {
		// If it's a variable declaration (the iterator), evaluate it
		e.Evaluate(node.Condition, loopScope)
	}

	// Increment loop depth so `brk` and `yld` statements know they are legally allowed
	e.LoopDepth++
	defer func() { e.LoopDepth-- }()

	// Currently, our yield type starts as unknown (0). The first `yld` encountered will set it.
	// We reset this for nested loops.
	prevYieldType := e.ExpectedYieldType
	e.ExpectedYieldType = 0
	defer func() { e.ExpectedYieldType = prevYieldType }()

	// Evaluate the loop body
	blockType := e.evaluateBlock(node.Body, loopScope)

	// If the loop utilizes `yld` statements, it returns an Array of the yielded type.
	// For now, we will just return the base yield type, but later this will construct an ArrayType in the TypePool.
	if e.ExpectedYieldType != 0 {
		// TODO: Wrap ExpectedYieldType into an `ast.ArrayType` in the TypePool
		return e.ExpectedYieldType
	}

	return blockType
}

// --- STATEMENT EVALUATORS ---

func (e *Evaluator) evaluateExprStmt(node *ast.ExprStmt, scope *Scope) TypeID {
	e.Evaluate(node.Value, scope)
	// Statements never bubble values. Only trailing Block.Values do.
	return 0
}

func (e *Evaluator) evaluateBreak(node *ast.BreakStmt) TypeID {
	if e.LoopDepth == 0 {
		return e.error(node.Span(), "illegal 'brk' statement outside of a loop")
	}
	return 0
}

func (e *Evaluator) evaluateYield(node *ast.YieldStmt, scope *Scope) TypeID {
	if e.LoopDepth == 0 {
		return e.error(node.Span(), "illegal 'yld' statement outside of a loop")
	}

	yldType := e.Evaluate(node.Value, scope)

	if e.ExpectedYieldType == 0 {
		e.ExpectedYieldType = yldType // First yield locks in the type
	} else if e.ExpectedYieldType != yldType {
		return e.error(node.Span(), fmt.Sprintf("yielded type does not match previously yielded type in loop"))
	}

	return 0
}

func (e *Evaluator) evaluateReturn(node *ast.ReturnStmt, scope *Scope) TypeID {
	retType := e.CachedPrimitives["void"] // Default to void if no value is returned
	if node.Value != nil {
		retType = e.Evaluate(node.Value, scope)
	}

	if e.ExpectedReturnType != 0 && retType != e.ExpectedReturnType {
		return e.error(node.Span(), "return type does not match function signature")
	}

	return 0 // The return statement itself doesn't bubble locally; it breaks control flow
}
