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

	Locals          map[string]string // Tracks variable allocations
	StringConstants []string          // Hoists literal strings to global scope
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

func (b *Builder) EmitLine(format string, args ...any) {
	b.Output.WriteString(fmt.Sprintf(format+"\n", args...))
}

func (b *Builder) Generate(program []ast.Decl) string {
	b.emitTypeDeclarations()
	b.EmitLine("")

	for _, decl := range program {
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			b.emitFunction(fn)
		}
	}

	// LLVM allows globals at the bottom! Print hoisted strings here.
	for _, strDef := range b.StringConstants {
		b.EmitLine(strDef)
	}

	return b.Output.String()
}
