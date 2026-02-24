package parser

import (
	"fmt"
	"synovium/ast"
	"synovium/lexer"
)

// Precedences (1 to 16 matching your EBNF exactly)
const (
	_ int = iota
	LOWEST
	ASSIGN         // = or ~= or :=
	RANGE          // ...
	LOGICAL_OR     // ||
	LOGICAL_AND    // &&
	BITWISE_OR     // |
	BITWISE_XOR    // ^
	BITWISE_AND    // &
	EQUALITY       // == or !=
	RELATIONAL     // <, >, <=, >=
	SHIFT          // << or >>
	ADDITIVE       // + or -
	MULTIPLICATIVE // *, /, or %
	CAST           // as
	UNARY          // !, ~, -, *, & (Prefix)
	POSTFIX        // obj.field, func(), arr[], ?
	TYPE_DOT       // std.String
)

var precedences = map[lexer.TokenType]int{
	lexer.ASSIGN:      ASSIGN,
	lexer.MUT_ASSIGN:  ASSIGN,
	lexer.DECL_ASSIGN: ASSIGN, // Added := to precedence
	lexer.RANGE:       RANGE,
	lexer.OR:          LOGICAL_OR,
	lexer.AND:         LOGICAL_AND,
	lexer.PIPE:        BITWISE_OR,
	lexer.CARET:       BITWISE_XOR,
	lexer.AMPERS:      BITWISE_AND,
	lexer.EQ:          EQUALITY,
	lexer.NOT_EQ:      EQUALITY,
	lexer.LT:          RELATIONAL,
	lexer.LTE:         RELATIONAL,
	lexer.GT:          RELATIONAL,
	lexer.GTE:         RELATIONAL,
	lexer.LSHIFT:      SHIFT,
	lexer.RSHIFT:      SHIFT,
	lexer.PLUS:        ADDITIVE,
	lexer.MINUS:       ADDITIVE,
	lexer.ASTERISK:    MULTIPLICATIVE,
	lexer.SLASH:       MULTIPLICATIVE,
	lexer.MOD:         MULTIPLICATIVE,
	lexer.AS:          CAST,
	lexer.LPAREN:      POSTFIX, // Function calls
	lexer.LBRACKET:    POSTFIX, // Array indexing
	lexer.DOT:         POSTFIX, // Field access
	lexer.QUESTION:    POSTFIX, // Bubbling operator
}

type prefixParseFn func() ast.Expr
type infixParseFn func(ast.Expr) ast.Expr

type Parser struct {
	l      *lexer.Lexer
	errors []string

	curToken  lexer.Token
	peekToken lexer.Token

	prefixParseFns map[lexer.TokenType]prefixParseFn
	infixParseFns  map[lexer.TokenType]infixParseFn
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}

	p.prefixParseFns = make(map[lexer.TokenType]prefixParseFn)
	p.infixParseFns = make(map[lexer.TokenType]infixParseFn)

	// --- PREFIX REGISTRATIONS (Primary Expressions & Unary) ---
	p.registerPrefix(lexer.IDENT, p.parseIdentifier)
	p.registerPrefix(lexer.INT, p.parseIntLiteral)
	// (You can add Float, String, Char, Bool literals here easily)

	// Unary operators
	p.registerPrefix(lexer.BANG, p.parsePrefixExpression)
	p.registerPrefix(lexer.TILDE, p.parsePrefixExpression)
	p.registerPrefix(lexer.MINUS, p.parsePrefixExpression)
	p.registerPrefix(lexer.ASTERISK, p.parsePrefixExpression)
	p.registerPrefix(lexer.AMPERS, p.parsePrefixExpression)

	// Grouping & Control Flow
	p.registerPrefix(lexer.LPAREN, p.parseGroupedExpression)
	p.registerPrefix(lexer.IF, p.parseIfExpression)
	p.registerPrefix(lexer.MATCH, p.parseMatchExpression)
	p.registerPrefix(lexer.LOOP, p.parseLoopExpression)
	p.registerPrefix(lexer.LBRACE, p.parseBlockExpression) // Naked blocks

	// --- INFIX REGISTRATIONS (Binary Math, Logic, Postfix) ---
	infixes := []lexer.TokenType{
		lexer.PLUS, lexer.MINUS, lexer.ASTERISK, lexer.SLASH, lexer.MOD,
		lexer.EQ, lexer.NOT_EQ, lexer.LT, lexer.LTE, lexer.GT, lexer.GTE,
		lexer.AND, lexer.OR, lexer.PIPE, lexer.CARET, lexer.AMPERS,
		lexer.LSHIFT, lexer.RSHIFT, lexer.RANGE, lexer.ASSIGN, lexer.MUT_ASSIGN,
	}
	for _, t := range infixes {
		p.registerInfix(t, p.parseInfixExpression)
	}

	// Postfix / Structural
	p.registerInfix(lexer.LPAREN, p.parseCallExpression)
	p.registerInfix(lexer.DOT, p.parseFieldAccess)
	p.registerInfix(lexer.LBRACKET, p.parseIndexExpression)

	p.nextToken()
	p.nextToken()

	return p
}

// ============================================================================
// TOP LEVEL: RECURSIVE DESCENT ROUTERS
// ============================================================================

// ParseSourceFile is the main entry point ( <source_file> ::= <declaration>* )
func (p *Parser) ParseSourceFile() *ast.SourceFile {
	program := &ast.SourceFile{}
	program.Declarations = []ast.Decl{}

	for p.curToken.Type != lexer.EOF {
		decl := p.parseDeclaration()
		if decl != nil {
			program.Declarations = append(program.Declarations, decl)
		}
		p.nextToken()
	}

	return program
}

// parseDeclaration routes to the correct top-level struct
func (p *Parser) parseDeclaration() ast.Decl {
	switch p.curToken.Type {
	case lexer.STRUCT:
		return p.parseStructDecl()
	case lexer.ENUM:
		return p.parseEnumDecl()
	case lexer.IMPL:
		return p.parseImplDecl()
	case lexer.FNC:
		return p.parseFunctionDecl()
	case lexer.IDENT:
		// If it's an IDENT followed by a COLON, it's a variable declaration: `x : i32 = 5;`
		if p.peekTokenIs(lexer.COLON) {
			return p.parseVariableDecl()
		}
		// Otherwise, it's an illegal top-level statement (handled by error reporter)
		fallthrough
	default:
		p.errors = append(p.errors, fmt.Sprintf("illegal top-level declaration starting with %s", p.curToken.Literal))
		return nil
	}
}

// parseStatement handles block-level execution
func (p *Parser) parseStatement() ast.Stmt {
	switch p.curToken.Type {
	case lexer.IDENT:
		if p.peekTokenIs(lexer.COLON) {
			return p.parseVariableDeclStmt()
		}
		return p.parseExpressionStatement()
	case lexer.RET:
		return p.parseReturnStatement()
	case lexer.YLD:
		return p.parseYieldStatement()
	case lexer.BRK:
		return p.parseBreakStatement()
	default:
		return p.parseExpressionStatement()
	}
}

// ============================================================================
// THE PRATT EXPRESSION ENGINE
// ============================================================================

func (p *Parser) parseExpression(precedence int) ast.Expr {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.noPrefixParseFnError(p.curToken.Type)
		return nil
	}
	leftExp := prefix()

	for !p.peekTokenIs(lexer.SEMICOLON) && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}

		p.nextToken()
		leftExp = infix(leftExp)
	}

	return leftExp
}

// --- PREFIX IMPLEMENTATIONS ---

func (p *Parser) parseIdentifier() ast.Expr {
	// Lookahead: If it's `Vec3 {`, it's a struct initialization, not just an identifier!
	if p.peekTokenIs(lexer.LBRACE) {
		return p.parseStructInitExpression()
	}
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseIntLiteral() ast.Expr {
	return &ast.IntLiteral{Token: p.curToken}
}

func (p *Parser) parsePrefixExpression() ast.Expr {
	expr := &ast.PrefixExpr{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
	}

	p.nextToken()
	expr.Right = p.parseExpression(UNARY)
	return expr
}

func (p *Parser) parseGroupedExpression() ast.Expr {
	p.nextToken()
	exp := p.parseExpression(LOWEST)

	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}
	return exp
}

// --- INFIX IMPLEMENTATIONS ---

func (p *Parser) parseInfixExpression(left ast.Expr) ast.Expr {
	expr := &ast.InfixExpr{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
		Left:     left,
	}

	precedence := p.curPrecedence()
	p.nextToken()

	// If it's an assignment or range (Right-Associative or specific left bind), we might adjust precedence here,
	// but standard Pratt handles left-associative natively by passing the current precedence.
	if expr.Operator == "=" || expr.Operator == "~=" {
		expr.Right = p.parseExpression(precedence - 1) // Right associativity trick
	} else {
		expr.Right = p.parseExpression(precedence)
	}

	return expr
}

// parseCallExpression handles `leftExp(args...)`
func (p *Parser) parseCallExpression(left ast.Expr) ast.Expr {
	// You will implement argument parsing here (looping until RPAREN)
	// Example struct to add to ast.go: ast.CallExpr{Function: left, Arguments: []Expr}
	return nil
}

func (p *Parser) parseFieldAccess(left ast.Expr) ast.Expr {
	// Handles `leftExp.identifier`
	return nil
}

func (p *Parser) parseIndexExpression(left ast.Expr) ast.Expr {
	// Handles `leftExp[index]` or `leftExp[start...end]`
	return nil
}

// ============================================================================
// STUBS FOR RECURSIVE DESCENT (Fill these out!)
// ============================================================================
func (p *Parser) parseVariableDecl() ast.Decl { return nil }
func (p *Parser) parseStructDecl() ast.Decl   { return nil }
func (p *Parser) parseEnumDecl() ast.Decl     { return nil }
func (p *Parser) parseImplDecl() ast.Decl     { return nil }
func (p *Parser) parseFunctionDecl() ast.Decl { return nil }

func (p *Parser) parseVariableDeclStmt() ast.Stmt { return nil }
func (p *Parser) parseReturnStatement() ast.Stmt  { return nil }
func (p *Parser) parseYieldStatement() ast.Stmt   { return nil }
func (p *Parser) parseBreakStatement() ast.Stmt   { return nil }
func (p *Parser) parseExpressionStatement() ast.Stmt {
	// Example of a completed statement parser
	stmt := &ast.ExprStmt{Token: p.curToken}
	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseIfExpression() ast.Expr         { return nil }
func (p *Parser) parseMatchExpression() ast.Expr      { return nil }
func (p *Parser) parseLoopExpression() ast.Expr       { return nil }
func (p *Parser) parseBlockExpression() ast.Expr      { return nil }
func (p *Parser) parseStructInitExpression() ast.Expr { return nil }

// --- Utility Functions ---
func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}
func (p *Parser) peekTokenIs(t lexer.TokenType) bool { return p.peekToken.Type == t }
func (p *Parser) curTokenIs(t lexer.TokenType) bool  { return p.curToken.Type == t }
func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}
func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return LOWEST
}
func (p *Parser) expectPeek(t lexer.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}
func (p *Parser) peekError(t lexer.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead at line %d", t, p.peekToken.Type, p.peekToken.Line)
	p.errors = append(p.errors, msg)
}
func (p *Parser) registerPrefix(tokenType lexer.TokenType, fn prefixParseFn) {
	p.prefixParseFns[tokenType] = fn
}
func (p *Parser) registerInfix(tokenType lexer.TokenType, fn infixParseFn) {
	p.infixParseFns[tokenType] = fn
}
func (p *Parser) noPrefixParseFnError(t lexer.TokenType) {
	msg := fmt.Sprintf("no prefix parse function for %s found at line %d", t, p.curToken.Line)
	p.errors = append(p.errors, msg)
}
