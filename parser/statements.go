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

func (p *Parser) parseVariableDeclStmt() ast.Stmt {
	// A variable declaration is legally both a Decl and a Stmt in Synovium.
	decl := p.parseVariableDecl()
	if v, ok := decl.(*ast.VariableDecl); ok {
		return v
	}
	return nil
}

func (p *Parser) parseReturnStatement() ast.Stmt {
	stmt := &ast.ReturnStmt{Token: p.curToken}
	p.nextToken()

	if !p.curTokenIs(lexer.SEMICOLON) {
		stmt.Value = p.parseExpression(LOWEST)
	}
	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseYieldStatement() ast.Stmt {
	stmt := &ast.YieldStmt{Token: p.curToken}
	p.nextToken()

	if !p.curTokenIs(lexer.SEMICOLON) {
		stmt.Value = p.parseExpression(LOWEST)
	}
	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseBreakStatement() ast.Stmt {
	stmt := &ast.BreakStmt{Token: p.curToken}
	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}
