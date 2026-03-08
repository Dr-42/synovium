package ast

import (
	"fmt"

	"synovium/lexer"
)

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

func (e *ExprStmt) stmtNode() {}

func (e *ExprStmt) Span() lexer.Span { return e.Value.Span() }

// ============================================================================
// EXPRESSIONS (Pratt Parser Targets)
// ============================================================================

type Identifier struct {
	Token lexer.Token // The IDENT token
	Value string
}

func (i *Identifier) exprNode() {}

func (i *Identifier) Span() lexer.Span { return i.Token.Span }

type IntLiteral struct {
	Token lexer.Token // The INT token
	Value int64       // We will parse the hex/octal strings into actual integers here
}

// Tell the AST that FunctionDecl can be used as an expression (lambda/nested)
func (f *FunctionDecl) exprNode() {}

// FunctionType maps to: fnc(i32, *Manager) = str
type FunctionType struct {
	Token      lexer.Token // The 'fnc' token
	Parameters []Type      // Note: These are just Types, not named Parameters!
	ReturnType Type        // Optional
	IsVariadic bool
	EndSpan    int
}

func (f *FunctionType) typeNode() {}

func (f *FunctionType) Span() lexer.Span {
	return lexer.Span{Start: f.Token.Span.Start, End: f.EndSpan}
}

func (i *IntLiteral) exprNode() {}

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
	GenericArgs []Type      // Optional
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

func (f *FloatLiteral) exprNode() {}

func (f *FloatLiteral) Span() lexer.Span { return f.Token.Span }

type StringLiteral struct {
	Token lexer.Token
	Value string
}

func (s *StringLiteral) exprNode() {}

func (s *StringLiteral) Span() lexer.Span { return s.Token.Span }

type CharLiteral struct {
	Token lexer.Token
	Value string
}

func (c *CharLiteral) exprNode() {}

func (c *CharLiteral) Span() lexer.Span { return c.Token.Span }

type BoolLiteral struct {
	Token lexer.Token
	Value bool
}

func (b *BoolLiteral) exprNode() {}

func (b *BoolLiteral) Span() lexer.Span { return b.Token.Span }

// ============================================================================
// TOP LEVEL DECLARATIONS
// ============================================================================

// FunctionDecl maps to: fnc <identifier> "(" <parameter_list>? ")" ( <return_op> <type> )? <block>
type FunctionDecl struct {
	Token      lexer.Token
	Name       *Identifier
	Parameters []*Parameter
	IsVariadic bool
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

func (p *Parameter) Span() lexer.Span {
	return lexer.Span{Start: p.Token.Span.Start, End: p.Type.Span().End}
}

// StructDecl maps to: struct <identifier> { <field_decl_list>? }
type StructDecl struct {
	Token         lexer.Token
	Name          *Identifier
	GenericParams []*Parameter
	Fields        []*FieldDecl
	EndSpan       int
}

func (s *StructDecl) declNode() {}

func (s *StructDecl) exprNode() {}

func (s *StructDecl) Span() lexer.Span { return lexer.Span{Start: s.Token.Span.Start, End: s.EndSpan} }

type FieldDecl struct {
	Token lexer.Token
	Name  *Identifier
	Type  Type
}

func (f *FieldDecl) Span() lexer.Span {
	return lexer.Span{Start: f.Name.Span().Start, End: f.Type.Span().End}
}

// EnumDecl maps to: enum <identifier> { <variant_list>? }
type EnumDecl struct {
	Token         lexer.Token
	Name          *Identifier
	GenericParams []*Parameter
	Variants      []*VariantDecl
	EndSpan       int
}

func (e *EnumDecl) declNode() {}

func (e *EnumDecl) exprNode() {}

func (e *EnumDecl) Span() lexer.Span { return lexer.Span{Start: e.Token.Span.Start, End: e.EndSpan} }

type VariantDecl struct {
	Token lexer.Token
	Name  *Identifier
	Types []Type // e.g., Running(f64, i32)
}

func (v *VariantDecl) Span() lexer.Span {
	return lexer.Span{Start: v.Name.Span().Start, End: v.Types[len(v.Types)-1].Span().End}
}

// ImplDecl maps to: impl <identifier> { <function_decl>* }
type ImplDecl struct {
	Token   lexer.Token
	Target  *Identifier
	Methods []*FunctionDecl
	EndSpan int
}

func (i *ImplDecl) declNode() {}

func (i *ImplDecl) stmtNode() {}

func (i *ImplDecl) Span() lexer.Span { return lexer.Span{Start: i.Token.Span.Start, End: i.EndSpan} }

// ============================================================================
// CONTROL FLOW & COMPLEX EXPRESSIONS
// ============================================================================

type IfExpr struct {
	Token      lexer.Token // The 'if' token
	Condition  Expr
	Body       *Block
	ElifConds  []Expr
	ElifBodies []*Block
	ElseBody   *Block
}

func (i *IfExpr) exprNode() {}

func (i *IfExpr) Span() lexer.Span {
	end := i.Body.Span().End
	if i.ElseBody != nil {
		end = i.ElseBody.Span().End
	} else if len(i.ElifBodies) > 0 {
		end = i.ElifBodies[len(i.ElifBodies)-1].Span().End
	}
	return lexer.Span{Start: i.Token.Span.Start, End: end}
}

type MatchExpr struct {
	Token   lexer.Token // The 'match' token
	Value   Expr
	Arms    []*MatchArm
	EndSpan int
}

func (m *MatchExpr) exprNode() {}

func (m *MatchExpr) Span() lexer.Span { return lexer.Span{Start: m.Token.Span.Start, End: m.EndSpan} }

type MatchArm struct {
	Token   lexer.Token
	Pattern *Identifier   // Simplified for now, can be expanded to full paths
	Params  []*Identifier // For e.g. Status.Running(speed)
	Body    *Block
}

func (m *MatchArm) Span() lexer.Span {
	return m.Body.Span()
}

type LoopExpr struct {
	Token     lexer.Token // The 'loop' token
	Condition Node        // Can be an Expr OR a VariableDecl (e.g., i: i32 = 0...10)
	Body      *Block
}

func (l *LoopExpr) exprNode() {}

func (l *LoopExpr) Span() lexer.Span {
	return lexer.Span{Start: l.Token.Span.Start, End: l.Body.Span().End}
}

type StructInitExpr struct {
	Token   lexer.Token // The '{' token
	Name    Expr        // <-- CHANGED: Now it can be an Identifier or CallExpr!
	Fields  []*StructInitField
	EndSpan int
}

func (s *StructInitExpr) exprNode() {}

func (s *StructInitExpr) Span() lexer.Span {
	return lexer.Span{Start: s.Token.Span.Start, End: s.EndSpan}
}

type StructInitField struct {
	Token lexer.Token // The '.' token
	Name  *Identifier
	Value Expr
	Type  Type
}

func (c *StructInitField) exprNode() {}

func (c *StructInitField) Span() lexer.Span {
	return lexer.Span{Start: c.Name.Span().Start, End: c.Value.Span().End}
}

type CastExpr struct {
	Token lexer.Token // The 'as' token
	Left  Expr
	Type  Type
}

func (c *CastExpr) exprNode() {}

func (c *CastExpr) Span() lexer.Span {
	return lexer.Span{Start: c.Left.Span().Start, End: c.Type.Span().End}
}

type BubbleExpr struct {
	Token lexer.Token // The '?' token
	Left  Expr
}

func (b *BubbleExpr) exprNode() {}

func (b *BubbleExpr) Span() lexer.Span {
	return lexer.Span{Start: b.Left.Span().Start, End: b.Token.Span.End}
}

// ============================================================================
// STATEMENTS
// ============================================================================

type ReturnStmt struct {
	Token lexer.Token // 'ret'
	Value Expr        // Optional
}

func (r *ReturnStmt) stmtNode() {}

func (r *ReturnStmt) Span() lexer.Span {
	if r.Value != nil {
		return lexer.Span{Start: r.Token.Span.Start, End: r.Value.Span().End}
	}
	return r.Token.Span
}

type YieldStmt struct {
	Token lexer.Token // 'yld'
	Value Expr        // Optional
}

func (y *YieldStmt) stmtNode() {}

func (y *YieldStmt) Span() lexer.Span {
	if y.Value != nil {
		return lexer.Span{Start: y.Token.Span.Start, End: y.Value.Span().End}
	}
	return y.Token.Span
}

type BreakStmt struct {
	Token lexer.Token // 'brk'
}

func (b *BreakStmt) stmtNode() {}

func (b *BreakStmt) Span() lexer.Span { return b.Token.Span }

type ArrayInitExpr struct {
	Token    lexer.Token // The '[' token
	Elements []Expr
	Count    Expr
	EndSpan  int
}

func (a *ArrayInitExpr) exprNode() {}

func (a *ArrayInitExpr) Span() lexer.Span {
	return lexer.Span{Start: a.Token.Span.Start, End: a.EndSpan}
}

// --- COMPTIME ---
type ComptimeBlob struct {
	Token      lexer.Token
	Type       int // Will hold TypeID
	Data       []byte
	SourceCode string
}

func (cb *ComptimeBlob) exprNode() {}

func (cb *ComptimeBlob) Span() lexer.Span { return cb.Token.Span }

func (cb *ComptimeBlob) String() string {
	return fmt.Sprintf("<comptime blob: %d bytes>", len(cb.Data))
}
