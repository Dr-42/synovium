package lexer

type Lexer struct {
	input        string
	position     int  // current byte offset in input
	readPosition int  // next byte offset to read
	ch           byte // current char under examination
	line         int  // current line number
	column       int  // current column number
}

func New(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 0,
	}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
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

func (l *Lexer) peekCharN(offset int) byte {
	if l.position+offset >= len(l.input) {
		return 0
	}
	return l.input[l.position+offset]
}

func (l *Lexer) NextToken() Token {
	var tok Token

	l.skipWhitespace()

	// Skip comments (and re-evaluate whitespace after)
	for l.ch == '/' && l.peekChar() == '/' {
		l.skipComment()
		l.skipWhitespace()
	}

	// Capture exact starting location
	startLine := l.line
	startCol := l.column
	startOffset := l.position

	// Helper to instantly map operators with their exact byte spans
	makeToken := func(t TokenType, literal string, length int) Token {
		return Token{
			Type:    t,
			Literal: literal,
			Line:    startLine,
			Column:  startCol,
			Span:    Span{Start: startOffset, End: startOffset + length},
		}
	}

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(EQ, "==", 2)
		} else {
			tok = makeToken(ASSIGN, string(l.ch), 1)
		}
	case ':':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(DECL_ASSIGN, ":=", 2)
		} else {
			tok = makeToken(COLON, string(l.ch), 1)
		}
	case '~':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(MUT_ASSIGN, "~=", 2)
		} else {
			tok = makeToken(TILDE, string(l.ch), 1)
		}
	case '-':
		if l.peekChar() == '>' {
			l.readChar()
			tok = makeToken(ARROW, "->", 2)
		} else if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(MIN_ASSIGN, "-=", 2)
		} else {
			tok = makeToken(MINUS, string(l.ch), 1)
		}
	case '<':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(LTE, "<=", 2)
		} else if l.peekChar() == '<' {
			l.readChar()
			tok = makeToken(LSHIFT, "<<", 2)
		} else {
			tok = makeToken(LT, string(l.ch), 1)
		}
	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(GTE, ">=", 2)
		} else if l.peekChar() == '>' {
			l.readChar()
			tok = makeToken(RSHIFT, ">>", 2)
		} else {
			tok = makeToken(GT, string(l.ch), 1)
		}
	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(NOT_EQ, "!=", 2)
		} else {
			tok = makeToken(BANG, string(l.ch), 1)
		}
	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok = makeToken(AND, "&&", 2)
		} else {
			tok = makeToken(AMPERS, string(l.ch), 1)
		}
	case '|':
		if l.peekChar() == '|' {
			l.readChar()
			tok = makeToken(OR, "||", 2)
		} else {
			tok = makeToken(PIPE, string(l.ch), 1)
		}
	case '.':
		if l.peekChar() == '.' && l.peekCharN(2) == '.' {
			l.readChar()
			l.readChar()
			tok = makeToken(RANGE, "...", 3)
		} else {
			tok = makeToken(DOT, string(l.ch), 1)
		}
	case '+':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(PLUS_ASSIGN, "+=", 2)
		} else {
			tok = makeToken(PLUS, string(l.ch), 1)
		}
	case '*':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(MUL_ASSIGN, "*=", 2)
		} else {
			tok = makeToken(ASTERISK, string(l.ch), 1)
		}
	case '/':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(DIV_ASSIGN, "/=", 2)
		} else {
			tok = makeToken(SLASH, string(l.ch), 1)
		}
	case '%':
		if l.peekChar() == '=' {
			l.readChar()
			tok = makeToken(MOD_ASSIGN, "%=", 2)
		} else {
			tok = makeToken(MOD, string(l.ch), 1)
		}
	case '^':
		tok = makeToken(CARET, string(l.ch), 1)
	case '?':
		tok = makeToken(QUESTION, string(l.ch), 1)
	case ';':
		tok = makeToken(SEMICOLON, string(l.ch), 1)
	case ',':
		tok = makeToken(COMMA, string(l.ch), 1)
	case '(':
		tok = makeToken(LPAREN, string(l.ch), 1)
	case ')':
		tok = makeToken(RPAREN, string(l.ch), 1)
	case '{':
		tok = makeToken(LBRACE, string(l.ch), 1)
	case '}':
		tok = makeToken(RBRACE, string(l.ch), 1)
	case '[':
		tok = makeToken(LBRACKET, string(l.ch), 1)
	case ']':
		tok = makeToken(RBRACKET, string(l.ch), 1)
	case '"':
		literal := l.readString()
		// l.position is naturally at the exclusive end offset now
		return Token{Type: STRING, Literal: literal, Line: startLine, Column: startCol, Span: Span{Start: startOffset, End: l.position}}
	case '\'':
		literal := l.readCharLiteral()
		return Token{Type: CHAR, Literal: literal, Line: startLine, Column: startCol, Span: Span{Start: startOffset, End: l.position}}
	case 0:
		tok = makeToken(EOF, "", 0)
	default:
		if isLetter(l.ch) {
			literal := l.readIdentifier()
			tokType := LookupIdent(literal)
			return Token{Type: tokType, Literal: literal, Line: startLine, Column: startCol, Span: Span{Start: startOffset, End: l.position}}
		} else if isDigit(l.ch) {
			tokType, literal := l.readNumber()
			return Token{Type: tokType, Literal: literal, Line: startLine, Column: startCol, Span: Span{Start: startOffset, End: l.position}}
		} else {
			tok = makeToken(ILLEGAL, string(l.ch), 1)
		}
	}

	// Advance past the final character of fixed-length tokens
	l.readChar()
	return tok
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		if l.ch == '\n' {
			l.line++
			l.column = 0 // Reset column on new line
		}
		l.readChar()
	}
}

func (l *Lexer) skipComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

// --- Reading Helpers ---

func (l *Lexer) readIdentifier() string {
	startPos := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[startPos:l.position]
}

func (l *Lexer) readNumber() (TokenType, string) {
	startPos := l.position
	tokType := INT

	if l.ch == '0' {
		peek := l.peekChar()
		switch peek {
		case 'x':
		case 'X':
			l.readChar()
			l.readChar()
			for isHexDigit(l.ch) {
				l.readChar()
			}
			return tokType, l.input[startPos:l.position]
		case 'o':
		case 'O':
			l.readChar()
			l.readChar()
			for isOctalDigit(l.ch) {
				l.readChar()
			}
			return tokType, l.input[startPos:l.position]
		case 'b':
		case 'B':
			l.readChar()
			l.readChar()
			for isBinaryDigit(l.ch) {
				l.readChar()
			}
			return tokType, l.input[startPos:l.position]
		}
	}

	for isDigit(l.ch) {
		l.readChar()
	}

	// Peek digit ensures we don't accidentally consume methods (1.to_string()) or ranges (1...10)
	if l.ch == '.' && isDigit(l.peekChar()) {
		tokType = FLOAT
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}

	return tokType, l.input[startPos:l.position]
}

func (l *Lexer) readString() string {
	startPos := l.position
	l.readChar() // consume initial quote
	for l.ch != '"' && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar() // Skip escape character
		}
		l.readChar()
	}
	l.readChar() // consume closing quote
	return l.input[startPos:l.position]
}

func (l *Lexer) readCharLiteral() string {
	startPos := l.position
	l.readChar() // consume initial quote
	for l.ch != '\'' && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
		}
		l.readChar()
	}
	l.readChar() // consume closing quote
	return l.input[startPos:l.position]

} // --- Validation Helpers ---

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func isHexDigit(ch byte) bool {
	return isDigit(ch) || ('a' <= ch && ch <= 'f') || ('A' <= ch && ch <= 'F')
}

func isOctalDigit(ch byte) bool {
	return '0' <= ch && ch <= '7'
}

func isBinaryDigit(ch byte) bool {
	return ch == '0' || ch == '1'
}
