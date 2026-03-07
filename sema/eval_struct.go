package sema

import (
	"fmt"

	"synovium/ast"
)

func (e *Evaluator) evaluateStructDecl(node *ast.StructDecl, scope *Scope) TypeID {
	fields := make(map[string]TypeID)
	var fieldLayout []TypeID
	fieldIndices := make(map[string]int) // <-- NEW
	var totalSize uint64 = 0

	// --- NEW: Template Registration ---
	name := fmt.Sprintf("anon_struct_%d", len(e.Pool.Types))
	if node.Name != nil {
		name = node.Name.Value
	}

	if len(node.GenericParams) > 0 {
		templateID := TypeID(len(e.Pool.Types))
		e.Pool.Types = append(e.Pool.Types, UniversalType{
			ID:         templateID,
			Mask:       MaskIsMeta,
			Name:       name + "_template",
			Executable: node, // Stash AST!
		})
		if node.Name != nil {
			if sym, exists := scope.Resolve(node.Name.Value); exists {
				sym.TypeID = templateID
				sym.IsResolved = true
			}
		}
		return templateID
	}

	for i, field := range node.Fields { // <-- Note the 'i'
		fieldTypeID := e.resolveTypeSignature(field.Type, scope)
		if fieldTypeID == 0 {
			return 0
		}

		fields[field.Name.Value] = fieldTypeID
		fieldLayout = append(fieldLayout, fieldTypeID)
		fieldIndices[field.Name.Value] = i // <-- NEW: Save the index!
		totalSize += e.Pool.Types[fieldTypeID].TrueSizeBits
	}

	if node.Name != nil {
		name = node.Name.Value
	}

	structType := UniversalType{
		ID:            TypeID(len(e.Pool.Types)),
		Mask:          MaskIsStruct,
		Name:          name,
		TrueSizeBits:  totalSize,
		IsFundamental: false,
		Fields:        fields,
		FieldLayout:   fieldLayout,
		FieldIndices:  fieldIndices, // <-- NEW: Attach it to the type!
		Methods:       make(map[string]TypeID),
	}

	e.Pool.Types = append(e.Pool.Types, structType)

	// 2. Only patch the scope if it actually had a name!
	if node.Name != nil {
		if sym, exists := scope.Resolve(node.Name.Value); exists {
			sym.TypeID = structType.ID
			sym.IsResolved = true
		}
	}

	return structType.ID
}

func (e *Evaluator) evaluateStructInit(node *ast.StructInitExpr, scope *Scope) TypeID {
	structSym, exists := scope.Resolve(node.Name.Value)
	if !exists || !structSym.IsResolved {
		return e.error(node.Name.Span(), "undeclared or unresolved struct: "+node.Name.Value)
	}

	targetType := e.Pool.Types[structSym.TypeID]
	if (targetType.Mask & MaskIsStruct) == 0 {
		return e.error(node.Name.Span(), "cannot initialize a non-struct type")
	}

	initializedFields := make(map[string]bool)

	for _, initField := range node.Fields {
		expectedFieldType, fieldExists := targetType.Fields[initField.Name.Value]
		if !fieldExists {
			return e.error(initField.Name.Span(), "struct '"+targetType.Name+"' has no field named '"+initField.Name.Value+"'")
		}

		providedType := e.Evaluate(initField.Value, scope)
		if providedType != expectedFieldType {
			return e.error(initField.Value.Span(), "type mismatch for field '"+initField.Name.Value+"'")
		}

		initializedFields[initField.Name.Value] = true
	}

	if len(initializedFields) != len(targetType.Fields) {
		return e.error(node.Span(), "missing fields in struct initialization for '"+targetType.Name+"'")
	}

	return targetType.ID
}

func (e *Evaluator) evaluateFieldAccess(node *ast.FieldAccessExpr, scope *Scope) TypeID {
	leftID := e.Evaluate(node.Left, scope)
	if leftID == 0 {
		return 0
	}

	leftType := e.Pool.Types[leftID]

	// --- SYNTACTIC SUGAR: AUTO-DEREFERENCE ---
	// If it's a pointer, implicitly unwrap it to its base struct/enum type
	if (leftType.Mask & MaskIsPointer) != 0 {
		leftType = e.Pool.Types[leftType.BaseType]
	}

	if (leftType.Mask & MaskIsStruct) == 0 {
		return e.error(node.Left.Span(), "cannot access field of a non-struct type")
	}

	// 1. Lookup Struct Field
	if fieldTypeID, exists := leftType.Fields[node.Field.Value]; exists {
		return fieldTypeID
	}

	// 2. Lookup Impl Method
	if methodTypeID, exists := leftType.Methods[node.Field.Value]; exists {
		return methodTypeID
	}

	// 3. Lookup Enum Variant (Dynamic Constructor Forging)
	if payloadTypes, isVariant := leftType.Variants[node.Field.Value]; isVariant {
		// If the variant has no payload (e.g., Status.Idle), it evaluates directly to the Enum itself
		if len(payloadTypes) == 0 {
			return leftType.ID
		}

		// If it has a payload, forge a function signature that takes the payloads and returns the Enum
		constructorType := UniversalType{
			ID:         TypeID(len(e.Pool.Types)),
			Mask:       MaskIsFunction,
			Name:       leftType.Name + "::" + node.Field.Value,
			FuncParams: payloadTypes,
			FuncReturn: leftType.ID,
		}
		e.Pool.Types = append(e.Pool.Types, constructorType)
		return constructorType.ID
	}

	// allMethods := make([]string, 0, len(leftType.Methods))
	// methodStr := ""
	// for _, methodName := range allMethods {
	// 	methodStr += "\n\t" + methodName
	// }
	//
	// return e.error(node.Field.Span(), "type '"+leftType.Name+"' has no field, method, or variant named '"+node.Field.Value+"'"+"Existing methods are"+methodStr)
	return e.error(node.Field.Span(), "type '"+leftType.Name+"' has no field, method, or variant named '"+node.Field.Value+"'")
}
