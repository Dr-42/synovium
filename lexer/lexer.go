package lexer

type Lexer struct {
	input        string
	position     int  // current position in input (points to current char)
	readPosition int  // current reading position in input (after current char)
	ch           byte // current char under examination
	line         int
	column       int
}

func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1, column: 0}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.ch == '\n' {
		l.line++
		l.column = 0
	}
	if l.readPosition >= len(l.input) {
		l.ch = 0 // ASCII code for "NUL" (EOF)
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
	l.column++
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

func (l *Lexer) NextToken() Token {
	var tok Token

	l.skipWhitespace()

	// Capture current line and column for the token
	tokLine := l.line
	tokCol := l.column

	switch l.ch {
	case '=':
		tok = newToken(ASSIGN, l.ch, tokLine, tokCol)
	case ':':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: COMPTIME_ASSIGN, Literal: literal, Line: tokLine, Column: tokCol}
		} else {
			tok = newToken(COLON, l.ch, tokLine, tokCol)
		}
	case '~':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			literal := string(ch) + string(l.ch)
			tok = Token{Type: MUT_ASSIGN, Literal: literal, Line: tokLine, Column: tokCol}
		} else {
			tok = newToken(ILLEGAL, l.ch, tokLine, tokCol)
		}
	case ';':
		tok = newToken(SEMICOLON, l.ch, tokLine, tokCol)
	case ',':
		tok = newToken(COMMA, l.ch, tokLine, tokCol)
	case '(':
		tok = newToken(LPAREN, l.ch, tokLine, tokCol)
	case ')':
		tok = newToken(RPAREN, l.ch, tokLine, tokCol)
	case '{':
		tok = newToken(LBRACE, l.ch, tokLine, tokCol)
	case '}':
		tok = newToken(RBRACE, l.ch, tokLine, tokCol)
	case 0:
		tok.Literal = ""
		tok.Type = EOF
		tok.Line = tokLine
		tok.Column = tokCol
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = LookupIdent(tok.Literal)
			tok.Line = tokLine
			tok.Column = tokCol
			return tok
		} else if isDigit(l.ch) {
			tok.Literal = l.readNumber()
			tok.Type = INT
			tok.Line = tokLine
			tok.Column = tokCol
			return tok
		} else {
			tok = newToken(ILLEGAL, l.ch, tokLine, tokCol)
		}
	}

	l.readChar()
	return tok
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
	}
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) { // Allow digits in identifiers after the first char
		l.readChar()
	}
	return l.input[position:l.position]
}

func (l *Lexer) readNumber() string {
	position := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func newToken(tokenType TokenType, ch byte, line int, col int) Token {
	return Token{Type: tokenType, Literal: string(ch), Line: line, Column: col}
}
