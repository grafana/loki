package token

import (
    "fmt"
    "github.com/pb33f/jsonpath/pkg/jsonpath/config"
    "strconv"
    "strings"
)

// *****************************************************************************
// The Tokenizer is responsible for tokenizing the jsonpath expression. This means
//  * removing whitespace
//  * scanning strings
//  * and detecting illegal characters
// *****************************************************************************

// Token represents a lexical token in a JSONPath expression.
type Token int

// We are allowed the following tokens

//jsonpath-query      = root-identifier segments
//segments            = *(segment)
//root-identifier     = "$"
//selector            = name-selector /
//                      wildcard-selector /
//                      slice-selector /
//                      index-selector /
//                      filter-selector
//name-selector       = string-literal
//wildcard-selector   = "*"
//index-selector      = int                        ; decimal integer
//
//int                 = "0" /
//                      (["-"] DIGIT1 *DIGIT)      ; - optional
//DIGIT1              = %x31-39                    ; 1-9 non-zero digit
//slice-selector      = [start] ":" [end] [":" [step]]
//
//start               = int       ; included in selection
//end                 = int       ; not included in selection
//step                = int       ; default: 1
//filter-selector     = "?" logical-expr
//logical-expr        = logical-or-expr
//logical-or-expr     = logical-and-expr *("||" logical-and-expr)
//                        ; disjunction
//                        ; binds less tightly than conjunction
//logical-and-expr    = basic-expr *("&&" basic-expr)
//                        ; conjunction
//                        ; binds more tightly than disjunction
//
//basic-expr          = paren-expr /
//                      comparison-expr /
//                      test-expr
//
//paren-expr          = [logical-not-op] "(" logical-expr ")"
//                                        ; parenthesized expression
//logical-not-op      = "!"               ; logical NOT operator
//test-expr           = [logical-not-op S]
//                      (filter-query / ; existence/non-existence
//                       function-expr) ; LogicalType or NodesType
//filter-query        = rel-query / jsonpath-query
//rel-query           = current-node-identifier segments
//current-node-identifier = "@"
//comparison-expr     = comparable comparison-op comparable
//literal             = number / string-literal /
//                      true / false / null
//comparable          = literal /
//                      singular-query / ; singular query value
//                      function-expr    ; ValueType
//comparison-op       = "==" / "!=" /
//                      "<=" / ">=" /
//                      "<"  / ">"
//
//singular-query      = rel-singular-query / abs-singular-query
//rel-singular-query  = current-node-identifier singular-query-segments
//abs-singular-query  = root-identifier singular-query-segments
//singular-query-segments = *(S (name-segment / index-segment))
//name-segment        = ("[" name-selector "]") /
//                      ("." member-name-shorthand)
//index-segment       = "[" index-selector "]"
//number              = (int / "-0") [ frac ] [ exp ] ; decimal number
//frac                = "." 1*DIGIT                  ; decimal fraction
//exp                 = "e" [ "-" / "+" ] 1*DIGIT    ; decimal exponent
//true                = %x74.72.75.65                ; true
//false               = %x66.61.6c.73.65             ; false
//null                = %x6e.75.6c.6c                ; null
//function-name       = function-name-first *function-name-char
//function-name-first = LCALPHA
//function-name-char  = function-name-first / "_" / DIGIT
//LCALPHA             = %x61-7A  ; "a".."z"
//
//function-expr       = function-name "(" [function-argument
//                         *(S "," function-argument)] ")"
//function-argument   = literal /
//                      filter-query / ; (includes singular-query)
//                      logical-expr /
//                      function-expr
//segment             = child-segment / descendant-segment
//child-segment       = bracketed-selection /
//                      ("."
//                       (wildcard-selector /
//                        member-name-shorthand))
//
//bracketed-selection = "[" selector *(S "," selector) "]"
//
//member-name-shorthand = name-first *name-char
//name-first          = ALPHA /
//                      "_"   /
//                      %x80-D7FF /
//                         ; skip surrogate code points
//                      %xE000-10FFFF
//name-char           = name-first / DIGIT
//
//DIGIT               = %x30-39              ; 0-9
//ALPHA               = %x41-5A / %x61-7A    ; A-Z / a-z
//descendant-segment  = ".." (bracketed-selection /
//                            wildcard-selector /
//                            member-name-shorthand)
//
//             Figure 2: Collected ABNF of JSONPath Queries
//
//Figure 3 contains the collected ABNF grammar that defines the syntax
//of a JSONPath Normalized Path while also using the rules root-
//identifier, ESC, DIGIT, and DIGIT1 from Figure 2.
//
//normalized-path      = root-identifier *(normal-index-segment)
//normal-index-segment = "[" normal-selector "]"
//normal-selector      = normal-name-selector / normal-index-selector
//normal-name-selector = %x27 *normal-single-quoted %x27 ; 'string'
//normal-single-quoted = normal-unescaped /
//                       ESC normal-escapable
//normal-unescaped     =    ; omit %x0-1F control codes
//                       %x20-26 /
//                          ; omit 0x27 '
//                       %x28-5B /
//                          ; omit 0x5C \
//                       %x5D-D7FF /
//                          ; skip surrogate code points
//                       %xE000-10FFFF
//
//normal-escapable     = %x62 / ; b BS backspace U+0008
//                       %x66 / ; f FF form feed U+000C
//                       %x6E / ; n LF line feed U+000A
//                       %x72 / ; r CR carriage return U+000D
//                       %x74 / ; t HT horizontal tab U+0009
//                       "'" /  ; ' apostrophe U+0027
//                       "\" /  ; \ backslash (reverse solidus) U+005C
//                       (%x75 normal-hexchar)
//                                       ; certain values u00xx U+00XX
//normal-hexchar       = "0" "0"
//                       (
//                          ("0" %x30-37) / ; "00"-"07"
//                             ; omit U+0008-U+000A BS HT LF
//                          ("0" %x62) /    ; "0b"
//                             ; omit U+000C-U+000D FF CR
//                          ("0" %x65-66) / ; "0e"-"0f"
//                          ("1" normal-HEXDIG)
//                       )
//normal-HEXDIG        = DIGIT / %x61-66    ; "0"-"9", "a"-"f"
//normal-index-selector = "0" / (DIGIT1 *DIGIT)
//                        ; non-negative decimal integer

// The list of tokens.
const (
    ILLEGAL Token = iota
    STRING
    INTEGER
    FLOAT
    STRING_LITERAL
    TRUE
    FALSE
    NULL
    ROOT
    CURRENT
    WILDCARD
    PROPERTY_NAME
    RECURSIVE
    CHILD
    ARRAY_SLICE
    FILTER
    PAREN_LEFT
    PAREN_RIGHT
    BRACKET_LEFT
    BRACKET_RIGHT
    COMMA
    TILDE
    AND
    OR
    NOT
    EQ
    NE
    GT
    GE
    LT
    LE
    MATCHES
    FUNCTION

    // JSONPath Plus context variable tokens
    CONTEXT_PROPERTY        // @property - current property name
    CONTEXT_ROOT            // @root - root node access in filter
    CONTEXT_PARENT          // @parent - parent node reference
    CONTEXT_PARENT_PROPERTY // @parentProperty - parent's property name
    CONTEXT_PATH            // @path - absolute path to current node
    CONTEXT_INDEX           // @index - current array index

    // JSONPath Plus parent selector
    PARENT_SELECTOR // ^ - select parent of current node
)

var SimpleTokens = [...]Token{
    STRING,
    INTEGER,
    STRING_LITERAL,
    CHILD,
    BRACKET_LEFT,
    BRACKET_RIGHT,
    ROOT,
}

var tokens = [...]string{
    ILLEGAL:        "ILLEGAL",
    STRING:         "STRING",
    INTEGER:        "INTEGER",
    FLOAT:          "FLOAT",
    STRING_LITERAL: "STRING_LITERAL",
    TRUE:           "TRUE",
    FALSE:          "FALSE",
    NULL:           "NULL",
    // root node identifier (Section 2.2)
    ROOT: "$",
    // current node identifier (Section 2.3.5)
    // (valid only within filter selectors)
    CURRENT:   "@",
    WILDCARD:  "*",
    RECURSIVE: "..",
    CHILD:     ".",
    // start:end:step array slice operator (Section 2.3.4)
    ARRAY_SLICE: ":",
    // filter selector (Section 2.3.5): selects
    // particular children using a logical
    // expression
    FILTER:        "?",
    PAREN_LEFT:    "(",
    PAREN_RIGHT:   ")",
    BRACKET_LEFT:  "[",
    BRACKET_RIGHT: "]",
    COMMA:         ",",
    TILDE:         "~",
    AND:           "&&",
    OR:            "||",
    NOT:           "!",
    EQ:            "==",
    NE:            "!=",
    GT:            ">",
    GE:            ">=",
    LT:            "<",
    LE:            "<=",
    MATCHES:       "=~",
    FUNCTION:      "FUNCTION",

    // JSONPath Plus context variables
    CONTEXT_PROPERTY:        "@property",
    CONTEXT_ROOT:            "@root",
    CONTEXT_PARENT:          "@parent",
    CONTEXT_PARENT_PROPERTY: "@parentProperty",
    CONTEXT_PATH:            "@path",
    CONTEXT_INDEX:           "@index",

    // JSONPath Plus parent selector
    PARENT_SELECTOR: "^",
}

// String returns the string representation of the token.
func (tok Token) String() string {
    if tok >= 0 && tok < Token(len(tokens)) {
        return tokens[tok]
    }
    return "token(" + strconv.Itoa(int(tok)) + ")"
}

func (tok Tokens) IsSimple() bool {
    if len(tok) == 0 {
        return false
    }
    if tok[0].Token != ROOT {
        return false
    }
    for _, token := range tok {
        isSimple := false
        for _, simpleToken := range SimpleTokens {
            if token.Token == simpleToken {
                isSimple = true
            }
        }
        if !isSimple {
            return false
        }
    }
    return true
}

// When there's an error in the tokenizer, this helps represent it.
func (t Tokenizer) ErrorString(target *TokenInfo, msg string) string {
    var errorBuilder strings.Builder

    var token TokenInfo
    if target == nil {
        // grab last token (as value)
        token = t.tokens[len(t.tokens)-1]
        // set column to +1
        token.Column++
        target = &token
    }

    // Write the error message with line and column information
    errorBuilder.WriteString(fmt.Sprintf("Error at line %d, column %d: %s\n", target.Line, target.Column, msg))

    // Find the start and end positions of the line containing the target token
    lineStart := 0
    lineEnd := len(t.input)
    for i := target.Line - 1; i > 0; i-- {
        if pos := strings.LastIndexByte(t.input[:lineStart], '\n'); pos != -1 {
            lineStart = pos + 1
            break
        }
    }
    if pos := strings.IndexByte(t.input[lineStart:], '\n'); pos != -1 {
        lineEnd = lineStart + pos
    }

    // Extract the line containing the target token
    line := t.input[lineStart:lineEnd]
    errorBuilder.WriteString(line)
    errorBuilder.WriteString("\n")

    // Calculate the number of spaces before the target token
    spaces := strings.Repeat(" ", target.Column)

    // Write the caret symbol pointing to the target token
    errorBuilder.WriteString(spaces)
    dots := ""
    if target.Len > 0 {
        dots = strings.Repeat(".", target.Len-1)
    }
    errorBuilder.WriteString("^" + dots + "\n")

    return errorBuilder.String()
}

// When there's an error
func (t Tokenizer) ErrorTokenString(target *TokenInfo, msg string) string {
    var errorBuilder strings.Builder
    var token TokenInfo
    if target == nil {
        // grab last token (as value)
        token = t.tokens[len(t.tokens)-1]
        // set column to +1
        token.Column++
        target = &token
    }
    // Write the error message with line and column information
    errorBuilder.WriteString(t.ErrorString(target, msg))

    // Find the start and end positions of the line containing the target token
    lineStart := 0
    lineEnd := len(t.input)
    for i := target.Line - 1; i > 0; i-- {
        if pos := strings.LastIndexByte(t.input[:lineStart], '\n'); pos != -1 {
            lineStart = pos + 1
            break
        }
    }
    if pos := strings.IndexByte(t.input[lineStart:], '\n'); pos != -1 {
        lineEnd = lineStart + pos
    }

    // Extract the line containing the target token
    line := t.input[lineStart:lineEnd]

    // Calculate the number of spaces before the target token
    for _, token := range t.tokens {
        errorBuilder.WriteString(line)
        errorBuilder.WriteString("\n")
        spaces := strings.Repeat(" ", token.Column)
        dots := ""
        if token.Len > 0 {
            dots = strings.Repeat(".", token.Len-1)
        }
        errorBuilder.WriteString(spaces)
        errorBuilder.WriteString(fmt.Sprintf("^%s %s\n", dots, tokens[token.Token]))
    }

    return errorBuilder.String()
}

// TokenInfo represents a token and its associated information.
type TokenInfo struct {
    Token   Token
    Line    int
    Column  int
    Literal string
    Len     int
}

// Tokens represents the list of tokens
type Tokens []TokenInfo

// Tokenizer represents a JSONPath tokenizer.
type Tokenizer struct {
    input             string
    pos               int
    line              int
    column            int
    tokens            []TokenInfo
    stack             []Token
    illegalWhitespace bool
    config            config.Config
}

// NewTokenizer creates a new JSONPath tokenizer for the given input string.
func NewTokenizer(input string, opts ...config.Option) *Tokenizer {
    cfg := config.New(opts...)
    return &Tokenizer{
        input:  input,
        config: cfg,
        line:   1,
        stack:  make([]Token, 0),
    }
}

// Tokenize tokenizes the input string and returns a slice of TokenInfo.
func (t *Tokenizer) Tokenize() Tokens {
    for t.pos < len(t.input) {
        if !t.illegalWhitespace {
            t.skipWhitespace()
        }
        if t.pos >= len(t.input) {
            break
        }

        switch ch := t.input[t.pos]; {
        case ch == '$':
            t.addToken(ROOT, 1, "")
        case ch == '@':
            // Check for JSONPath Plus context variables when enabled
            handled := false
            if t.config.JSONPathPlusEnabled() {
                if contextToken, length := t.tryContextVariable(); contextToken != ILLEGAL {
                    t.addToken(contextToken, length, "")
                    // Advance past the token (minus 1 because main loop does pos++)
                    t.pos += length - 1
                    t.column += length - 1
                    handled = true
                }
            }
            if !handled {
                t.addToken(CURRENT, 1, "")
            }
        case ch == '*':
            t.addToken(WILDCARD, 1, "")
        case ch == '~':
            if t.config.PropertyNameEnabled() {
                t.addToken(PROPERTY_NAME, 1, "")
            } else {
                t.addToken(ILLEGAL, 1, "invalid property name token without config.PropertyNameExtension set to true")
            }
        case ch == '^':
            // JSONPath Plus parent selector
            if t.config.JSONPathPlusEnabled() {
                t.addToken(PARENT_SELECTOR, 1, "")
            } else {
                t.addToken(ILLEGAL, 1, "parent selector ^ requires JSONPath Plus mode (enabled by default, disabled with StrictRFC9535)")
            }
        case ch == '.':
            if t.peek() == '.' {
                t.addToken(RECURSIVE, 2, "")
                t.pos++
                t.column++
                t.illegalWhitespace = true
            } else {
                t.addToken(CHILD, 1, "")
                t.illegalWhitespace = true
            }
        case ch == ',':
            t.addToken(COMMA, 1, "")
        case ch == ':':
            t.addToken(ARRAY_SLICE, 1, "")
        case ch == '?':
            t.addToken(FILTER, 1, "")
        case ch == '(':
            t.addToken(PAREN_LEFT, 1, "")
            t.stack = append(t.stack, PAREN_LEFT)
        case ch == ')':
            t.addToken(PAREN_RIGHT, 1, "")
            if len(t.stack) > 0 && t.stack[len(t.stack)-1] == PAREN_LEFT {
                t.stack = t.stack[:len(t.stack)-1]
            } else {
                t.addToken(ILLEGAL, 1, "unmatched closing parenthesis")
            }
        case ch == '[':
            t.addToken(BRACKET_LEFT, 1, "")
            t.stack = append(t.stack, BRACKET_LEFT)
        case ch == ']':
            if len(t.stack) > 0 && t.stack[len(t.stack)-1] == BRACKET_LEFT {
                t.addToken(BRACKET_RIGHT, 1, "")
                t.stack = t.stack[:len(t.stack)-1]
            } else {
                t.addToken(ILLEGAL, 1, "unmatched closing bracket")
            }
        case ch == '&':
            if t.peek() == '&' {
                t.addToken(AND, 2, "")
                t.pos++
                t.column++
            } else {
                t.addToken(ILLEGAL, 1, "invalid token")
            }
        case ch == '|':
            if t.peek() == '|' {
                t.addToken(OR, 2, "")
                t.pos++
                t.column++
            } else {
                t.addToken(ILLEGAL, 1, "invalid token")
            }
        case ch == '!':
            if t.peek() == '=' {
                // Check for JavaScript !== (strict not-equals) - treat as RFC 9535 !=
                if t.pos+2 < len(t.input) && t.input[t.pos+2] == '=' {
                    t.addToken(NE, 3, "") // !== becomes !=
                    t.pos += 2
                    t.column += 2
                } else {
                    t.addToken(NE, 2, "")
                    t.pos++
                    t.column++
                }
            } else {
                t.addToken(NOT, 1, "")
            }
        case ch == '=':
            if t.peek() == '=' {
                // Check for JavaScript === (strict equals) - treat as RFC 9535 ==
                if t.pos+2 < len(t.input) && t.input[t.pos+2] == '=' {
                    t.addToken(EQ, 3, "") // === becomes ==
                    t.pos += 2
                    t.column += 2
                } else {
                    t.addToken(EQ, 2, "")
                    t.pos++
                    t.column++
                }
            } else if t.peek() == '~' {
                t.addToken(MATCHES, 2, "")
                t.pos++
                t.column++
            } else {
                t.addToken(ILLEGAL, 1, "invalid token")
            }
        case ch == '>':
            if t.peek() == '=' {
                t.addToken(GE, 2, "")
                t.pos++
                t.column++
            } else {
                t.addToken(GT, 1, "")
            }
        case ch == '<':
            if t.peek() == '=' {
                t.addToken(LE, 2, "")
                t.pos++
                t.column++
            } else {
                t.addToken(LT, 1, "")
            }
        case ch == '"' || ch == '\'':
            t.scanString(rune(ch))
        case ch == '-' && isDigit(t.peek()):
            fallthrough
        case isDigit(ch):
            t.scanNumber()
        case isLiteralChar(ch):
            t.scanLiteral()
        default:
            t.addToken(ILLEGAL, 1, string(ch))
        }
        t.pos++
        t.column++
    }

    if len(t.stack) > 0 {
        t.addToken(ILLEGAL, 1, fmt.Sprintf("unmatched %s", t.stack[len(t.stack)-1].String()))
    }
    return t.tokens
}

func (t *Tokenizer) addToken(token Token, len int, literal string) {
    t.tokens = append(t.tokens, TokenInfo{
        Token:   token,
        Line:    t.line,
        Column:  t.column,
        Len:     len,
        Literal: literal,
    })
    t.illegalWhitespace = false
}

func (t *Tokenizer) scanString(quote rune) {
    start := t.pos + 1
    var literal strings.Builder
illegal:
    for i := start; i < len(t.input); i++ {
        b := literal.String()
        _ = b
        if t.input[i] == byte(quote) {
            t.addToken(STRING_LITERAL, len(t.input[start:i])+2, literal.String())
            t.pos = i
            t.column += i - start + 1
            return
        }
        if t.input[i] == '\\' {
            i++
            if i >= len(t.input) {
                t.addToken(ILLEGAL, len(t.input[start:]), literal.String())
                t.pos = len(t.input) - 1
                t.column = len(t.input) - 1
                return
            }
            switch t.input[i] {
            case 'b':
                literal.WriteByte('\b')
            case 'f':
                literal.WriteByte('\f')
            case 'n':
                literal.WriteByte('\n')
            case 'r':
                literal.WriteByte('\n')
            case 't':
                literal.WriteByte('\t')
            case '\'':
                if quote != '\'' {
                    // don't escape it, when we're not in a single quoted string
                    break illegal
                } else {
                    literal.WriteByte(t.input[i])
                }
            case '"':
                if quote != '"' {
                    // don't escape it, when we're not in a single quoted string
                    break illegal
                } else {
                    literal.WriteByte(t.input[i])
                }
            case '\\', '/':
                literal.WriteByte(t.input[i])
            default:
                break illegal
            }
        } else {
            literal.WriteByte(t.input[i])
        }
    }
    t.addToken(ILLEGAL, len(t.input[start:]), literal.String())
    t.pos = len(t.input) - 1
    t.column = len(t.input) - 1
}

func (t *Tokenizer) scanNumber() {
    start := t.pos
    tokenType := INTEGER
    dotSeen := false
    exponentSeen := false

    for i := start; i < len(t.input); i++ {
        if i == start && t.input[i] == '-' {
            continue
        }

        if t.input[i] == '.' {
            if dotSeen || exponentSeen {
                t.addToken(ILLEGAL, len(t.input[start:i]), t.input[start:i])
                t.pos = i
                t.column += i - start
                return
            }
            tokenType = FLOAT
            dotSeen = true
            continue
        }

        if t.input[i] == 'e' || t.input[i] == 'E' {
            if exponentSeen || (len(t.input) > 0 && t.input[i-1] == '.') {
                t.addToken(ILLEGAL, len(t.input[start:i]), t.input[start:i])
                t.pos = i
                t.column += i - start
                return
            }
            tokenType = FLOAT
            exponentSeen = true
            if i+1 < len(t.input) && (t.input[i+1] == '+' || t.input[i+1] == '-') {
                i++
            }
            continue
        }

        if !isDigit(t.input[i]) {
            literal := t.input[start:i]
            // check for legal numbers
            _, err := strconv.ParseFloat(literal, 64)
            if err != nil {
                tokenType = ILLEGAL
            }
            // conformance spec
            if len(literal) > 1 && literal[0] == '0' && !dotSeen {
                // no leading zero
                tokenType = ILLEGAL
            } else if len(literal) > 2 && literal[0] == '-' && literal[1] == '0' && !dotSeen {
                // no trailing dot
                tokenType = ILLEGAL
            } else if len(literal) > 0 && literal[len(literal)-1] == '.' {
                // no trailing dot
                tokenType = ILLEGAL
            } else if literal[len(literal)-1] == 'e' || literal[len(literal)-1] == 'E' {
                // no exponent
                tokenType = ILLEGAL
            }

            t.addToken(tokenType, len(literal), literal)
            t.pos = i - 1
            t.column += i - start - 1
            return
        }
    }

    if exponentSeen && !isDigit(t.input[len(t.input)-1]) {
        t.addToken(ILLEGAL, len(t.input[start:]), t.input[start:])
        t.pos = len(t.input) - 1
        t.column = len(t.input) - 1
        return
    }

    literal := t.input[start:]
    t.addToken(tokenType, len(literal), literal)
    t.pos = len(t.input) - 1
    t.column = len(t.input) - 1
}

func (t *Tokenizer) scanLiteral() {
    start := t.pos
    for i := start; i < len(t.input); i++ {
        if !isLiteralChar(t.input[i]) && !isDigit(t.input[i]) {
            literal := t.input[start:i]
            switch literal {
            case "true":
                t.addToken(TRUE, len(literal), literal)
            case "false":
                t.addToken(FALSE, len(literal), literal)
            case "null":
                t.addToken(NULL, len(literal), literal)
            default:
                // Only treat as FUNCTION if it's a function name AND followed by '('
                // Otherwise it's a property name (STRING)
                if isFunctionName(literal) && i < len(t.input) && t.input[i] == '(' {
                    t.addToken(FUNCTION, len(literal), literal)
                    t.illegalWhitespace = true
                } else {
                    t.addToken(STRING, len(literal), literal)
                }
            }
            t.pos = i - 1
            t.column += i - start - 1
            return
        }
    }
    literal := t.input[start:]
    switch literal {
    case "true":
        t.addToken(TRUE, len(literal), literal)
    case "false":
        t.addToken(FALSE, len(literal), literal)
    case "null":
        t.addToken(NULL, len(literal), literal)
    default:
        t.addToken(STRING, len(literal), literal)
    }
    t.pos = len(t.input) - 1
    t.column = len(t.input) - 1
}

func isFunctionName(literal string) bool {
    switch literal {
    // RFC 9535 standard functions
    case "length", "count", "match", "search", "value":
        return true
    // JSONPath Plus type selector functions
    case "isNull", "isBoolean", "isNumber", "isString", "isArray", "isObject", "isInteger":
        return true
    }
    return false
}

func (t *Tokenizer) skipWhitespace() {
    //   S                   = *B        ; optional blank space
    //   B                   = %x20 /    ; Space
    //                         %x09 /    ; Horizontal tab
    //                         %x0A /    ; Line feed or New line
    //                         %x0D      ; Carriage return
    for len(t.tokens) > 0 && t.pos+1 < len(t.input) {
        ch := t.input[t.pos]
        if ch == '\n' {
            t.line++
            t.pos++
            t.column = 0
        } else if !isSpace(ch) {
            break
        } else {
            t.pos++
            t.column++
        }
    }
}

func (t *Tokenizer) peek() byte {
    if t.pos+1 < len(t.input) {
        return t.input[t.pos+1]
    }
    return 0
}

func isDigit(ch byte) bool {
    return '0' <= ch && ch <= '9'
}

func isLiteralChar(ch byte) bool {
    // allow unicode characters
    return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_' || ch >= 0x80
}

func isSpace(ch byte) bool {
    return ch == ' ' || ch == '\t' || ch == '\r'
}

// contextVariableKeywords maps context variable names to their token types.
// These are JSONPath Plus extensions for accessing filter context.
var contextVariableKeywords = map[string]Token{
    "property":       CONTEXT_PROPERTY,
    "root":           CONTEXT_ROOT,
    "parent":         CONTEXT_PARENT,
    "parentProperty": CONTEXT_PARENT_PROPERTY,
    "path":           CONTEXT_PATH,
    "index":          CONTEXT_INDEX,
}

// tryContextVariable checks if the current position starts a context variable.
// It returns the token type and total length (including @) if found, or ILLEGAL and 0 if not.
// Context variables are @property, @root, @parent, @parentProperty, @path, @index.
func (t *Tokenizer) tryContextVariable() (Token, int) {
    // Must start with @
    if t.pos >= len(t.input) || t.input[t.pos] != '@' {
        return ILLEGAL, 0
    }

    // Extract the word following @
    start := t.pos + 1
    if start >= len(t.input) {
        return ILLEGAL, 0
    }

    // Find the end of the identifier
    end := start
    for end < len(t.input) && isLiteralChar(t.input[end]) {
        end++
    }

    if end == start {
        return ILLEGAL, 0
    }

    keyword := t.input[start:end]
    if tok, ok := contextVariableKeywords[keyword]; ok {
        // Return the token and total length including @
        return tok, end - t.pos
    }

    return ILLEGAL, 0
}
