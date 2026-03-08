package parser

import (
	"strconv"
	"strings"

	"synovium/ast"
	"synovium/lexer"
)

// --- PREFIX IMPLEMENTATIONS ---
func (p *Parser) parseIdentifier() ast.Expr {
	ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	// If followed by '{', morph into Struct Init!
	if !p.disallowStructInit && p.peekTokenIs(lexer.LBRACE) {
		return p.parseStructInitExpression(ident)
	}
	return ident
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

func isOverloadableOperator(t lexer.TokenType) bool {
	switch t {
	case lexer.PLUS, lexer.MINUS, lexer.ASTERISK, lexer.SLASH, lexer.MOD, lexer.EQ, lexer.NOT_EQ, lexer.LT, lexer.LTE, lexer.GT, lexer.GTE:
		return true
	}
	return false
}

func (p *Parser) parseFunctionLiteral() ast.Expr {
	decl := &ast.FunctionDecl{Token: p.curToken}

	// 1. OPTIONAL Name (if it's named, it's a nested function; if not, it's a lambda)
	if p.peekTokenIs(lexer.IDENT) || isOverloadableOperator(p.peekToken.Type) {
		p.nextToken()
		decl.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	}

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}

	// 2. Parameters
	for !p.peekTokenIs(lexer.RPAREN) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken() // Move to identifier
		if p.curToken.Type == lexer.RANGE {
			decl.IsVariadic = true
			break
		}
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

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken() // Consume the ';'
		return decl
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

	// --- THE FIX: Generic Struct Initialization ---
	// If Vector3(f64) is followed by '{', morph it into a Struct Init!
	if !p.disallowStructInit && p.peekTokenIs(lexer.LBRACE) {
		return p.parseStructInitExpression(expr)
	}

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

func (p *Parser) parseFloatLiteral() ast.Expr {
	lit := &ast.FloatLiteral{Token: p.curToken, Value: p.curToken.Literal}

	// Validate that it is a mathematically sound IEEE-754 float
	_, err := strconv.ParseFloat(p.curToken.Literal, 64)
	if err != nil {
		p.errors = append(p.errors, "invalid float literal: "+p.curToken.Literal)
	}
	return lit
}

func (p *Parser) parseStringLiteral() ast.Expr {
	val := p.curToken.Literal

	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}

	val = strings.ReplaceAll(val, `\n`, "\n")
	val = strings.ReplaceAll(val, `\t`, "\t")
	val = strings.ReplaceAll(val, `\r`, "\r")
	val = strings.ReplaceAll(val, `\\`, "\\")
	val = strings.ReplaceAll(val, `\"`, "\"")

	return &ast.StringLiteral{Token: p.curToken, Value: val}
}

// --- NEW: Array Initialization Parser ---
func (p *Parser) parseArrayInitExpression() ast.Expr {
	expr := &ast.ArrayInitExpr{Token: p.curToken}

	if p.peekTokenIs(lexer.RBRACKET) {
		p.nextToken()
		expr.EndSpan = p.curToken.Span.End
		return expr
	}

	p.nextToken() // move to first element
	firstEl := p.parseExpression(LOWEST)

	// --- NEW: Array Repeat Syntax `[0 ; 50]` ---
	if p.peekTokenIs(lexer.SEMICOLON) {
		expr.Elements = []ast.Expr{firstEl}
		p.nextToken() // move to ';'
		p.nextToken() // move to count expression
		expr.Count = p.parseExpression(LOWEST)
		if !p.expectPeek(lexer.RBRACKET) {
			return nil
		}
	} else {
		// Standard List Syntax `[1, 2, 3]`
		expr.Elements = append(expr.Elements, firstEl)
		for p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
			p.nextToken()
			expr.Elements = append(expr.Elements, p.parseExpression(LOWEST))
		}
		if !p.expectPeek(lexer.RBRACKET) {
			return nil
		}
	}

	expr.EndSpan = p.curToken.Span.End
	return expr
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

	if p.curTokenIs(lexer.BACKTICK) {
		if !p.expectPeek(lexer.IDENT) {
			return nil
		}
		expr.Label = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

		if !p.expectPeek(lexer.LOOP) {
			return nil
		}
	}

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
	p.disallowStructInit = true
	expr.Value = p.parseExpression(LOWEST)
	p.disallowStructInit = false

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	for !p.peekTokenIs(lexer.RBRACE) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken()
		arm := &ast.MatchArm{Token: p.curToken}

		// --- THE FIX: Smart Pattern Path Parsing ---
		patternName := p.curToken.Literal

		for p.peekTokenIs(lexer.DOT) || p.peekTokenIs(lexer.LPAREN) {
			if p.peekTokenIs(lexer.DOT) {
				p.nextToken()
				patternName += "."
				if !p.expectPeek(lexer.IDENT) {
					return nil
				}
				patternName += p.curToken.Literal
			} else if p.peekTokenIs(lexer.LPAREN) {
				p.nextToken() // eat '('

				var insideParens []string
				for !p.peekTokenIs(lexer.RPAREN) && !p.peekTokenIs(lexer.EOF) {
					p.nextToken()
					insideParens = append(insideParens, p.curToken.Literal)
					if p.peekTokenIs(lexer.COMMA) {
						p.nextToken()
					}
				}
				p.expectPeek(lexer.RPAREN) // eat ')'

				// If the NEXT token is DOT, these were generic arguments! e.g. Option(i32).Some
				if p.peekTokenIs(lexer.DOT) {
					patternName += "(" + strings.Join(insideParens, ", ") + ")"
				} else if p.peekTokenIs(lexer.ARROW) {
					// If the NEXT token is ARROW, this was the payload! e.g. .Some(val) ->
					for _, paramName := range insideParens {
						arm.Params = append(arm.Params, &ast.Identifier{Value: paramName})
					}
					break // We reached the end of the pattern
				} else {
					p.errors = append(p.errors, "unexpected token in match pattern after parens")
					return nil
				}
			}
		}

		arm.Pattern = &ast.Identifier{Token: p.curToken, Value: patternName}

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

func (p *Parser) parseStructInitExpression(name ast.Expr) ast.Expr {
	expr := &ast.StructInitExpr{
		Token: p.peekToken, // The '{' token
		Name:  name,
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
