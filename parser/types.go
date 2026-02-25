package parser

import (
	"fmt"
	"synovium/ast"
	"synovium/lexer"
)

var intrinsicTypes = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true, "i128": true,
	"u8": true, "u16": true, "u32": true, "u64": true, "u128": true,
	"f4": true, "f8": true, "f16": true, "f32": true, "f64": true, "f128": true,
	"chr": true, "bln": true, "usize": true, "str": true, "type": true,
}

func (p *Parser) parseType() ast.Type {
	if p.curTokenIs(lexer.ASTERISK) {
		tok := p.curToken
		p.nextToken()
		return &ast.PointerType{Token: tok, Base: p.parseType()}
	}

	if p.curTokenIs(lexer.AMPERS) {
		tok := p.curToken
		p.nextToken()
		return &ast.ReferenceType{Token: tok, Base: p.parseType()}
	}

	return p.parseBaseType()
}

func (p *Parser) parseBaseType() ast.Type {
	if p.curTokenIs(lexer.LBRACKET) {
		return p.parseArrayType()
	}

	if p.curTokenIs(lexer.IDENT) {
		return p.parseNamedType()
	}

	p.errors = append(p.errors, fmt.Sprintf("expected a type at line %d, got %s", p.curToken.Line, p.curToken.Literal))
	return nil
}

func (p *Parser) parseNamedType() ast.Type {
	tok := p.curToken
	name := p.curToken.Literal
	endSpan := tok.Span.End

	for p.peekTokenIs(lexer.DOT) {
		p.nextToken()
		name += "."
		if !p.expectPeek(lexer.IDENT) {
			return nil
		}
		name += p.curToken.Literal
		endSpan = p.curToken.Span.End
	}

	return &ast.NamedType{
		Token:       tok,
		Name:        name,
		IsIntrinsic: intrinsicTypes[name],
		EndSpan:     endSpan,
	}
}

func (p *Parser) parseArrayType() ast.Type {
	tok := p.curToken
	p.nextToken()
	baseType := p.parseType()

	if !p.expectPeek(lexer.SEMICOLON) {
		return nil
	}
	p.nextToken()

	arrType := &ast.ArrayType{Token: tok, Base: baseType}

	if p.curTokenIs(lexer.COLON) {
		arrType.IsSlice = true
		if !p.expectPeek(lexer.RBRACKET) {
			return nil
		}
	} else {
		arrType.Size = p.parseExpression(LOWEST)
		if !p.expectPeek(lexer.RBRACKET) {
			return nil
		}
	}

	arrType.EndSpan = p.curToken.Span.End
	return arrType
}
