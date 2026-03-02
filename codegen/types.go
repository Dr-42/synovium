package codegen

import (
	"fmt"
	"strings"
	"synovium/sema"
)

func (b *Builder) emitTypeDeclarations() {
	for _, t := range b.Pool.Types {
		if (t.Mask & sema.MaskIsStruct) != 0 {
			if len(t.Variants) > 0 {
				payloadBytes := (t.TrueSizeBits - 8) / 8
				b.EmitLine("%%%s = type { i8, [%d x i8] }", t.Name, payloadBytes)
			} else {
				fieldTypes := make([]string, 0)
				// Iterate over the ordered FieldLayout instead of the chaotic map!
				for _, fieldTypeID := range t.FieldLayout {
					fieldTypes = append(fieldTypes, b.GetLLVMType(fieldTypeID))
				}
				b.EmitLine("%%%s = type { %s }", t.Name, strings.Join(fieldTypes, ", "))
			}
		}
	}
}

func (b *Builder) GetLLVMType(id sema.TypeID) string {
	if int(id) >= len(b.Pool.Types) {
		return "void"
	}

	t := b.Pool.Types[id]

	// --- FIX: Force Synovium string to map to C's char pointer ---
	if t.Name == "str" {
		return "i8*"
	}

	if t.LLVMTypeName != "" {
		return t.LLVMTypeName
	}
	if (t.Mask & sema.MaskIsPointer) != 0 {
		return b.GetLLVMType(t.BaseType) + "*"
	}
	if (t.Mask & sema.MaskIsStruct) != 0 {
		return "%" + t.Name
	}
	if (t.Mask & sema.MaskIsArray) != 0 {
		if t.Capacity == 0 {
			baseLLVM := b.GetLLVMType(t.BaseType)
			return fmt.Sprintf("{ i64, %s* }", baseLLVM)
		}
		baseLLVM := b.GetLLVMType(t.BaseType)
		return fmt.Sprintf("[%d x %s]", t.Capacity, baseLLVM)
	}

	return "void"
}
