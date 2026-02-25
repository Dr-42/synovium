package parser

import (
	"synovium/ast"
	"synovium/lexer"
)

// --- PREFIX IMPLEMENTATIONS ---
func (p *Parser) parseIdentifier() ast.Expr {
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

	// Right associativity trick for assignments
	if expr.Operator == "=" || expr.Operator == "~=" || expr.Operator == ":=" {
		expr.Right = p.parseExpression(precedence - 1)
	} else {
		expr.Right = p.parseExpression(precedence)
	}

	return expr
}

func (p *Parser) parseCallExpression(left ast.Expr) ast.Expr {
	expr := &ast.CallExpr{
		Token:    p.curToken,
		Function: left,
	}
	expr.Arguments = p.parseExpressionList(lexer.RPAREN)
	expr.EndSpan = p.curToken.Span.End
	return expr
}

func (p *Parser) parseFieldAccess(left ast.Expr) ast.Expr {
	expr := &ast.FieldAccessExpr{
		Token: p.curToken,
		Left:  left,
	}
	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	expr.Field = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	return expr
}

func (p *Parser) parseIndexExpression(left ast.Expr) ast.Expr {
	expr := &ast.IndexExpr{
		Token: p.curToken,
		Left:  left,
	}
	p.nextToken()
	expr.Index = p.parseExpression(LOWEST)
	if !p.expectPeek(lexer.RBRACKET) {
		return nil
	}
	expr.EndSpan = p.curToken.Span.End
	return expr
}

func (p *Parser) parseBlockExpression() ast.Expr {
	block := &ast.Block{
		Token:      p.curToken,
		Statements: []ast.Stmt{},
	}

	p.nextToken()

	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}

	// Synovium bubbling logic
	if len(block.Statements) > 0 {
		lastIdx := len(block.Statements) - 1
		if exprStmt, ok := block.Statements[lastIdx].(*ast.ExprStmt); ok {
			block.Value = exprStmt.Value
			block.Statements = block.Statements[:lastIdx]
		}
	}

	block.CloseSpan = p.curToken.Span
	return block
}

func (p *Parser) parseExpressionList(endToken lexer.TokenType) []ast.Expr {
	var list []ast.Expr
	if p.peekTokenIs(endToken) {
		p.nextToken()
		return list
	}

	p.nextToken()
	list = append(list, p.parseExpression(LOWEST))

	for p.peekTokenIs(lexer.COMMA) {
		p.nextToken()
		p.nextToken()
		list = append(list, p.parseExpression(LOWEST))
	}

	if p.peekTokenIs(lexer.COMMA) {
		p.nextToken()
	}
	if !p.expectPeek(endToken) {
		return nil
	}
	return list
}

// Stubs for future implementation
func (p *Parser) parseIfExpression() ast.Expr         { return nil }
func (p *Parser) parseMatchExpression() ast.Expr      { return nil }
func (p *Parser) parseLoopExpression() ast.Expr       { return nil }
func (p *Parser) parseStructInitExpression() ast.Expr { return nil }
