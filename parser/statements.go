package parser

import (
	"synovium/ast"
	"synovium/lexer"
)

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

func (p *Parser) parseExpressionStatement() ast.Stmt {
	stmt := &ast.ExprStmt{Token: p.curToken}
	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

// Stubs for future implementation
func (p *Parser) parseVariableDeclStmt() ast.Stmt { return nil }
func (p *Parser) parseReturnStatement() ast.Stmt  { return nil }
func (p *Parser) parseYieldStatement() ast.Stmt   { return nil }
func (p *Parser) parseBreakStatement() ast.Stmt   { return nil }
