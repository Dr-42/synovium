# Synovium Language Roadmap

## Phase 1: Formalization & Ecosystem Tooling (✅ Complete)

The goal of this phase was to mathematically lock the syntax and build the editor ecosystem before writing the compiler.

- [x] Design language semantics and EBNF grammar.
- [x] Build Tree-sitter grammar (`grammar.js`).
- [x] Resolve GLR shift/reduce conflicts.

## Phase 2: Compiler Frontend (✅ Complete)

Translating raw source text into a structured, strictly-typed Abstract Syntax Tree.

- [x] **Lexical Analysis (Lexer)**
  - [x] Map tokens, operators, and keywords.
- [x] **Syntactic Analysis (Parser)**
  - [x] Pratt Parser for expressions.
  - [x] Recursive Descent for statements and declarations.
- [x] **Act 2 Syntactic Surgery**
  - [x] Add bitwise assignment tokens (`&=`, `|=`, `^=`, `<<=`, `>>=`).
  - [x] Remove `yld` keyword and AST nodes.
  - [x] Implement `defer` statement parsing.
  - [x] Implement named loops (`` `label loop ``) and value-carrying breaks (``brk `label <expr>``).

## Phase 3: Semantic Analysis & The DAG (✅ Complete)

Ensuring the structurally correct code actually makes logical sense.

- [x] Order-independent global declarations via Kahn's Topological Sort.
- [x] Zero-cost Generics and Monomorphization.
- [x] Subprocess JIT for comptime execution.
- [x] Closure Boundary Isolation (safe lambda `ret` validation).
- [x] Block value unification for `brk <expr>`.
- [x] Ergonomic C ABI Naming for generic instantiations (e.g. `%Result_template_f64_str`).
- [x] **Deterministic File System DAG**: Auto-loading via the file system without `import`.
- [x] **Comptime Autopruning**: Dead code elimination prior to LLVM generation.

## Phase 4: Backend & Memory Semantics (🚧 In Progress)

- [x] Stack-based `defer` unrolling in LLVM Codegen.
- [ ] Context-Bound Arenas (Implicit allocator passing for pure functions).
- [ ] Dynamic Array Slicing (Fat Pointers).

## Phase 5: The Crucible - Microtesting & Hardening (🔥 Active Phase)

Subjecting the foundation to extreme edge cases before expanding the ecosystem.

- [ ] Deeply nested block bubbling and `brk` routing validation.
- [ ] Complex generic permutation stress tests (Structs containing Enums containing Generics).
- [ ] Lexical Scope shadowing and memory-leak tests within the Comptime JIT.
- [ ] Pointer-to-Array decay edge cases in variadic FFI boundaries.

## Phase 6: The Ecosystem Integration (Transition to Act 3)

- [ ] Native C FFI via `std.inc_c("header.h")` comptime header parsing.
- [ ] Standard Library bootstrapping (`std.io`, `std.mem`).
- [ ] Shift `MaskIsPure` trait enforcement to the self-hosted compiler.
