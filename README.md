# Synovium (`.syn`)

Synovium is a fast, statically-typed, expression-oriented systems programming language.

It is designed to offer the low-level memory control of C, the compile-time metaprogramming of Zig, and the modern ergonomics of Rust—all powered by a custom LLVM IR backend.

## Features

- **Expression-Oriented:** Everything is an expression. Blocks evaluate to their final expression without needing a `ret` keyword.
- **Zero-Cost Generics:** Generic functions, structs, and enums are monomorphized at compile-time into highly optimized, concrete LLVM structures.
- **Tagged Unions & Pattern Matching:** First-class support for algebraic data types (`enum`) with exhaustive pattern matching.
- **Automatic Error Bubbling:** The postfix `?` operator automatically unboxes `Ok`/`Some` variants or seamlessly returns `Err`/`None` up the call stack.
- **Operator Overloading:** Define mathematical behaviors for custom structs without relying on magic strings.
- **Direct C FFI:** Seamlessly link against `libc` and native system libraries with zero overhead.

## Syntax Examples

### Error Handling & Bubbling (`?`)

Synovium treats errors as values using generic `Result` and `Option` enums. The `?` operator eliminates nested `if/match` blocks.

```synovium
fnc printf(fmt: str, ...);

enum Result(T: type, E: type) {
    Ok(T),
    Err(E)
}

fnc safe_divide(num: i32, denom: i32) = Result(i32, str) {
    if denom == 0 {
        ret Result(i32, str).Err("Division by zero!");
    }
    ret Result(i32, str).Ok(num / denom);
}

fnc process(a: i32, b: i32, c: i32) = Result(i32, str) {
    // If division fails, `?` instantly returns the Err(str) to the caller.
    // If it succeeds, it unboxes the `i32` directly into `step1`.
    step1: i32 = safe_divide(a, b)?;
    step2: i32 = safe_divide(step1, c)?;

    ret Result(i32, str).Ok(step2 + 100);
}

```

### Operator Overloading & Auto-Coercion

Define standard operators on structs natively. The compiler automatically handles symmetrical pointer referencing and dereferencing at the call site.

```synovium
struct Vec { x: f64, y: f64 }

impl Vec {
    // Defined using pointers for memory efficiency
    fnc +(self: *Vec, other: *Vec) = Vec {
        ret Vec{ .x = self.x + other.x, .y = self.y + other.y };
    }
}

fnc main() = i32 {
    a: Vec = Vec{ .x = 2.0, .y = 1.0 };
    b: Vec = Vec{ .x = 1.0, .y = 3.0 };

    // Passed by value; the compiler auto-references them based on the signature!
    c: Vec = a + b;
    ret 0;
}

```

### Control Flow & Bubbling

Blocks yield values. `loop` and `if` are expressions.

```synovium
fnc main() = i32 {
    health : f64 ~= 100.0;
    damage : f64 = 40.0;

    // Block bubbling
    status : str = if health - damage <= 0.0 {
        "Dead"
    } else {
        health -= damage;
        "Alive"
    };

    ret 0;
}

```

## Architecture

The Synovium compiler is written in Go and operates in 8 distinct stages:

1. **Lexical Analysis:** Byte-span accurate tokenization.
2. **Syntactic Analysis (Pratt Parser):** Top-down operator precedence parsing.
3. **DAG Resolution:** Kahn's topological sort allows out-of-order top-level declarations.
4. **Semantic Analysis:** Strict type-checking, implicit primitive promotion, and comptime monomorphization.
5. **TAST Annotation:** The Abstract Syntax Tree is stamped with resolved `TypeID` memory layouts.
6. **LLVM Codegen:** The AST is walked and lowered directly into LLVM Intermediate Representation.
7. **Native Compilation:** Clang wraps the LLVM IR and links it into a native OS executable.

## Building the Compiler

Requires Go 1.24+ and Clang/LLVM.

```bash
go build -o synovium main.go
./synovium myscript.syn

```
