package computetest

// token is the set of lexical tokens for the compute DSL.
type token int

const (
	tokenIllegal token = iota
	tokenEOF
	tokenTerm // \n (statement terminator)

	tokenIdent   // foo
	tokenInteger // 12345 (also used for unsigned)
	tokenString  // "quoted string"
	tokenSelect  // "select"

	tokenSub // - (used for negative integers)

	tokenColon  // ":"
	tokenArrow  // "->"
	tokenLBrack // "["
	tokenRBrack // "]"
)

var tokenNames = [...]string{
	tokenIllegal: "ILLEGAL",
	tokenEOF:     "EOF",
	tokenTerm:    "TERMINATOR",

	tokenIdent:   "IDENT",
	tokenInteger: "INTEGER",
	tokenString:  "STRING",
	tokenSelect:  "select",

	tokenSub: "-",

	tokenColon:  ":",
	tokenArrow:  "->",
	tokenLBrack: "[",
	tokenRBrack: "]",
}

func (t token) String() string {
	if int(t) < len(tokenNames) {
		return tokenNames[t]
	}
	return "ILLEGAL"
}
