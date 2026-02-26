package parser

import (
	"synovium/ast"
	"synovium/lexer"
)

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
		if p.peekTokenIs(lexer.COLON) {
			return p.parseVariableDecl()
		}
		fallthrough
	default:
		if p.curToken.Type == lexer.SEMICOLON {
			return nil
		}
		p.errors = append(p.errors, "illegal top-level declaration: "+p.curToken.Literal)
		return nil
	}
}

func (p *Parser) parseVariableDecl() ast.Decl {
	decl := &ast.VariableDecl{
		Token: p.curToken,
		Name:  &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal},
	}

	if !p.expectPeek(lexer.COLON) {
		return nil
	}

	p.nextToken()
	decl.Type = p.parseType()

	if !p.peekTokenIs(lexer.ASSIGN) && !p.peekTokenIs(lexer.MUT_ASSIGN) && !p.peekTokenIs(lexer.DECL_ASSIGN) {
		p.errors = append(p.errors, "expected assignment operator (=, ~=, :=) after type")
		return nil
	}

	p.nextToken()
	decl.Operator = p.curToken.Literal

	p.nextToken()
	decl.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}

	return decl
}

// --- STRUCT DECLARATION ---
func (p *Parser) parseStructDecl() ast.Decl {
	decl := &ast.StructDecl{Token: p.curToken}
	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	for !p.peekTokenIs(lexer.RBRACE) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken() // move to identifier
		field := &ast.FieldDecl{
			Token: p.curToken,
			Name:  &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal},
		}
		if !p.expectPeek(lexer.COLON) {
			return nil
		}
		p.nextToken() // move to type
		field.Type = p.parseType()
		decl.Fields = append(decl.Fields, field)

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
	}

	if !p.expectPeek(lexer.RBRACE) {
		return nil
	}
	decl.EndSpan = p.curToken.Span.End
	return decl
}

// --- ENUM DECLARATION ---
func (p *Parser) parseEnumDecl() ast.Decl {
	decl := &ast.EnumDecl{Token: p.curToken}
	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	for !p.peekTokenIs(lexer.RBRACE) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken()
		variant := &ast.VariantDecl{
			Token: p.curToken,
			Name:  &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal},
		}
		// Check for associated types like Running(f64)
		if p.peekTokenIs(lexer.LPAREN) {
			p.nextToken() // Eat '('
			p.nextToken() // Move to first type
			for !p.curTokenIs(lexer.RPAREN) && !p.curTokenIs(lexer.EOF) {
				variant.Types = append(variant.Types, p.parseType())
				if p.peekTokenIs(lexer.COMMA) {
					p.nextToken()
				}
				p.nextToken()
			}
		}
		decl.Variants = append(decl.Variants, variant)
		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
	}

	if !p.expectPeek(lexer.RBRACE) {
		return nil
	}
	decl.EndSpan = p.curToken.Span.End
	return decl
}

// --- IMPL DECLARATION ---
func (p *Parser) parseImplDecl() ast.Decl {
	decl := &ast.ImplDecl{Token: p.curToken}
	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Target = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	for !p.peekTokenIs(lexer.RBRACE) && !p.peekTokenIs(lexer.EOF) {
		p.nextToken()
		if p.curTokenIs(lexer.FNC) {
			if method, ok := p.parseFunctionDecl().(*ast.FunctionDecl); ok {
				decl.Methods = append(decl.Methods, method)
			}
		}
	}

	if !p.expectPeek(lexer.RBRACE) {
		return nil
	}
	decl.EndSpan = p.curToken.Span.End
	return decl
}

// --- FUNCTION DECLARATION ---
func (p *Parser) parseFunctionDecl() ast.Decl {
	decl := &ast.FunctionDecl{Token: p.curToken}
	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}

	// Parse parameters
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

	// Optional return type
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
