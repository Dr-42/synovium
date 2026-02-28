package sema

import (
	"fmt"
	"strings"
	"synovium/ast"
)

// evaluateEnumDecl evaluates an enum and registers it as a tagged union in the pool.
func (e *Evaluator) evaluateEnumDecl(node *ast.EnumDecl, scope *Scope) TypeID {
	variants := make(map[string][]TypeID)
	var maxPayloadSize uint64 = 0

	// 1. Resolve Variants and calculate maximum payload size
	for _, variant := range node.Variants {
		payloadTypes := make([]TypeID, len(variant.Types))
		var currentPayloadSize uint64 = 0

		for i, t := range variant.Types {
			typeID := e.resolveTypeSignature(t, scope)
			if typeID == 0 {
				return 0
			}

			payloadTypes[i] = typeID
			currentPayloadSize += e.Pool.Types[typeID].TrueSizeBits
		}

		variants[variant.Name.Value] = payloadTypes

		// Synovium Enums are Tagged Unions: memory size is max(payloads) + 8 bit tag
		if currentPayloadSize > maxPayloadSize {
			maxPayloadSize = currentPayloadSize
		}
	}

	// --- THE FIX: Handle Anonymous Enums ---
	name := fmt.Sprintf("anon_enum_%d", len(e.Pool.Types))
	if node.Name != nil {
		name = node.Name.Value
	}

	// 2. Allocate the UniversalType
	enumType := UniversalType{
		ID:            TypeID(len(e.Pool.Types)),
		Mask:          MaskIsStruct,       // Enums behave like structs for memory routing
		Name:          name,               // Use the real name or the anonymous ID
		TrueSizeBits:  8 + maxPayloadSize, // 8 bits for the variant tag
		IsFundamental: false,
		Variants:      variants,
		Methods:       make(map[string]TypeID),
	}

	e.Pool.Types = append(e.Pool.Types, enumType)

	// 3. Only patch the deferred symbol in the scope if it had a name!
	if node.Name != nil {
		if sym, exists := scope.Resolve(node.Name.Value); exists {
			sym.TypeID = enumType.ID
			sym.IsResolved = true
		}
	}

	return enumType.ID
}

// evaluateImplDecl attaches methods to an existing Struct or Enum.
func (e *Evaluator) evaluateImplDecl(node *ast.ImplDecl, scope *Scope) TypeID {
	targetSym, exists := scope.Resolve(node.Target.Value)
	if !exists || !targetSym.IsResolved {
		return e.error(node.Target.Span(), "impl target must be a declared type")
	}

	// We access the pool directly via index to mutate the Methods map
	targetType := &e.Pool.Types[targetSym.TypeID]

	if targetType.Methods == nil {
		targetType.Methods = make(map[string]TypeID)
	}

	// Inject 'Self' into the scope so methods can legally use `self: *Self`
	implScope := NewScope(scope)
	implScope.Define("Self", targetSym.TypeID, false, node.Target)

	for _, method := range node.Methods {
		methodID := e.evaluateFunctionDecl(method, implScope)
		if methodID == 0 {
			return 0
		}

		targetType.Methods[method.Name.Value] = methodID
	}

	return targetSym.TypeID
}

// evaluateMatchExpr handles exhaustiveness checking and variant unwrapping
func (e *Evaluator) evaluateMatchExpr(node *ast.MatchExpr, scope *Scope) TypeID {
	valueID := e.Evaluate(node.Value, scope)
	if valueID == 0 {
		return 0
	}

	valueType := e.Pool.Types[valueID]
	if valueType.Variants == nil {
		return e.error(node.Value.Span(), "can only match on enum types")
	}

	var expectedReturnType TypeID = 0

	// Iterate through match arms
	for _, arm := range node.Arms {
		// arm.Pattern.Value looks like "Status.Running"
		parts := strings.Split(arm.Pattern.Value, ".")
		variantName := parts[len(parts)-1]

		payloadTypes, isValidVariant := valueType.Variants[variantName]
		if !isValidVariant {
			return e.error(arm.Pattern.Span(), "invalid variant '"+variantName+"' for enum "+valueType.Name)
		}

		if len(arm.Params) != len(payloadTypes) {
			return e.error(arm.Pattern.Span(), "incorrect number of bound parameters for variant")
		}

		armScope := NewScope(scope)

		// Bind the extracted payload (e.g. `speed`) to its type (e.g. `f64`) in the arm's inner scope
		for i, paramIdent := range arm.Params {
			armScope.Define(paramIdent.Value, payloadTypes[i], false, paramIdent)
		}

		armType := e.evaluateBlock(arm.Body, armScope)
		if armType == 0 {
			return 0
		}

		// Strict branch unification
		if expectedReturnType == 0 {
			expectedReturnType = armType
		} else if !e.typesMatch(expectedReturnType, armType) {
			return e.error(arm.Body.Span(), "match arms have incompatible return types")
		}
	}

	return expectedReturnType
}
