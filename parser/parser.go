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
	lexer.DECL_ASSIGN: ASSIGN,
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

	disallowStructInit bool // Resolves the `if/match IDENT {` vs `StructInit {` ambiguity
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}

	p.prefixParseFns = make(map[lexer.TokenType]prefixParseFn)
	p.infixParseFns = make(map[lexer.TokenType]infixParseFn)

	// --- PREFIX REGISTRATIONS ---
	p.registerPrefix(lexer.IDENT, p.parseIdentifier)
	p.registerPrefix(lexer.INT, p.parseIntLiteral)
	p.registerPrefix(lexer.FLOAT, p.parseFloatLiteral)
	p.registerPrefix(lexer.STRING, p.parseStringLiteral)
	p.registerPrefix(lexer.CHAR, p.parseCharLiteral)
	p.registerPrefix(lexer.TRUE, p.parseBoolLiteral)
	p.registerPrefix(lexer.FALSE, p.parseBoolLiteral)

	p.registerPrefix(lexer.BANG, p.parsePrefixExpression)
	p.registerPrefix(lexer.TILDE, p.parsePrefixExpression)
	p.registerPrefix(lexer.MINUS, p.parsePrefixExpression)
	p.registerPrefix(lexer.ASTERISK, p.parsePrefixExpression)
	p.registerPrefix(lexer.AMPERS, p.parsePrefixExpression)

	p.registerPrefix(lexer.LPAREN, p.parseGroupedExpression)
	p.registerPrefix(lexer.IF, p.parseIfExpression)
	p.registerPrefix(lexer.MATCH, p.parseMatchExpression)
	p.registerPrefix(lexer.LOOP, p.parseLoopExpression)
	p.registerPrefix(lexer.LBRACE, p.parseBlockExpression)

	// --- INFIX REGISTRATIONS ---
	infixes := []lexer.TokenType{
		lexer.PLUS, lexer.MINUS, lexer.ASTERISK, lexer.SLASH, lexer.MOD,
		lexer.EQ, lexer.NOT_EQ, lexer.LT, lexer.LTE, lexer.GT, lexer.GTE,
		lexer.AND, lexer.OR, lexer.PIPE, lexer.CARET, lexer.AMPERS,
		lexer.LSHIFT, lexer.RSHIFT, lexer.RANGE, lexer.ASSIGN, lexer.MUT_ASSIGN,
	}
	for _, t := range infixes {
		p.registerInfix(t, p.parseInfixExpression)
	}

	p.registerInfix(lexer.LPAREN, p.parseCallExpression)
	p.registerInfix(lexer.DOT, p.parseFieldAccess)
	p.registerInfix(lexer.LBRACKET, p.parseIndexExpression)
	p.registerInfix(lexer.AS, p.parseCastExpression)
	p.registerInfix(lexer.QUESTION, p.parseBubbleExpression)

	p.nextToken()
	p.nextToken()

	return p
}

// THE PRATT PARSING LOOP
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

// --- UTILITIES ---
func (p *Parser) Errors() []string { return p.errors }
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
