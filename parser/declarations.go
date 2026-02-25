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

// Stubs for future implementation
func (p *Parser) parseStructDecl() ast.Decl   { return nil }
func (p *Parser) parseEnumDecl() ast.Decl     { return nil }
func (p *Parser) parseImplDecl() ast.Decl     { return nil }
func (p *Parser) parseFunctionDecl() ast.Decl { return nil }
