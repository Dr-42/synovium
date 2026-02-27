package sema

import (
	"fmt"
	"synovium/ast"
)

// evaluatePrefix handles -, !, ~, *, and &
func (e *Evaluator) evaluatePrefix(node *ast.PrefixExpr, scope *Scope) TypeID {
	rightID := e.Evaluate(node.Right, scope)
	if rightID == 0 {
		return 0
	}
	rightType := e.Pool.Types[rightID]

	switch node.Operator {
	case "-":
		if (rightType.Mask & MaskIsNumeric) == 0 {
			return e.error(node.Span(), "cannot negate a non-numeric type")
		}
		return rightID
	case "!":
		if rightID != e.CachedPrimitives["bln"] {
			return e.error(node.Span(), "logical NOT (!) requires a boolean")
		}
		return rightID
	case "~":
		if (rightType.Mask&MaskIsNumeric) == 0 || (rightType.Mask&MaskIsFloat) != 0 {
			return e.error(node.Span(), "bitwise NOT (~) requires an integer")
		}
		return rightID
	case "*": // Dereference
		if (rightType.Mask & MaskIsPointer) == 0 {
			return e.error(node.Span(), "cannot dereference a non-pointer type")
		}
		return rightType.BaseType
	case "&": // Address-Of
		return e.getOrCreatePointerType(rightID)
	}

	return e.error(node.Span(), "unknown prefix operator: "+node.Operator)
}

// evaluateIndexExpr handles array and string indexing: arr[i]
func (e *Evaluator) evaluateIndexExpr(node *ast.IndexExpr, scope *Scope) TypeID {
	leftID := e.Evaluate(node.Left, scope)
	indexID := e.Evaluate(node.Index, scope)

	if leftID == 0 || indexID == 0 {
		return 0
	}

	leftType := e.Pool.Types[leftID]
	indexType := e.Pool.Types[indexID]

	// 1. Verify index is an integer
	if (indexType.Mask&MaskIsNumeric) == 0 || (indexType.Mask&MaskIsFloat) != 0 {
		return e.error(node.Index.Span(), "array index must be an integer")
	}

	// 2. Verify left side is indexable
	if leftType.Name == "str" {
		return e.CachedPrimitives["u8"] // Strings are arrays of bytes
	}

	if (leftType.Mask & MaskIsArray) == 0 {
		return e.error(node.Left.Span(), "cannot index into a non-array type")
	}

	return leftType.BaseType
}

// evaluateCastExpr handles explicit conversions: val as i32
func (e *Evaluator) evaluateCastExpr(node *ast.CastExpr, scope *Scope) TypeID {
	leftID := e.Evaluate(node.Left, scope)
	targetID := e.resolveTypeSignature(node.Type, scope)

	if leftID == 0 || targetID == 0 {
		return 0
	}

	leftType := e.Pool.Types[leftID]
	targetType := e.Pool.Types[targetID]

	// Synovium Cast Rules:
	// 1. Numeric to Numeric is always allowed (truncation/extension handled by LLVM)
	if (leftType.Mask&MaskIsNumeric) != 0 && (targetType.Mask&MaskIsNumeric) != 0 {
		return targetID
	}

	// 2. Pointer to Pointer (Unsafe casting)
	if (leftType.Mask&MaskIsPointer) != 0 && (targetType.Mask&MaskIsPointer) != 0 {
		return targetID
	}

	// 3. Integer to Pointer / Pointer to Integer (for raw memory addresses)
	if ((leftType.Mask&MaskIsPointer) != 0 && (targetType.Mask&MaskIsNumeric) != 0) ||
		((leftType.Mask&MaskIsNumeric) != 0 && (targetType.Mask&MaskIsPointer) != 0) {
		return targetID
	}

	return e.error(node.Span(), fmt.Sprintf("invalid cast from %s to %s", leftType.Name, targetType.Name))
}

// evaluateBubbleExpr handles the error bubbling operator: val?
func (e *Evaluator) evaluateBubbleExpr(node *ast.BubbleExpr, scope *Scope) TypeID {
	leftID := e.Evaluate(node.Left, scope)
	if leftID == 0 {
		return 0
	}

	// In a complete implementation, this would check if leftID is an Enum (like `Status`),
	// extract the "Ok" variant's type, and automatically wire the "Error" variant to return
	// from the current function scope.

	// For now, to keep the compiler unblocked, we assume it cleanly unwraps and returns the type itself.
	return leftID
}

// --- DYNAMIC TYPE GENERATORS ---

func (e *Evaluator) getOrCreatePointerType(baseID TypeID) TypeID {
	// 1. Search existing pool to prevent duplicates (Hash Consing)
	for _, t := range e.Pool.Types {
		if (t.Mask&MaskIsPointer) != 0 && t.BaseType == baseID {
			return t.ID
		}
	}

	// 2. Forge a new Pointer Type
	baseName := e.Pool.Types[baseID].Name
	pt := UniversalType{
		ID:           TypeID(len(e.Pool.Types)),
		Mask:         MaskIsPointer,
		Name:         "*" + baseName,
		TrueSizeBits: 64, // Pointers are always 64-bit on modern hardware
		BaseType:     baseID,
	}
	e.Pool.Types = append(e.Pool.Types, pt)
	return pt.ID
}

func (e *Evaluator) getOrCreateArrayType(baseID TypeID, size uint64, isSlice bool) TypeID {
	// 1. Search existing pool
	for _, t := range e.Pool.Types {
		if (t.Mask&MaskIsArray) != 0 && t.BaseType == baseID && t.Capacity == size {
			return t.ID
		}
	}

	// 2. Forge a new Array Type
	baseName := e.Pool.Types[baseID].Name
	name := fmt.Sprintf("[%s; %d]", baseName, size)
	if isSlice {
		name = fmt.Sprintf("[%s; :]", baseName)
	}

	at := UniversalType{
		ID:           TypeID(len(e.Pool.Types)),
		Mask:         MaskIsArray,
		Name:         name,
		TrueSizeBits: e.Pool.Types[baseID].TrueSizeBits * size,
		BaseType:     baseID,
		Capacity:     size,
	}

	// Slices are fat pointers (pointer + length)
	if isSlice {
		at.TrueSizeBits = 128
	}

	e.Pool.Types = append(e.Pool.Types, at)
	return at.ID
}
