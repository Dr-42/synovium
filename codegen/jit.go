package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"synovium/ast"
	"synovium/sema"
)

// A global thread-safe counter to guarantee absolutely zero collisions during recursive JITs
var jitCounter int64

// RunJIT spins up a temporary LLVM instance to execute an AST Expression and retrieve its raw C-ABI memory.
// Add envScope to the signature!
// Signature updated to accept globalDecls
func RunJIT(expr ast.Expr, targetType sema.TypeID, pool *sema.TypePool, envScope *sema.Scope, globalDecls []ast.Decl) ([]byte, error) {
	b := NewBuilder(pool)

	// THE FIX: Use the exact original File AST to rebuild the environment!
	// This guarantees `struct`, `impl`, and `enum` are properly emitted!
	var dummy []ast.Decl
	for _, decl := range globalDecls {
		if fn, isFn := decl.(*ast.FunctionDecl); isFn && fn.Name != nil && fn.Name.Value == "main" {
			continue // Strip the user's main function
		}
		dummy = append(dummy, decl)
	}
	b.Generate(dummy)

	tmpDir := os.TempDir()
	id := atomic.AddInt64(&jitCounter, 1)
	timestamp := time.Now().UnixNano()

	filePrefix := fmt.Sprintf("syn_comptime_%d_%d", timestamp, id)
	binFile := filepath.Join(tmpDir, filePrefix+".bin")
	llFile := filepath.Join(tmpDir, filePrefix+".ll")
	exeFile := filepath.Join(tmpDir, filePrefix+".exe")

	safeBinFile := strings.ReplaceAll(binFile, "\\", "\\\\")

	llvmType := b.GetLLVMType(targetType)
	sizeBytes := pool.Types[targetType].TrueSizeBits / 8
	if sizeBytes == 0 {
		sizeBytes = 1
	}

	b.EmitLine("\ndeclare i8* @fopen(i8*, i8*)")
	b.EmitLine("declare i64 @fwrite(i8*, i64, i64, i8*)")
	b.EmitLine("declare i32 @fclose(i8*)")
	b.EmitLine("@.jit.mode = private unnamed_addr constant [3 x i8] c\"wb\\00\"")
	b.EmitLine("@.jit.file = private unnamed_addr constant [%d x i8] c\"%s\\00\"", len(safeBinFile)+1, safeBinFile)

	b.EmitLine("\ndefine i32 @main() {")
	b.EmitLine("entry:")

	// --- THE COMPTIME CLOSURE INJECTION ---
	currScope := envScope
	for currScope != nil {
		for name, sym := range currScope.Symbols {
			if sym.ComptimeData != nil {
				globalName := fmt.Sprintf("@.env.blob.%d", b.nextStringID)
				b.nextStringID++
				var hexBytes []string
				for _, byt := range sym.ComptimeData {
					hexBytes = append(hexBytes, fmt.Sprintf("i8 %d", byt))
				}
				b.StringConstants = append(b.StringConstants, fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] [%s]", globalName, len(sym.ComptimeData), strings.Join(hexBytes, ", ")))

				envLLVMType := b.GetLLVMType(sym.TypeID)
				allocReg := fmt.Sprintf("%%%s_env_%d", name, b.nextRegID)
				b.nextRegID++

				b.EmitLine("  %s = alloca %s", allocReg, envLLVMType)

				castID := b.nextRegID
				b.nextRegID++
				b.EmitLine("  %%cast_%d = bitcast [%d x i8]* %s to %s*", castID, len(sym.ComptimeData), globalName, envLLVMType)

				loadReg := b.NextReg()
				b.EmitLine("  %s = load %s, %s* %%cast_%d", loadReg, envLLVMType, envLLVMType, castID)
				b.EmitLine("  store %s %s, %s* %s", envLLVMType, loadReg, envLLVMType, allocReg)

				b.Locals[name] = allocReg
			}
		}
		currScope = currScope.Outer
	}

	valReg := b.emitExpression(expr)

	if valReg == "" {
		return nil, fmt.Errorf("comptime expression attempted to read a runtime variable.\n  Hint: Comptime assignments (:=) can only use literals, pure functions, or other comptime variables.\n  Change ':=' to '=' if you need runtime evaluation.")
	}

	b.EmitLine("  %%ptr = alloca %s", llvmType)
	b.EmitLine("  store %s %s, %s* %%ptr", llvmType, valReg, llvmType)

	b.EmitLine("  %%mode = getelementptr inbounds [3 x i8], [3 x i8]* @.jit.mode, i64 0, i64 0")
	b.EmitLine("  %%filename = getelementptr inbounds [%d x i8], [%d x i8]* @.jit.file, i64 0, i64 0", len(safeBinFile)+1, len(safeBinFile)+1)
	b.EmitLine("  %%file = call i8* @fopen(i8* %%filename, i8* %%mode)")
	b.EmitLine("  %%void_ptr = bitcast %s* %%ptr to i8*", llvmType)
	b.EmitLine("  call i64 @fwrite(i8* %%void_ptr, i64 1, i64 %d, i8* %%file)", sizeBytes)
	b.EmitLine("  call i32 @fclose(i8* %%file)")

	b.EmitLine("  ret i32 0")
	b.EmitLine("}")

	var content strings.Builder
	for _, sc := range b.StringConstants {
		content.WriteString(sc + "\n")
	}
	content.WriteString(b.Output.String())

	os.WriteFile(llFile, []byte(content.String()), 0644)
	defer os.Remove(llFile)
	defer os.Remove(exeFile)
	defer os.Remove(binFile)

	cmdCompile := exec.Command("clang", llFile, "-lm", "-o", exeFile, "-O0", "-Wno-override-module")
	if out, err := cmdCompile.CombinedOutput(); err != nil {
		var debugOut strings.Builder
		debugOut.WriteString(fmt.Sprintf("JIT clang compilation failed: %s\n", string(out)))
		debugOut.WriteString("--- JIT DEBUG DUMP ---\n")
		debugOut.WriteString("Injected Locals:\n")
		if len(b.Locals) == 0 {
			debugOut.WriteString("  (none)\n")
		}
		for k, v := range b.Locals {
			debugOut.WriteString(fmt.Sprintf("  %s -> %s\n", k, v))
		}
		debugOut.WriteString("\nGenerated LLVM:\n")
		debugOut.WriteString(content.String())
		debugOut.WriteString("----------------------\n")

		// THE FIX: Format String Protection
		return nil, fmt.Errorf("%s", debugOut.String())
	}

	cmdRun := exec.Command(exeFile)
	if err := cmdRun.Run(); err != nil {
		return nil, fmt.Errorf("JIT execution panicked")
	}

	return os.ReadFile(binFile)
}
