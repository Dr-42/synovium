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
	case lexer.DEFER: // Replaced YLD
		return p.parseDeferStatement()
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

func (p *Parser) parseDeferStatement() ast.Stmt {
	stmt := &ast.DeferStmt{Token: p.curToken}
	p.nextToken() // move past 'defer'

	// A defer can encapsulate an entire block, or a single statement
	if p.curTokenIs(lexer.LBRACE) {
		stmt.Body = &ast.ExprStmt{
			Token: p.curToken,
			Value: p.parseBlockExpression(),
		}
	} else {
		stmt.Body = p.parseStatement()
	}
	return stmt
}

// --- UPGRADED: Parse Break Statements ---
func (p *Parser) parseBreakStatement() ast.Stmt {
	stmt := &ast.BreakStmt{Token: p.curToken}

	// 1. Optional target label: brk `outer
	if p.peekTokenIs(lexer.BACKTICK) {
		p.nextToken() // move to '`'
		if p.expectPeek(lexer.IDENT) {
			stmt.Label = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		}
	}

	// 2. Optional bubbled value: brk 42;
	if !p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
		stmt.Value = p.parseExpression(LOWEST)
	}

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}
