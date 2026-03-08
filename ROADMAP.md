# Synovium Language Roadmap

## Phase 1: Formalization & Ecosystem Tooling (✅ Complete)

The goal of this phase was to mathematically lock the syntax and build the editor ecosystem before writing the compiler.

- [x] Design language semantics and EBNF grammar.
- [x] Build Tree-sitter grammar (`grammar.js`).
- [x] Resolve GLR shift/reduce conflicts.
- [x] Compile WASM and C parser bindings.
- [x] Helix Integration (Syntax highlighting via `highlights.scm`).

## Phase 2: Compiler Frontend (🚧 In Progress)

Translating raw source text into a structured, strictly-typed Abstract Syntax Tree.

- [x] **Lexical Analysis (Lexer)**
  - [x] Map tokens, operators, and keywords.
- [x] **Syntactic Analysis (Parser)**
  - [x] Pratt Parser for expressions.
  - [x] Recursive Descent for statements and declarations.
- [ ] **Act 2 Syntactic Surgery**
  - [ ] Add bitwise assignment tokens (`&=`, `|=`, `^=`, `<<=`, `>>=`).
  - [ ] Remove `yld` keyword and AST nodes.
  - [ ] Implement `defer` statement parsing.
  - [ ] Implement named loops (`` `label loop ``) and value-carrying breaks (``brk `label <expr>``).

## Phase 3: Semantic Analysis & The DAG (🚧 In Progress)

Ensuring the structurally correct code actually makes logical sense.

- [x] Order-independent global declarations via Kahn's Topological Sort.
- [x] Zero-cost Generics and Monomorphization.
- [x] Subprocess JIT for comptime execution.
- [ ] **Act 2 Semantic Engine**
  - [ ] Deterministic File System DAG (`std.math.Vector3` auto-loading).
  - [ ] **Closure Boundary Isolation:** Upgrade `ExpectedReturnType` to a stack for safe lambda `ret` validation.
  - [ ] Block value unification for `brk <expr>` (ensuring all breaks in a loop yield the same type).
  - [ ] `MaskIsPure` trait enforcement (blocking outer-scope mutation).

## Phase 4: Backend & Memory Semantics (Act 2)

- [ ] Context-Bound Arenas (Implicit allocator passing for pure functions).
- [ ] Stack-based `defer` unrolling in LLVM Codegen.
- [ ] Dynamic Array Slicing (Fat Pointers).

## Phase 5: The Ecosystem Crucible

- [ ] Native C FFI via `std.inc_c("header.h")` comptime header parsing.
