package lexer

type TokenType string

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

// Token Types
const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"

	// Identifiers + Literals
	IDENT  = "IDENT"
	INT    = "INT"
	FLOAT  = "FLOAT"
	STRING = "STRING"
	CHAR   = "CHAR"

	// Operators
	ASSIGN          = "="
	MUT_ASSIGN      = "~="
	COMPTIME_ASSIGN = ":="
	COLON           = ":"

	// Delimiters
	COMMA     = ","
	SEMICOLON = ";"
	LPAREN    = "("
	RPAREN    = ")"
	LBRACE    = "{"
	RBRACE    = "}"

	// Keywords
	STRUCT = "STRUCT"
	ENUM   = "ENUM"
	IMPL   = "IMPL"
	FNC    = "FNC"
	RET    = "RET"
)

var keywords = map[string]TokenType{
	"struct": STRUCT,
	"enum":   ENUM,
	"impl":   IMPL,
	"fnc":    FNC,
	"ret":    RET,
}

// LookupIdent checks if a given identifier is a reserved keyword.
// If it is, it returns the keyword's TokenType. Otherwise, it returns IDENT.
func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
