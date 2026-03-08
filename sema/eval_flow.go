package sema

import (
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
	} else {
		// baseType == 0 means an error already happened inside, so we don't cascade another error.
		if baseType != 0 && baseType != e.CachedPrimitives["void"] {
			return e.error(node.Span(), "if expression returning a value must have an exhaustive else branch")
		}
	}

	return baseType
}

// evaluateLoop tracks loop depth for valid breaks and checks the bubbled yield types.
func (e *Evaluator) evaluateLoop(node *ast.LoopExpr, scope *Scope) TypeID {
	loopScope := NewScope(scope)

	if node.Condition != nil {
		e.Evaluate(node.Condition, loopScope)
	}

	e.LoopDepth++
	defer func() { e.LoopDepth-- }()

	// Save the parent loop's expected break type
	parentYield := e.ExpectedYieldType
	e.ExpectedYieldType = 0

	blockType := e.evaluateBlock(node.Body, loopScope)

	// ACT 2 FIX: A broken loop evaluates directly to the bubbled value's type!
	if e.ExpectedYieldType != 0 {
		blockType = e.ExpectedYieldType
	}

	// NESTED BUBBLING: If this is an unnamed inner loop, it assumes a labeled break
	// is passing THROUGH it to an outer loop. We propagate the type up the chain!
	if node.Label == nil && e.ExpectedYieldType != 0 {
		parentYield = e.ExpectedYieldType
	}

	// Restore context
	e.ExpectedYieldType = parentYield

	return blockType
}

// --- STATEMENT EVALUATORS ---

func (e *Evaluator) evaluateExprStmt(node *ast.ExprStmt, scope *Scope) TypeID {
	e.Evaluate(node.Value, scope)
	// Statements never bubble values. Only trailing Block.Values do.
	return 0
}

// --- UPGRADED: Break Value Bubbling ---
func (e *Evaluator) evaluateBreak(node *ast.BreakStmt, scope *Scope) TypeID {
	if e.LoopDepth == 0 {
		return e.error(node.Span(), "illegal 'brk' statement outside of a loop")
	}

	if node.Value != nil {
		brkType := e.Evaluate(node.Value, scope)

		if e.ExpectedYieldType == 0 {
			e.ExpectedYieldType = brkType // Lock in the break type for this loop chain
		} else if e.ExpectedYieldType != brkType {
			return e.error(node.Span(), "break value type does not match previously broken type in loop")
		}
	}

	return 0
}

// --- NEW: Defer Evaluation (Replaces evaluateYield) ---
func (e *Evaluator) evaluateDefer(node *ast.DeferStmt, scope *Scope) TypeID {
	// A defer statement does not yield a value to the current block.
	// We simply evaluate its body to ensure it is structurally and mathematically sound.
	e.Evaluate(node.Body, scope)
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
