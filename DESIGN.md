# Synovium Architecture & Engineering Specification

**Document Status:** Act 1 (Go Bootstrapper) Finalized

**Architects:** Dr-42

**Target:** Act 2 (Standard Library & Userland) -> Act 3 (Self-Hosting)

Synovium is a Turing-complete systems programming language engineered for uncompromising performance, zero-cost abstractions, and highly predictable C-ABI native execution. The compiler is built upon the philosophy of **Adaptive Structuring**—favoring robust, fault-tolerant data pipelines over rigid chronological parsing, and embracing system stress-testing to refine structural integrity.

This document serves as the comprehensive engineering specification of the Synovium compiler architecture, detailing the mechanics developed during Act 1, the solutions to critical systems programming challenges, and the roadmap for Acts 2 and 3.

---

## 1. Core Engineering Philosophy

### 1.1 Adaptive Structuring

Compilers should not crash entirely upon encountering invalid states (especially during live editing). Synovium is designed to recover, isolate failures, and continue processing to maximize the extraction of valid metadata.

### 1.2 Trust the Developer (Safety via Structure, Not Friction)

Synovium rejects the steep cognitive friction of borrow checkers. It provides raw C-pointers for absolute hardware control but structurally mitigates memory leaks through **Zero-Cost RAII** (static `defer` unrolling).

### 1.3 Lean Core, Expansive Userland

The compiler primitives are strictly limited to necessary hardware and control-flow abstractions. High-level concepts (e.g., Slices, dynamic arrays, allocators) are intentionally excluded from the AST. Instead, they are built in userland (`std`) using Generics and Operator Overloading.

---

## 2. Syntactic & Semantic Pipeline (Act 1 Foundation)

Act 1 successfully established the entire front-to-back pipeline written in Go, acting as the bootstrapper for the language.

### 2.1 The Lexer & AST (Data-Rich Parsing)

The Synovium parser utilizes a custom Pratt parsing architecture designed to generate a highly resilient, metadata-rich Abstract Syntax Tree (AST).

- **Precise Span Tracking:** Every `ast.Node` implements `Span() lexer.Span`, tracking exact start and end byte offsets. This enables flawless error reporting and precise LSP integrations.
- **AST-Integrated Documentation:** Doc-comments (`///`) are not discarded. The Lexer harvests contiguous doc-comments and injects them as `*ast.DocComment` structs directly into their target declarations. This elevates documentation from text metadata to a mathematically addressable AST node.

### 2.2 Order-Independent Resolution (Semantic DAG)

Synovium completely eliminates the chronological parsing limits of C/C++ (no forward declarations, no header files).

- **Mechanic:** The compiler maps all global definitions (`struct`, `enum`, `fnc`, `impl`) as unresolved nodes. Edges are drawn based on type dependencies using **Kahn’s Algorithm** (Topological Sort).
- **Multi-File Auto-Loading:** The DAG integrates directly with the module loader (`ParseModule`). If a dependency is missing, the loader dynamically parses the external `.syn` file, injects the new AST declarations, and seamlessly merges the dependency sub-graphs.

---

## 3. Type System & Memory Semantics

### 3.1 Zero-Cost Generics & Monomorphization

Synovium implements strict monomorphization for generic types (`T: type`).

- **Mechanic:** When a generic function or struct is instantiated (e.g., `Vector3(f64)`), the `CloneNode` interface deeply duplicates the AST node, preserving all spans and doc-comments. The `TypePool` then stamps out a concrete structure and registers a deterministic, human-readable ABI name (e.g., `%Vector3_f64`).
- **Optimization:** Deterministic hash-consing prevents duplicate compilations. Ghost templates (`Mask == 0`) are statically stripped from the final LLVM IR emission.

### 3.2 Algebraic Data Types & Impl Blocks

- **Tagged Unions:** First-class Enums with varying payloads, heavily utilizing exhaustive `match` expressions for safe state unwrapping.
- **Method Binding:** Strict separation of data and logic. `struct` defines physical memory, while `impl` binds methods and operator overloads (`_op_add`, `_op_index`), which decay natively to C-functions taking a `*self` pointer.

### 3.3 Static Defer Unrolling (Zero-Cost RAII)

- **Mechanic:** The compiler maintains a compile-time `DeferStack` tracking lexical block depths. Upon encountering a `ret` or `brk` instruction, the Codegen engine statically unrolls and emits the exact required `defer` statements in LIFO order before executing the branch.
- **Result:** Highly predictable, zero-overhead memory cleanup.

### 3.4 Value-Yielding Control Flow

Control structures (`if`, `match`, `block`, `loop`) are expressions that evaluate to values.

- **Mechanic:** The compiler supports value-yielding labeled breaks (`brk \`outer 42`). `LoopContexts`track expected LLVM return types, target exit labels, and precise`DeferDepth`, enabling complex state-machine evaluations without mutable out-parameters.

---

## 4. Compile-Time Execution (Comptime)

Synovium rejects slow internal bytecode VMs (e.g., Zig's ZIR) in favor of hardware-speed native execution, triggered by the `:=` operator.

### 4.1 The Subprocess JIT

When a comptime expression is encountered, the compiler captures the lexical closure, spins up an isolated Clang/LLVM subprocess, compiles the logic natively, and executes it. The resulting memory blob is extracted and serialized directly into the host binary's `.data` section as a C-ABI struct.

### 4.2 The Inter-Process Pointer Trap

- **The Problem:** JITing expressions containing absolute memory addresses (e.g., string literal arrays) causes segmentation faults, as the pointers belong to the dead subprocess's temporary RAM.
- **The Solution (Pure Literal Bypass):** The Semantic Evaluator proactively scans comptime trees. If an expression contains C-ABI pointers but is a "Pure Literal" (lacking runtime logic), it dynamically bypasses the JIT. The AST node remains intact, allowing LLVM Codegen to safely allocate the pointers directly within the main process's memory space.

---

## 5. Codegen, Native Toolchain & Debugging

### 5.1 Native LLVM Type Coercion

Codegen handles seamless structural coercions for flawless C-FFI:

- Smart array-to-pointer decay.
- Native instructions for numeric promotion (`trunc`, `sext`, `bitcast`, `inttoptr`, `sitofp`).
- Direct external global binding (e.g., `@stderr`).

### 5.2 Source-Level Debugging (AOT & Comptime)

Developers must debug Synovium logic directly, not compiled `.ll` intermediate files.

- **DWARF Integration:** During LLVM IR generation, Codegen leverages the byte `Span` data tracked by the AST to emit DWARF debug metadata (`DICompileUnit`, `DISubprogram`, `DILocation`).
- **Direct Comptime Debugging:** Because the Comptime Subprocess JIT also utilizes the standard LLVM generation pipeline, DWARF metadata is attached to the temporary execution binaries. This means **developers can attach standard debuggers (GDB/LLDB) to directly step through comptime `:=` blocks** as they execute during the compilation phase, perfectly mapping back to the `.syn` source.

---

## 6. Language Server Protocol (LSP) Architecture

The Synovium LSP repurposes the core CLI compiler pipeline for real-time intelligence via a thread-safe, debounced Virtual File System (VFS).

### 6.1 Shattered AST Recovery

Live typing inherently shatters the AST (e.g., typing `player.`). To prevent intelligence blackout:

- **The Parser** never aborts on errors, returning the partially constructed AST to the Semantic Engine.
- **Textual Fallback:** If `findContextAtOffset` fails to locate the right-hand node, the LSP engine employs robust string parsing to extract the expression preceding the cursor, walks up the scope chain, and manually resolves the identifier against the `TypePool`.

### 6.2 Namespace vs. Instance Resolution

The autocomplete engine mathematically distinguishes between contexts:

- **Type Namespaces** (e.g., `State.` -> suggests Enum Variants / Static Functions).
- **Memory Instances** (e.g., `player.` -> suggests Struct Fields and non-operator Methods).
- **Global Purge:** The engine strictly filters out compiler artifacts, uninstantiated templates (`Mask == 0` rendered as `template`), and internal `.llvm` symbols to prevent scope pollution.

### 6.3 Mock-JIT Memory Alignment

Executing the Subprocess JIT on every keystroke causes massive latency and poisons memory maps with `<error>` constants when syntax is broken.

- **The Solution:** The LSP overrides the `JITCallback`. Instead of invoking Clang, it queries the `TypePool` for the expected type's `TrueSizeBits` and returns a dummy byte array of the exact structural size. This perfectly satisfies the Semantic Evaluator, maintaining stable memory layouts for Inlay Hints, Signature Help, and Semantic Tokens.

---

## 7. The Horizon: Act 2 & Act 3

With Act 1 (the Go Bootstrapper) finalized, the architecture scales outward.

### 7.1 Act 2: Standard Library & Userland (`std`)

- **Slices & Iterators:** Slices (`[T; :]`) will be implemented as generic standard library structs containing a `*T` pointer and `usize` length, hooked into language syntax via `_op_index` overloading.
- **Explicit Allocators:** Synovium will provide robust `Arena`, `Pool`, and `Heap` allocators in userland. The language will rely on explicit parameter passing for allocators to guarantee deterministic, highly visible memory origins.
- **The Module System (`inc`):** Expanding Kahn's DAG to handle cross-file boundaries, automatically resolving, namespacing, and linking external `.syn` imports seamlessly.

### 7.2 Act 3: Self-Hosting

Once `std` and the module loader are stabilized, the Synovium compiler will be systematically translated from Go into Synovium. The language will ingest its own lexer, parser, and semantic DAG, proving its Turing-completeness and officially establishing itself as an independent, self-compiling systems architecture.
