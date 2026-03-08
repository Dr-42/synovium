package codegen

import (
	"fmt"
	"strings"

	"synovium/ast"
	"synovium/sema"
)

type Builder struct {
	Pool         *sema.TypePool
	Output       strings.Builder
	nextRegID    int
	nextLabelID  int
	nextStringID int
	currentFunc  string

	Locals          map[string]string
	Globals         map[string]string
	StringConstants []string
	LoopContexts    []LoopContext
}

type LoopContext struct {
	ExitLbl   string
	ResultPtr string
	LLVMType  string
	LabelName string
}

func NewBuilder(pool *sema.TypePool) *Builder {
	return &Builder{
		Pool:         pool,
		nextRegID:    1,
		nextStringID: 1,
		Locals:       make(map[string]string),
		Globals:      make(map[string]string),
	}
}

func (b *Builder) NextReg() string {
	reg := fmt.Sprintf("%%%d", b.nextRegID)
	b.nextRegID++
	return reg
}

func (b *Builder) NextLabel() string {
	lbl := fmt.Sprintf("L%d", b.nextLabelID)
	b.nextLabelID++
	return lbl
}

func (b *Builder) EmitLine(format string, args ...any) {
	b.Output.WriteString(fmt.Sprintf(format+"\n", args...))
}

func (b *Builder) isGenericFunction(id sema.TypeID) bool {
	if int(id) >= len(b.Pool.Types) {
		return false
	}
	t := b.Pool.Types[id]
	for _, pID := range t.FuncParams {
		if int(pID) < len(b.Pool.Types) {
			// If a parameter is 'type' OR a dummy template placeholder
			if b.Pool.Types[pID].Name == "type" || b.Pool.Types[pID].Mask == 0 {
				return true
			}
		}
	}
	return false
}

func (b *Builder) Generate(program []ast.Decl) string {
	b.emitTypeDeclarations()
	b.EmitLine("")

	// 0. Extern Global Variables
	for _, decl := range program {
		if vDecl, ok := decl.(*ast.VariableDecl); ok && vDecl.Value == nil {
			typeID := b.Pool.NodeTypes[vDecl]
			llvmType := b.GetLLVMType(typeID)
			b.EmitLine("@%s = external global %s", vDecl.Name.Value, llvmType)
			b.Globals[vDecl.Name.Value] = "@" + vDecl.Name.Value
		}
	}

	// 1. Top-Level Functions & Impl Methods
	for _, decl := range program {
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			funcTypeID := b.Pool.NodeTypes[fn]
			// Skip generic template definitions!
			if !b.isGenericFunction(funcTypeID) {
				b.emitFunction(fn, funcTypeID)
			}
		} else if impl, ok := decl.(*ast.ImplDecl); ok {
			for _, t := range b.Pool.Types {
				if t.Name == impl.Target.Value {
					for _, fn := range impl.Methods {
						if methodID, exists := t.Methods[fn.Name.Value]; exists {
							// Skip generic method definitions!
							if !b.isGenericFunction(methodID) {
								b.emitFunction(fn, methodID)
							}
						}
					}
					break
				}
			}
		}
	}

	// 2. Generic Instantiations
	for _, t := range b.Pool.Types {
		if (t.Mask&sema.MaskIsFunction) != 0 && strings.Contains(t.Name, "_inst_") {
			if fn, ok := t.Executable.(*ast.FunctionDecl); ok {
				// The concrete instantiations WILL be emitted here!
				b.emitFunction(fn, t.ID)
			}
		}
	}

	// 3. Global Strings
	for _, strDef := range b.StringConstants {
		b.EmitLine("%s", strDef)
	}
	b.StringConstants = nil

	return b.Output.String()
}
