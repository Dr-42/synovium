package sema

import "synovium/ast"

// TypeID is a direct index into the TypePool. (Handles 4.2 billion types)
type TypeID uint32

// TypeMask packs behavioral traits into 32 bits for single-cycle CPU logic.
type TypeMask uint32

const (
	// Basic routing traits (Bits 0-7)
	MaskIsPointer  TypeMask = 1 << 0
	MaskIsNumeric  TypeMask = 1 << 1
	MaskIsFloat    TypeMask = 1 << 2
	MaskIsSigned   TypeMask = 1 << 3
	MaskIsStruct   TypeMask = 1 << 4
	MaskIsArray    TypeMask = 1 << 5
	MaskIsFunction TypeMask = 1 << 6
	MaskIsMeta     TypeMask = 1 << 7 // The type of 'type'

	// Primitive Rank (Bits 8-11) used strictly for fast numeric promotion.
	// 0=Non-numeric, 1=8-bit, 2=16-bit, 3=32-bit, 4=64-bit, 5=128-bit, 6=SIMD
	RankShift = 8
	RankMask  = 0xF << RankShift
)

// UniversalType is the single, unified definition of all data layout in Synovium.
type UniversalType struct {
	ID   TypeID
	Mask TypeMask

	Name string

	// True limit-less size in bits. Decoupled from the bitmask to handle massive structs.
	TrueSizeBits uint64

	// Hardware Axioms
	IsFundamental bool
	LLVMTypeName  string

	// For Structs/Modules: Field name -> TypeID
	Fields map[string]TypeID

	// For Enums: Variant Name -> Slice of Payload TypeIDs
	Variants map[string][]TypeID

	// For Impl Blocks: Method Name -> Function TypeID
	Methods map[string]TypeID

	// For Arrays/Pointers/References: What is the underlying type?
	BaseType TypeID
	Capacity uint64 // For fixed arrays

	// For Functions
	FuncParams []TypeID
	FuncReturn TypeID

	// For Executable Comptime Functions/Modules
	Executable ast.Node
}

// TypePool is the global execution context holding all unique type layouts.
type TypePool struct {
	Types []UniversalType // Indexed natively by TypeID

	// 2D Operator Overload Dispatch Tables
	// Maps: [LeftID][RightID] -> ResultTypeID (0 means illegal/undefined)
	AddDispatch [][]TypeID
	SubDispatch [][]TypeID
	MulDispatch [][]TypeID
	DivDispatch [][]TypeID
}

// NewTypePool initializes the global pool.
func NewTypePool() *TypePool {
	pool := &TypePool{
		Types:       make([]UniversalType, 0),
		AddDispatch: make([][]TypeID, 0),
		SubDispatch: make([][]TypeID, 0),
		MulDispatch: make([][]TypeID, 0),
		DivDispatch: make([][]TypeID, 0),
	}

	// --- THE FIX: Reserve TypeID(0) for Errors ---
	// By pushing a dummy type at index 0, we ensure no valid type
	// (like our 'type' MetaType) accidentally gets an ID of 0.
	pool.Types = append(pool.Types, UniversalType{
		Name: "<error_or_unresolved>",
	})

	return pool
}

// promoteNumeric executes the cascading bitwise tie-breaker to find the dominant numeric type.
func PromoteNumeric(left, right TypeMask) TypeMask {
	leftRank := (left & RankMask) >> RankShift
	rightRank := (right & RankMask) >> RankShift

	// RULE 1: Highest Rank wins outright
	if leftRank > rightRank {
		return left
	}
	if rightRank > leftRank {
		return right
	}

	// --- TIE BREAKERS (Same Rank) ---

	// RULE 2: Float beats Integer
	if (left & MaskIsFloat) != 0 {
		return left
	}
	if (right & MaskIsFloat) != 0 {
		return right
	}

	// RULE 3: Unsigned beats Signed
	// (If MaskIsSigned is 0, it means it is unsigned)
	isLeftUnsigned := (left & MaskIsSigned) == 0
	isRightUnsigned := (right & MaskIsSigned) == 0

	if isLeftUnsigned {
		return left
	}
	if isRightUnsigned {
		return right
	}

	// Exact identical mask
	return left
}
