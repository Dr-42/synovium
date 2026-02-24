package ast

import "synovium/lexer"

// ============================================================================
// BASE INTERFACES
// ============================================================================

// Node is the root interface for everything in the AST.
// Every node must know exactly where it lives in the source code.
type Node interface {
	Span() lexer.Span
}

// Expr represents any node that produces a value (e.g., 1 + 2, func_call(), block).
type Expr interface {
	Node
	exprNode() // Dummy method to enforce interface satisfaction
}

// Stmt represents any node that executes an action but produces no value (e.g., brk;, ret;).
type Stmt interface {
	Node
	stmtNode() // Dummy method to enforce interface satisfaction
}

// Decl represents top-level definitions (e.g., fnc, struct, impl).
type Decl interface {
	Node
	declNode() // Dummy method to enforce interface satisfaction
}

// ============================================================================
// PROGRAM & BLOCKS
// ============================================================================

// SourceFile is the root node of your entire parsed file.
type SourceFile struct {
	Declarations []Decl
}

func (s *SourceFile) Span() lexer.Span {
	if len(s.Declarations) > 0 {
		return lexer.Span{
			Start: s.Declarations[0].Span().Start,
			End:   s.Declarations[len(s.Declarations)-1].Span().End,
		}
	}
	return lexer.Span{Start: 0, End: 0}
}

// Block maps to: <block> ::= "{" <statement>* <expression>? "}"
// Because of Synovium's bubbling return feature, blocks are Expressions.
type Block struct {
	Token      lexer.Token // The '{' token
	Statements []Stmt
	Value      Expr       // The optional bubbling expression at the end
	CloseSpan  lexer.Span // We need the '}' span to calculate the full block span
}

func (b *Block) exprNode() {}
func (b *Block) Span() lexer.Span {
	return lexer.Span{Start: b.Token.Span.Start, End: b.CloseSpan.End}
}

// ============================================================================
// DECLARATIONS & STATEMENTS
// ============================================================================

// VariableDecl maps to: <variable_decl> ::= <identifier> ":" <type> <assign_op> <expression>
// It can act as a Statement when followed by a semicolon.
type VariableDecl struct {
	Token    lexer.Token // The identifier token
	Name     *Identifier
	Type     *TypeNode
	Operator string // '=', '~=', or ':='
	Value    Expr
}

func (v *VariableDecl) stmtNode() {}
func (v *VariableDecl) declNode() {}
func (v *VariableDecl) Span() lexer.Span {
	return lexer.Span{Start: v.Name.Span().Start, End: v.Value.Span().End}
}

// ExprStmt wraps an expression so it can sit legally in a statement list (e.g., `1 + 1;`)
type ExprStmt struct {
	Token lexer.Token // The first token of the expression
	Value Expr
}

func (e *ExprStmt) stmtNode()        {}
func (e *ExprStmt) Span() lexer.Span { return e.Value.Span() }

// ============================================================================
// EXPRESSIONS (Pratt Parser Targets)
// ============================================================================

type Identifier struct {
	Token lexer.Token // The IDENT token
	Value string
}

func (i *Identifier) exprNode()        {}
func (i *Identifier) Span() lexer.Span { return i.Token.Span }

type IntLiteral struct {
	Token lexer.Token // The INT token
	Value int64       // We will parse the hex/octal strings into actual integers here
}

func (i *IntLiteral) exprNode()        {}
func (i *IntLiteral) Span() lexer.Span { return i.Token.Span }

// PrefixExpr handles: "!" | "~" | "-" | "*" | "&"
type PrefixExpr struct {
	Token    lexer.Token // The operator token, e.g. '-'
	Operator string
	Right    Expr
}

func (p *PrefixExpr) exprNode() {}
func (p *PrefixExpr) Span() lexer.Span {
	return lexer.Span{Start: p.Token.Span.Start, End: p.Right.Span().End}
}

// InfixExpr handles: + - * / % == != > < >= <= && ||
type InfixExpr struct {
	Token    lexer.Token // The operator token
	Left     Expr
	Operator string
	Right    Expr
}

func (i *InfixExpr) exprNode() {}
func (i *InfixExpr) Span() lexer.Span {
	return lexer.Span{Start: i.Left.Span().Start, End: i.Right.Span().End}
}

// ============================================================================
// TYPES
// ============================================================================

// TypeNode represents a parsed type signature like `*&std.Vec3` or `[i32; 10]`
type TypeNode struct {
	Token lexer.Token
	Value string // Simplified for now, we will expand this to handle arrays/pointers
}

func (t *TypeNode) Span() lexer.Span { return t.Token.Span }
