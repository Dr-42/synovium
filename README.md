# Synovium (`.syn`) - v0.0.1

Synovium is a fast, statically-typed, expression-oriented systems programming language.

Built on the philosophy of **Adaptive Structuring**, it is designed to offer the low-level memory control of C, the compile-time metaprogramming of Zig, and the modern ergonomics of Rust—all powered by a custom LLVM IR backend.

## 🚀 Features (v0.0.1)

- **Expression-Oriented Control Flow:** Everything is an expression. Blocks evaluate to their final expression without needing a `ret` keyword.
- **Order-Independent Architecture:** A Kahn's Algorithm DAG resolves all top-level symbols, allowing functions and structs to be declared in any order without C-style forward declarations.
- **Comptime JIT (`:=`):** Generic closures and complex initializations are compiled to native LLVM in the background and serialized directly into the host `.data` section before the main binary is built.
- **Zero-Cost Generics:** Generic functions, structs, and enums are monomorphized at compile-time into highly optimized, deterministic LLVM structures.
- **Tagged Unions & Pattern Matching:** First-class support for algebraic data types (`enum`) with exhaustive pattern matching.
- **Operator Overloading:** Define mathematical behaviors for custom structs natively inside `impl` blocks.
- **Syntactic Minimality:** The entire language operates on just 16 reserved keywords.

## 🔬 Syntax Examples

### Comptime Subprocess JIT

Synovium evaluates deterministic logic at compile time, storing only the raw bytes in the final executable.

```synovium
// The JIT evaluates this at compile-time and writes it directly to the .data section.
opt_val : Option(i32) := Option(i32).Some(999);

```

### Error Handling & Bubbling (`?`)

Synovium treats errors as values using generic `Result` and `Option` enums. The `?` operator eliminates nested `if/match` blocks.

```synovium
fnc process(a: i32, b: i32) = Result(i32, str) {
    // If division fails, `?` instantly returns the Err(str) to the caller.
    // If it succeeds, it unboxes the `i32` directly into `step1`.
    step1: i32 = safe_divide(a, b)?;
    ret Result(i32, str).Ok(step1 + 100);
}

```

### Operator Overloading & Auto-Coercion

Define standard operators on structs. The compiler automatically handles symmetrical pointer referencing and dereferencing at the call site.

```synovium
struct Vec { x: f64, y: f64 }

impl Vec {
    fnc +(self: *Vec, other: *Vec) = Vec {
        ret Vec{ .x = self.x + other.x, .y = self.y + other.y };
    }
}

```

## 🏗️ Compiler Architecture

The Synovium compiler is written in Go (`go 1.24.10`) and operates in distinct stages:

1. **Lexical Analysis:** Byte-span accurate tokenization.
2. **Syntactic Analysis (Pratt Parser):** Top-down operator precedence parsing.
3. **DAG Resolution:** Topological sorting allows out-of-order declarations.
4. **Semantic Analysis:** Strict type-checking, closure isolation, and comptime monomorphization.
5. **TAST Annotation:** The Abstract Syntax Tree is stamped with resolved `TypeID` memory layouts.
6. **LLVM Codegen:** The AST is lowered directly into LLVM Intermediate Representation.
7. **Native Compilation:** Clang wraps the LLVM IR and links it into a native OS executable.

## 🌅 The Horizon (Act 2 Roadmap)

Synovium is currently transitioning into Act 2, focusing on massive parallelization and ecosystem integration:

- **Deterministic File DAG:** Auto-loading namespaces via the file system (e.g., `std.math.Vector3`) without reserved import keywords.
- **Context-Bound Arenas:** Implicit allocator passing for mathematically pure functions, enabling automatic GPU/SIMD parallelization.
- **Native C FFI:** Seamless integration with 50 years of C ecosystem via `std.inc_c("header.h")`.
- **Targeted Control Flow:** Value-yielding labeled loops (`brk `label <expr>`) and block-scoped `defer` mechanics.
