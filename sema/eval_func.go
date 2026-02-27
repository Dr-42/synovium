package sema

import (
	"fmt"
	"synovium/ast"
)

// evaluateFunctionDecl registers a function's signature and handles Generic Template forging.
func (e *Evaluator) evaluateFunctionDecl(node *ast.FunctionDecl, scope *Scope) TypeID {
	paramTypes := make([]TypeID, len(node.Parameters))
	isGeneric := false

	// A dedicated scope for the signature so `val: T` can find `T`
	sigScope := NewScope(scope)

	for i, param := range node.Parameters {
		pType := e.resolveTypeSignature(param.Type, sigScope)
		if pType == 0 {
			return 0
		}

		if pType == e.CachedPrimitives["type"] {
			isGeneric = true

			// 1. Forge a Template Placeholder for the generic variable
			templateID := TypeID(len(e.Pool.Types))
			e.Pool.Types = append(e.Pool.Types, UniversalType{
				ID:   templateID,
				Name: param.Name.Value + "_template",
				Mask: 0, // A template has no boolean behaviors until instantiated
			})

			// 2. Bind `T` to this new template ID
			sigScope.Define(param.Name.Value, templateID, false, param.Name)
			paramTypes[i] = pType // The parameter itself expects a `type` argument
		} else {
			paramTypes[i] = pType
			sigScope.Define(param.Name.Value, pType, false, param.Name)
		}
	}

	retType := e.CachedPrimitives["void"]
	if node.ReturnType != nil {
		retType = e.resolveTypeSignature(node.ReturnType, sigScope)
		if retType == 0 {
			return 0
		}
	}

	name := "<lambda>"
	if node.Name != nil {
		name = node.Name.Value
	}

	funcType := UniversalType{
		ID:         TypeID(len(e.Pool.Types)),
		Mask:       MaskIsFunction,
		Name:       name + "_signature",
		FuncParams: paramTypes,
		FuncReturn: retType,
		Executable: node, // Stash the AST for later Monomorphization!
	}
	e.Pool.Types = append(e.Pool.Types, funcType)

	if node.Name != nil {
		if sym, exists := scope.Resolve(node.Name.Value); exists {
			sym.TypeID = funcType.ID
			sym.IsResolved = true
		}
	}

	// Only execute the body if it is NOT a generic template.
	// Templates defer execution until called.
	if !isGeneric && node.Body != nil {
		funcScope := NewScope(scope)
		for i, param := range node.Parameters {
			funcScope.Define(param.Name.Value, paramTypes[i], false, param.Name)
		}

		prevRet := e.ExpectedReturnType
		e.ExpectedReturnType = retType
		defer func() { e.ExpectedReturnType = prevRet }()

		bodyType := e.evaluateBlock(node.Body, funcScope)

		if bodyType != 0 && bodyType != retType && retType != e.CachedPrimitives["void"] {
			return e.error(node.Body.Span(), "function body bubbles a type different from its signature")
		}
	}

	return funcType.ID
}

// evaluateCallExpr intercepts generic functions, builds a concrete scope, and re-evaluates them.
func (e *Evaluator) evaluateCallExpr(node *ast.CallExpr, scope *Scope) TypeID {
	funcID := e.Evaluate(node.Function, scope)
	if funcID == 0 {
		return 0
	}

	funcType := e.Pool.Types[funcID]
	if (funcType.Mask & MaskIsFunction) == 0 {
		return e.error(node.Function.Span(), "attempted to call a non-function type")
	}

	// --- METHOD CALL INJECTION (Syntactic Sugar) ---
	isMethodCall := false
	var methodSelfArg TypeID = 0

	// If we are calling something like `v_ptr.magnitude_sq()`
	if fieldAccess, ok := node.Function.(*ast.FieldAccessExpr); ok {
		leftObjID := e.Evaluate(fieldAccess.Left, scope)
		actualObjType := e.Pool.Types[leftObjID]

		if (actualObjType.Mask & MaskIsPointer) != 0 {
			actualObjType = e.Pool.Types[actualObjType.BaseType]
		}

		// Check if it's genuinely a method attached to the struct
		if _, isMethod := actualObjType.Methods[fieldAccess.Field.Value]; isMethod {
			isMethodCall = true
			methodSelfArg = leftObjID

			// Auto-Reference: If method needs *Vec3 but we called it on Vec3
			expectedSelfType := funcType.FuncParams[0]
			if (e.Pool.Types[expectedSelfType].Mask&MaskIsPointer) != 0 && (e.Pool.Types[methodSelfArg].Mask&MaskIsPointer) == 0 {
				methodSelfArg = e.getOrCreatePointerType(methodSelfArg)
			}
			// Auto-Dereference: If method needs Vec3 but we called it on *Vec3
			if (e.Pool.Types[expectedSelfType].Mask&MaskIsPointer) == 0 && (e.Pool.Types[methodSelfArg].Mask&MaskIsPointer) != 0 {
				methodSelfArg = e.Pool.Types[methodSelfArg].BaseType
			}
		}
	}

	expectedArgs := len(funcType.FuncParams)
	actualArgs := len(node.Arguments)
	if isMethodCall {
		actualArgs++
	}

	if actualArgs != expectedArgs {
		return e.error(node.Span(), "incorrect number of arguments")
	}

	// Determine if this is a generic instantiation call
	isGenericCall := false
	for _, p := range funcType.FuncParams {
		if p == e.CachedPrimitives["type"] {
			isGenericCall = true
			break
		}
	}

	// --- GENERIC MONOMORPHIZATION ROUTINE ---
	if isGenericCall && funcType.Executable != nil {
		decl := funcType.Executable.(*ast.FunctionDecl)
		instScope := NewScope(scope)

		// 1. Extract and bind the generic type arguments FIRST
		for i, param := range decl.Parameters {
			if funcType.FuncParams[i] == e.CachedPrimitives["type"] {
				concreteTypeID := e.Evaluate(node.Arguments[i], scope)
				if concreteTypeID == 0 {
					return 0
				}
				instScope.Define(param.Name.Value, concreteTypeID, false, param.Name)
			}
		}

		// 2. Re-resolve parameter and return types within the instantiated scope!
		concreteParams := make([]TypeID, len(decl.Parameters))
		for i, param := range decl.Parameters {
			if funcType.FuncParams[i] == e.CachedPrimitives["type"] {
				concreteParams[i] = e.CachedPrimitives["type"]
			} else {
				concreteParams[i] = e.resolveTypeSignature(param.Type, instScope)
			}
		}

		concreteRet := e.CachedPrimitives["void"]
		if decl.ReturnType != nil {
			concreteRet = e.resolveTypeSignature(decl.ReturnType, instScope)
		}

		// 3. Type-Check normal arguments against the specialized signature
		for i, arg := range node.Arguments {
			argType := e.Evaluate(arg, scope)
			if argType == 0 {
				return 0
			}
			if concreteParams[i] == e.CachedPrimitives["type"] {
				continue
			}
			if argType != concreteParams[i] {
				return e.error(arg.Span(), "argument type mismatch in generic instantiation")
			}
			instScope.Define(decl.Parameters[i].Name.Value, argType, false, decl.Parameters[i].Name)
		}

		// 4. Evaluate the specialized body!
		prevRet := e.ExpectedReturnType
		e.ExpectedReturnType = concreteRet
		actualRet := e.evaluateBlock(decl.Body, instScope)
		e.ExpectedReturnType = prevRet

		if actualRet != 0 && actualRet != concreteRet && concreteRet != e.CachedPrimitives["void"] {
			return e.error(decl.Body.Span(), "generic function body bubbles a type different from its instantiated signature")
		}

		specializedName := fmt.Sprintf("%s_inst_%d", decl.Name.Value, concreteRet)
		specializedFunc := UniversalType{
			ID:         TypeID(len(e.Pool.Types)),
			Mask:       MaskIsFunction,
			Name:       specializedName,
			FuncParams: concreteParams,
			FuncReturn: concreteRet,
		}
		e.Pool.Types = append(e.Pool.Types, specializedFunc)

		return concreteRet
	}

	// --- STANDARD NON-GENERIC CALL ROUTINE ---
	argOffset := 0
	if isMethodCall {
		if methodSelfArg != funcType.FuncParams[0] {
			return e.error(node.Function.Span(), "implicit 'self' parameter type mismatch")
		}
		argOffset = 1
	}

	for i, arg := range node.Arguments {
		argType := e.Evaluate(arg, scope)
		if argType == 0 {
			return 0
		}

		if argType != funcType.FuncParams[i+argOffset] {
			return e.error(arg.Span(), "argument type mismatch")
		}
	}
	return funcType.FuncReturn
}
