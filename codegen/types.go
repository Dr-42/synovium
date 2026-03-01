package codegen

import (
	"fmt"
	"strings"
	"synovium/sema"
)

// emitTypeDeclarations loops over the TypePool and writes LLVM type strings.
func (b *Builder) emitTypeDeclarations() {
	for _, t := range b.Pool.Types {
		if (t.Mask & sema.MaskIsStruct) != 0 {
			if len(t.Variants) > 0 {
				// It's an Enum (Tagged Union)
				// E.g., %Action = type { i8, [24 x i8] }
				payloadBytes := (t.TrueSizeBits - 8) / 8
				b.EmitLine("%%%s = type { i8, [%d x i8] }", t.Name, payloadBytes)
			} else {
				// It's a Struct
				// E.g., %Entity = type { i64, double, i1 }
				fieldTypes := make([]string, 0)
				// Warning: Go maps are unordered! We need to sort fields by offset later,
				// but for now, we will just emit the types to see it work.
				for _, fieldTypeID := range t.Fields {
					fieldTypes = append(fieldTypes, b.GetLLVMType(fieldTypeID))
				}
				b.EmitLine("%%%s = type { %s }", t.Name, strings.Join(fieldTypes, ", "))
			}
		}
	}
}

// GetLLVMType converts a Synovium TypeID into its LLVM IR string equivalent.
func (b *Builder) GetLLVMType(id sema.TypeID) string {
	if int(id) >= len(b.Pool.Types) {
		return "void"
	}

	t := b.Pool.Types[id]

	// Base Primitives
	if t.LLVMTypeName != "" {
		return t.LLVMTypeName
	}

	// Pointers (e.g., *Entity -> %Entity*)
	if (t.Mask & sema.MaskIsPointer) != 0 {
		return b.GetLLVMType(t.BaseType) + "*"
	}

	// Structs & Enums (e.g., %Entity)
	if (t.Mask & sema.MaskIsStruct) != 0 {
		return "%" + t.Name
	}

	// Arrays (e.g., [5 x i32])
	if (t.Mask & sema.MaskIsArray) != 0 {
		if t.Capacity == 0 {
			// Slices (Fat Pointers) -> { i64, T* }
			baseLLVM := b.GetLLVMType(t.BaseType)
			return fmt.Sprintf("{ i64, %s* }", baseLLVM)
		}
		baseLLVM := b.GetLLVMType(t.BaseType)
		return fmt.Sprintf("[%d x %s]", t.Capacity, baseLLVM)
	}

	// Default fallback
	return "void"
}
