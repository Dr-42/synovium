package lexer

// Span tracks the exact byte offsets in the source string.
// Start is inclusive, End is exclusive (like Go slices).
type Span struct {
	Start int
	End   int
}

type TokenType string

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
	Span    Span
}

const (
	ILLEGAL TokenType = "ILLEGAL"
	EOF     TokenType = "EOF"

	IDENT  TokenType = "IDENT"
	INT    TokenType = "INT"
	FLOAT  TokenType = "FLOAT"
	STRING TokenType = "STRING"
	CHAR   TokenType = "CHAR"

	ASSIGN      TokenType = "="
	DECL_ASSIGN TokenType = ":="
	MUT_ASSIGN  TokenType = "~="
	PLUS_ASSIGN TokenType = "+="
	MIN_ASSIGN  TokenType = "-="
	MUL_ASSIGN  TokenType = "*="
	DIV_ASSIGN  TokenType = "/="
	MOD_ASSIGN  TokenType = "%="

	PLUS     TokenType = "+"
	MINUS    TokenType = "-"
	ASTERISK TokenType = "*"
	SLASH    TokenType = "/"
	MOD      TokenType = "%"

	BANG     TokenType = "!"
	TILDE    TokenType = "~"
	AMPERS   TokenType = "&"
	PIPE     TokenType = "|"
	CARET    TokenType = "^"
	QUESTION TokenType = "?"

	LSHIFT TokenType = "<<"
	RSHIFT TokenType = ">>"

	AND    TokenType = "&&"
	OR     TokenType = "||"
	EQ     TokenType = "=="
	NOT_EQ TokenType = "!="
	LT     TokenType = "<"
	LTE    TokenType = "<="
	GT     TokenType = ">"
	GTE    TokenType = ">="

	ARROW TokenType = "->"
	RANGE TokenType = "..."
	DOT   TokenType = "."

	COMMA     TokenType = ","
	COLON     TokenType = ":"
	SEMICOLON TokenType = ";"

	LPAREN   TokenType = "("
	RPAREN   TokenType = ")"
	LBRACE   TokenType = "{"
	RBRACE   TokenType = "}"
	LBRACKET TokenType = "["
	RBRACKET TokenType = "]"

	STRUCT TokenType = "STRUCT"
	ENUM   TokenType = "ENUM"
	IMPL   TokenType = "IMPL"
	FNC    TokenType = "FNC"
	RET    TokenType = "RET"
	YLD    TokenType = "YLD"
	BRK    TokenType = "BRK"
	IF     TokenType = "IF"
	ELIF   TokenType = "ELIF"
	ELSE   TokenType = "ELSE"
	MATCH  TokenType = "MATCH"
	LOOP   TokenType = "LOOP"
	AS     TokenType = "AS"

	TRUE  TokenType = "TRUE"
	FALSE TokenType = "FALSE"
)

var keywords = map[string]TokenType{
	"struct": STRUCT,
	"enum":   ENUM,
	"impl":   IMPL,
	"fnc":    FNC,
	"ret":    RET,
	"yld":    YLD,
	"brk":    BRK,
	"if":     IF,
	"elif":   ELIF,
	"else":   ELSE,
	"match":  MATCH,
	"loop":   LOOP,
	"as":     AS,
	"true":   TRUE,
	"false":  FALSE,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
