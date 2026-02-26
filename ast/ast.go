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
	Type     Type
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

// Type is a specialized interface for type signatures so we can't accidentally
// assign an Expression where a Type is expected.
type Type interface {
	Node
	typeNode()
}

// NamedType handles intrinsics (i32, str) and structs/namespaces (std.Vec3)
type NamedType struct {
	Token       lexer.Token // The first identifier token
	Name        string      // The full name (e.g., "i32" or "std.Vec3")
	IsIntrinsic bool        // True if it's one of Synovium's built-in types
	EndSpan     int         // We track the end byte for namespaces
}

func (n *NamedType) typeNode() {}
func (n *NamedType) Span() lexer.Span {
	return lexer.Span{Start: n.Token.Span.Start, End: n.EndSpan}
}

// PointerType handles '*' pointers
type PointerType struct {
	Token lexer.Token // The '*' token
	Base  Type
}

func (p *PointerType) typeNode() {}
func (p *PointerType) Span() lexer.Span {
	return lexer.Span{Start: p.Token.Span.Start, End: p.Base.Span().End}
}

// ReferenceType handles '&' references
type ReferenceType struct {
	Token lexer.Token // The '&' token
	Base  Type
}

func (r *ReferenceType) typeNode() {}
func (r *ReferenceType) Span() lexer.Span {
	return lexer.Span{Start: r.Token.Span.Start, End: r.Base.Span().End}
}

// ArrayType handles arrays [i32; 10] and slices [i32; :]
type ArrayType struct {
	Token   lexer.Token // The '[' token
	Base    Type
	Size    Expr // The capacity expression (nil if it's a slice)
	IsSlice bool // True if the size was ':'
	EndSpan int  // The ']' token end byte
}

func (a *ArrayType) typeNode() {}
func (a *ArrayType) Span() lexer.Span {
	return lexer.Span{Start: a.Token.Span.Start, End: a.EndSpan}
}

// ============================================================================
// POSTFIX EXPRESSIONS
// ============================================================================

// CallExpr represents `leftExp(arg1, arg2)`
type CallExpr struct {
	Token     lexer.Token // The '(' token
	Function  Expr        // Can be an Identifier, FieldAccess, or even another CallExpr
	Arguments []Expr
	EndSpan   int // The ')' token end byte
}

func (c *CallExpr) exprNode() {}
func (c *CallExpr) Span() lexer.Span {
	return lexer.Span{Start: c.Function.Span().Start, End: c.EndSpan}
}

// FieldAccessExpr represents `leftExp.identifier`
type FieldAccessExpr struct {
	Token lexer.Token // The '.' token
	Left  Expr
	Field *Identifier
}

func (f *FieldAccessExpr) exprNode() {}
func (f *FieldAccessExpr) Span() lexer.Span {
	return lexer.Span{Start: f.Left.Span().Start, End: f.Field.Span().End}
}

// IndexExpr represents `leftExp[index]` or `leftExp[start...end]`
type IndexExpr struct {
	Token   lexer.Token // The '[' token
	Left    Expr
	Index   Expr // Can be a standard Expr or a RangeExpr
	EndSpan int  // The ']' token end byte
}

func (i *IndexExpr) exprNode() {}
func (i *IndexExpr) Span() lexer.Span {
	return lexer.Span{Start: i.Left.Span().Start, End: i.EndSpan}
}

// ============================================================================
// ADDITIONAL LITERALS
// ============================================================================

type FloatLiteral struct {
	Token lexer.Token
	Value string // Keeping as string to retain exact formatting before backend compilation
}

func (f *FloatLiteral) exprNode()        {}
func (f *FloatLiteral) Span() lexer.Span { return f.Token.Span }

type StringLiteral struct {
	Token lexer.Token
	Value string
}

func (s *StringLiteral) exprNode()        {}
func (s *StringLiteral) Span() lexer.Span { return s.Token.Span }

type CharLiteral struct {
	Token lexer.Token
	Value string
}

func (c *CharLiteral) exprNode()        {}
func (c *CharLiteral) Span() lexer.Span { return c.Token.Span }

type BoolLiteral struct {
	Token lexer.Token
	Value bool
}

func (b *BoolLiteral) exprNode()        {}
func (b *BoolLiteral) Span() lexer.Span { return b.Token.Span }

// ============================================================================
// TOP LEVEL DECLARATIONS
// ============================================================================

// FunctionDecl maps to: fnc <identifier> "(" <parameter_list>? ")" ( <return_op> <type> )? <block>
type FunctionDecl struct {
	Token      lexer.Token
	Name       *Identifier
	Parameters []*Parameter
	ReturnOp   string // "=" or ":="
	ReturnType Type   // Can be nil
	Body       *Block
}

func (f *FunctionDecl) declNode() {}
func (f *FunctionDecl) Span() lexer.Span {
	return lexer.Span{Start: f.Token.Span.Start, End: f.Body.Span().End}
}

type Parameter struct {
	Token lexer.Token
	Name  *Identifier
	Type  Type
}

// StructDecl maps to: struct <identifier> { <field_decl_list>? }
type StructDecl struct {
	Token   lexer.Token
	Name    *Identifier
	Fields  []*FieldDecl
	EndSpan int
}

func (s *StructDecl) declNode()        {}
func (s *StructDecl) Span() lexer.Span { return lexer.Span{Start: s.Token.Span.Start, End: s.EndSpan} }

type FieldDecl struct {
	Token lexer.Token
	Name  *Identifier
	Type  Type
}

// EnumDecl maps to: enum <identifier> { <variant_list>? }
type EnumDecl struct {
	Token    lexer.Token
	Name     *Identifier
	Variants []*VariantDecl
	EndSpan  int
}

func (e *EnumDecl) declNode()        {}
func (e *EnumDecl) Span() lexer.Span { return lexer.Span{Start: e.Token.Span.Start, End: e.EndSpan} }

type VariantDecl struct {
	Token lexer.Token
	Name  *Identifier
	Types []Type // e.g., Running(f64, i32)
}

// ImplDecl maps to: impl <identifier> { <function_decl>* }
type ImplDecl struct {
	Token   lexer.Token
	Target  *Identifier
	Methods []*FunctionDecl
	EndSpan int
}

func (i *ImplDecl) declNode()        {}
func (i *ImplDecl) Span() lexer.Span { return lexer.Span{Start: i.Token.Span.Start, End: i.EndSpan} }
