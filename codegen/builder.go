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
	StringConstants []string
	LoopExits       []string // <-- NEW: Tracks active loop exit blocks for `brk`
}

func NewBuilder(pool *sema.TypePool) *Builder {
	return &Builder{
		Pool:         pool,
		nextRegID:    1,
		nextStringID: 1,
		Locals:       make(map[string]string),
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

func (b *Builder) Generate(program []ast.Decl) string {
	b.emitTypeDeclarations()
	b.EmitLine("")

	// 1. Top-Level Functions & Impl Methods
	for _, decl := range program {
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			b.emitFunction(fn, b.Pool.NodeTypes[fn])
		} else if impl, ok := decl.(*ast.ImplDecl); ok {
			// THE FIX: Safely find the Target Struct by Name
			for _, t := range b.Pool.Types {
				if t.Name == impl.Target.Value {
					for _, fn := range impl.Methods {
						if methodID, exists := t.Methods[fn.Name.Value]; exists {
							b.emitFunction(fn, methodID)
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
				b.emitFunction(fn, t.ID)
			}
		}
	}

	// 3. Global Strings
	for _, strDef := range b.StringConstants {
		b.EmitLine(strDef)
	}

	return b.Output.String()
}
