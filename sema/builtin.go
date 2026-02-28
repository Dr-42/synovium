package sema

import "synovium/ast"

// InjectBuiltins initializes the hardware axioms and fundamental types.
func (e *Evaluator) InjectBuiltins(globalScope *Scope) {
	// Helper to forge a primitive type
	forge := func(name string, mask TypeMask, sizeBits uint64, llvmName string) TypeID {
		t := UniversalType{
			ID:            TypeID(len(e.Pool.Types)),
			Mask:          mask,
			Name:          name,
			TrueSizeBits:  sizeBits,
			IsFundamental: true,
			LLVMTypeName:  llvmName,
			Fields:        make(map[string]TypeID),
		}
		e.Pool.Types = append(e.Pool.Types, t)
		e.CachedPrimitives[name] = t.ID

		// Inject the type into the global scope as an immutable compile-time constant
		globalScope.Define(name, t.ID, false, nil)
		return t.ID
	}

	// 1. The Meta-Type (The type of `type` itself)
	forge("type", MaskIsMeta, 0, "void")

	// 2. Booleans
	forge("bln", 0, 8, "i1")

	// 3. Chars
	forge("chr", MaskIsNumeric, 8, "i8")

	// 4. Integers (Ranked 1 to 4)
	forge("i8", MaskIsNumeric|MaskIsSigned|(1<<RankShift), 8, "i8")
	forge("i16", MaskIsNumeric|MaskIsSigned|(2<<RankShift), 16, "i16")
	forge("i32", MaskIsNumeric|MaskIsSigned|(3<<RankShift), 32, "i32")
	forge("i64", MaskIsNumeric|MaskIsSigned|(4<<RankShift), 64, "i64")

	forge("u8", MaskIsNumeric|(1<<RankShift), 8, "i8")
	forge("u16", MaskIsNumeric|(2<<RankShift), 16, "i16")
	forge("u32", MaskIsNumeric|(3<<RankShift), 32, "i32")
	forge("u64", MaskIsNumeric|(4<<RankShift), 64, "i64")

	// 5. Floats (Ranked 3 to 4, with the Float bit flagged)
	forge("f32", MaskIsNumeric|MaskIsFloat|MaskIsSigned|(3<<RankShift), 32, "float")
	forge("f64", MaskIsNumeric|MaskIsFloat|MaskIsSigned|(4<<RankShift), 64, "double")

	// 6. Strings (Represented natively as a fat pointer: length + pointer to u8)
	forge("str", MaskIsStruct, 128, "{ i64, i8* }")

	// 7. Void (For statements and empty blocks)
	forge("void", 0, 0, "void")
}

// In sema/builtin.go

// resolveTypeSignature translates an AST type node (like `i32` or `*Vec3`) into its TypeID.
func (e *Evaluator) resolveTypeSignature(t ast.Type, scope *Scope) TypeID {
	switch v := t.(type) {

	case *ast.NamedType:
		if sym, exists := scope.Resolve(v.Name); exists {
			return sym.TypeID
		}
		return e.error(v.Span(), "unknown type: "+v.Name)

	case *ast.FunctionType:
		// Build an anonymous UniversalType for the function pointer
		paramIDs := make([]TypeID, len(v.Parameters))
		for i, p := range v.Parameters {
			paramIDs[i] = e.resolveTypeSignature(p, scope)
		}
		retID := e.CachedPrimitives["void"]
		if v.ReturnType != nil {
			retID = e.resolveTypeSignature(v.ReturnType, scope)
		}

		// Note: We don't hash-cons function pointers yet, but we could!
		ft := UniversalType{
			ID:         TypeID(len(e.Pool.Types)),
			Mask:       MaskIsFunction,
			Name:       "fnc_ptr",
			FuncParams: paramIDs,
			FuncReturn: retID,
		}
		e.Pool.Types = append(e.Pool.Types, ft)
		return ft.ID

	case *ast.PointerType:
		base := e.resolveTypeSignature(v.Base, scope)
		return e.getOrCreatePointerType(base)

	case *ast.ReferenceType: // References map exactly to pointers in hardware
		base := e.resolveTypeSignature(v.Base, scope)
		return e.getOrCreatePointerType(base)

	case *ast.ArrayType:
		base := e.resolveTypeSignature(v.Base, scope)
		var size uint64 = 0

		if !v.IsSlice && v.Size != nil {
			// Actually evaluate the comptime array size!
			// We check if the expression is a direct integer literal.
			if intLit, ok := v.Size.(*ast.IntLiteral); ok {
				size = uint64(intLit.Value)
			} else {
				// In the future, this would recursively evaluate comptime constants (like `THRESHOLD * 2`)
				return e.error(v.Size.Span(), "array size must be an integer literal")
			}
		}

		return e.getOrCreateArrayType(base, size, v.IsSlice)
	}

	return 0
}
