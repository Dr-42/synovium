package codegen

import (
	"fmt"
	"strings"
	"synovium/ast"
	"synovium/sema"
)

// Builder tracks the state of the LLVM IR generation.
type Builder struct {
	Pool   *sema.TypePool
	Output strings.Builder

	// Virtual Register & Label tracking
	nextRegID   int
	nextLabelID int

	// The currently executing function (for returns and allocas)
	currentFunc string
}

func NewBuilder(pool *sema.TypePool) *Builder {
	return &Builder{
		Pool:      pool,
		nextRegID: 1, // LLVM virtual registers start at %1
	}
}

// NextReg generates a new strictly incrementing virtual register (e.g., "%1", "%2")
func (b *Builder) NextReg() string {
	reg := fmt.Sprintf("%%%d", b.nextRegID)
	b.nextRegID++
	return reg
}

// NextLabel generates a new basic block label (e.g., "bb1", "bb2")
func (b *Builder) NextLabel() string {
	label := fmt.Sprintf("bb%d", b.nextLabelID)
	b.nextLabelID++
	return label
}

// EmitLine writes a formatted line of LLVM IR to the output buffer
func (b *Builder) EmitLine(format string, args ...any) {
	b.Output.WriteString(fmt.Sprintf(format+"\n", args...))
}

// Generate takes the completely resolved TAST and produces the final .ll string.
func (b *Builder) Generate(program []ast.Decl) string {
	// 1. Emit the Target Data Layout and Triple (Optional, but good for C FFI later)
	// b.EmitLine("target datalayout = \"e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-f80:128-n8:16:32:64-S128\"")

	// 2. Emit all custom types (Structs and Enums)
	b.emitTypeDeclarations()
	b.EmitLine("")

	// 3. Emit globals (String literals, etc. - coming soon)

	// 4. Emit the actual functions
	for _, decl := range program {
		if fn, ok := decl.(*ast.FunctionDecl); ok {
			b.emitFunction(fn)
		}
	}

	return b.Output.String()
}
