package parser

import (
	"strconv"
	"synovium/ast"
	"synovium/lexer"
)

// --- PREFIX IMPLEMENTATIONS ---
func (p *Parser) parseIdentifier() ast.Expr {
	if !p.disallowStructInit && p.peekTokenIs(lexer.LBRACE) {
		return p.parseStructInitExpression()
	}
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseIntLiteral() ast.Expr {
	lit := &ast.IntLiteral{Token: p.curToken}

	// Actually parse the string into a binary integer so the Semantic Phase can read it!
	val, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err == nil {
		lit.Value = val
	}

	return lit
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

func (p *Parser) parseFunctionLiteral() ast.Expr {
	decl := &ast.FunctionDecl{Token: p.curToken}

	// 1. OPTIONAL Name (if it's named, it's a nested function; if not, it's a lambda)
	if p.peekTokenIs(lexer.IDENT) {
		p.nextToken()
		decl.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	}

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}

	// 2. Parameters
	for !p.peekTokenIs(lexer.RPAREN) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken() // Move to identifier
		param := &ast.Parameter{
			Token: p.curToken,
			Name:  &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal},
		}
		if !p.expectPeek(lexer.COLON) {
			return nil
		}
		p.nextToken() // Move to type
		param.Type = p.parseType()
		decl.Parameters = append(decl.Parameters, param)

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
	}
	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}

	// 3. Optional Return Type
	if p.peekTokenIs(lexer.ASSIGN) || p.peekTokenIs(lexer.DECL_ASSIGN) {
		p.nextToken() // Move to '=' or ':='
		decl.ReturnOp = p.curToken.Literal
		p.nextToken() // Move to type
		decl.ReturnType = p.parseType()
	}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	decl.Body = p.parseBlockExpression().(*ast.Block)

	return decl
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
	if expr.Operator == "=" || expr.Operator == "~=" || expr.Operator == ":=" ||
		expr.Operator == "+=" || expr.Operator == "-=" || expr.Operator == "*=" ||
		expr.Operator == "/=" || expr.Operator == "%=" {
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
func (p *Parser) parseFloatLiteral() ast.Expr {
	return &ast.FloatLiteral{Token: p.curToken, Value: p.curToken.Literal}
}
func (p *Parser) parseStringLiteral() ast.Expr {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}
func (p *Parser) parseCharLiteral() ast.Expr {
	return &ast.CharLiteral{Token: p.curToken, Value: p.curToken.Literal}
}
func (p *Parser) parseBoolLiteral() ast.Expr {
	return &ast.BoolLiteral{Token: p.curToken, Value: p.curTokenIs(lexer.TRUE)}
}

// ============================================================================
// CONTROL FLOW & COMPLEX EXPRESSIONS
// ============================================================================

func (p *Parser) parseIfExpression() ast.Expr {
	expr := &ast.IfExpr{Token: p.curToken}

	p.nextToken() // Move past 'if'

	// Disable struct init to prevent `if cond {` from being eaten as a struct literal
	p.disallowStructInit = true
	expr.Condition = p.parseExpression(LOWEST)
	p.disallowStructInit = false

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	expr.Body = p.parseBlockExpression().(*ast.Block)

	for p.peekTokenIs(lexer.ELIF) {
		p.nextToken() // Move to 'elif'
		p.nextToken() // Move past 'elif' to the condition

		p.disallowStructInit = true
		cond := p.parseExpression(LOWEST)
		p.disallowStructInit = false

		if !p.expectPeek(lexer.LBRACE) {
			return nil
		}
		body := p.parseBlockExpression().(*ast.Block)

		expr.ElifConds = append(expr.ElifConds, cond)
		expr.ElifBodies = append(expr.ElifBodies, body)
	}

	if p.peekTokenIs(lexer.ELSE) {
		p.nextToken() // Move to 'else'
		if !p.expectPeek(lexer.LBRACE) {
			return nil
		}
		expr.ElseBody = p.parseBlockExpression().(*ast.Block)
	}

	return expr
}

func (p *Parser) parseLoopExpression() ast.Expr {
	expr := &ast.LoopExpr{Token: p.curToken} // The 'loop' token

	// Optional condition: loop (i : i32 = 0...10) { ... }
	if p.peekTokenIs(lexer.LPAREN) {
		p.nextToken() // move to '('
		p.nextToken() // move inside '('

		// Is it a variable declaration like `i : i32 = 0...10` or just an expression like `true`?
		if p.curTokenIs(lexer.IDENT) && p.peekTokenIs(lexer.COLON) {
			expr.Condition = p.parseVariableDecl()
		} else {
			expr.Condition = p.parseExpression(LOWEST)
		}

		if !p.expectPeek(lexer.RPAREN) {
			return nil
		}
	}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	expr.Body = p.parseBlockExpression().(*ast.Block)

	return expr
}

func (p *Parser) parseMatchExpression() ast.Expr {
	expr := &ast.MatchExpr{Token: p.curToken}

	p.nextToken() // move past 'match'

	// Disable struct init to prevent `match s {` from eating the block
	p.disallowStructInit = true
	expr.Value = p.parseExpression(LOWEST)
	p.disallowStructInit = false

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	for !p.peekTokenIs(lexer.RBRACE) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken() // move to the start of the pattern arm

		arm := &ast.MatchArm{Token: p.curToken}

		patternName := p.curToken.Literal
		for p.peekTokenIs(lexer.DOT) {
			p.nextToken()
			patternName += "."
			if !p.expectPeek(lexer.IDENT) {
				return nil
			}
			patternName += p.curToken.Literal
		}
		arm.Pattern = &ast.Identifier{Token: p.curToken, Value: patternName}

		if p.peekTokenIs(lexer.LPAREN) {
			p.nextToken()
			for !p.peekTokenIs(lexer.RPAREN) && !p.peekTokenIs(lexer.EOF) {
				p.nextToken()
				if p.curTokenIs(lexer.IDENT) {
					arm.Params = append(arm.Params, &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal})
				}
				if p.peekTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}
			p.expectPeek(lexer.RPAREN)
		}

		if !p.expectPeek(lexer.ARROW) {
			return nil
		}
		if !p.expectPeek(lexer.LBRACE) {
			return nil
		}
		arm.Body = p.parseBlockExpression().(*ast.Block)

		expr.Arms = append(expr.Arms, arm)

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
	}

	if !p.expectPeek(lexer.RBRACE) {
		return nil
	}
	expr.EndSpan = p.curToken.Span.End
	return expr
}

func (p *Parser) parseStructInitExpression() ast.Expr {
	// We are currently sitting on the IDENTifier before the '{'
	expr := &ast.StructInitExpr{
		Token: p.curToken,
		Name:  &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal},
	}

	p.nextToken() // move to '{'

	for !p.peekTokenIs(lexer.RBRACE) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken() // Move to '.'
		if !p.curTokenIs(lexer.DOT) {
			p.errors = append(p.errors, "struct initialization fields must start with '.'")
			return nil
		}

		field := &ast.StructInitField{Token: p.curToken}

		if !p.expectPeek(lexer.IDENT) {
			return nil
		}
		field.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

		if !p.expectPeek(lexer.ASSIGN) {
			return nil
		}
		p.nextToken() // move onto the expression
		field.Value = p.parseExpression(LOWEST)

		expr.Fields = append(expr.Fields, field)

		// Consume optional comma
		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
	}

	if !p.expectPeek(lexer.RBRACE) {
		return nil
	}
	expr.EndSpan = p.curToken.Span.End
	return expr
}

// ============================================================================
// INFIX COMPLEX EXPRESSIONS
// ============================================================================

func (p *Parser) parseCastExpression(left ast.Expr) ast.Expr {
	expr := &ast.CastExpr{
		Token: p.curToken, // The 'as' token
		Left:  left,
	}

	p.nextToken() // Move to the type
	expr.Type = p.parseType()

	return expr
}

func (p *Parser) parseBubbleExpression(left ast.Expr) ast.Expr {
	// The '?' operator is pure postfix, no right-hand side needed.
	return &ast.BubbleExpr{
		Token: p.curToken,
		Left:  left,
	}
}
