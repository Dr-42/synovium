# Synovium Language Roadmap

## Phase 1: Formalization & Ecosystem Tooling (✅ Complete)
The goal of this phase was to mathematically lock the syntax and build the editor ecosystem before writing the compiler.
- [x] Design language semantics and EBNF grammar.
- [x] Build Tree-sitter grammar (`grammar.js`).
- [x] Resolve GLR shift/reduce conflicts (e.g., struct initialization vs. blocks).
- [x] Compile WASM and C parser bindings.
- [x] Helix Integration (Syntax highlighting via `highlights.scm`).
- [-] Neovim Integration (Local treesitter parser injection).

## Phase 2: Compiler Frontend (🚧 In Progress)
Translating raw source text into a structured, strictly-typed Abstract Syntax Tree.
- [x] **Lexical Analysis (Lexer)**
  - [x] Map tokens, operators, and keywords.
  - [x] Handle strict numeral bases (hex, octal, binary, floats).
  - [x] Track line/column coordinates.
  - [x] Byte span tracking for diagnostic reporting.
  - [x] Lexer test suite.
- [ ] **Abstract Syntax Tree (AST)**
  - [ ] Define Go structs for Expressions, Statements, and Declarations.
  - [ ] Implement `Node` interfaces.
- [ ] **Syntactic Analysis (Parser)**
  - [ ] Pratt Parser (Top-Down Operator Precedence) for expressions.
  - [ ] Recursive Descent for statements and declarations.
  - [ ] Parse Error recovery (preventing cascading panic errors).
- [ ] **Diagnostics Engine**
  - [ ] Build a pretty-printer for errors (using spans to underline code in red).

## Phase 3: Semantic Analysis (The Brain)
Ensuring the structurally correct code actually makes logical sense.
- [ ] **Name Resolution & Symbol Tables**
  - [ ] Scope tracking (global, block, closure).
  - [ ] Variable shadowing rules.
- [ ] **Type Checker**
  - [ ] Type inference engine.
  - [ ] Strict type checking (preventing `1 + "hello"`).
  - [ ] Struct and Enum resolution.
- [ ] **Comptime Engine**
  - [ ] Implement AST rewriting and comptime function execution.

## Phase 4: Intermediate Representation (IR) & Optimization
*Note: Depending on the backend target, we might skip to an AST-walker first or generate a custom SSA (Static Single Assignment) form.*
- [ ] Lower AST into a flat, linear IR.
- [ ] Constant folding and dead-code elimination.

## Phase 5: Backend & Code Generation
- [ ] **Decision:** Choose compilation target (C, LLVM IR, Cranelift, or Go-Assembly).
- [ ] Generate target code from AST/IR.
- [ ] Build the CLI wrapper (`syn build`, `syn run`).

## Phase 6: Standard Library
- [ ] Memory allocator interface.
- [ ] Core types (Strings, Vectors, I/O).
