package jsonpath

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pb33f/jsonpath/pkg/jsonpath/token"
)

type spectralBoolKind uint8

const (
	spectralBoolValue spectralBoolKind = iota
	spectralBoolNot
	spectralBoolAnd
	spectralBoolOr
	spectralBoolCompare
	spectralBoolParen
)

type spectralBoolExpr struct {
	kind                     spectralBoolKind
	value                    *spectralValueExpr
	left                     *spectralBoolExpr
	right                    *spectralBoolExpr
	op                       token.Token
	usage                    contextVarUsage
	usesPropertyNameSelector bool
}

func (e *spectralBoolExpr) String() string {
	if e == nil {
		return ""
	}
	switch e.kind {
	case spectralBoolValue:
		return e.value.String()
	case spectralBoolNot:
		return "!" + e.left.String()
	case spectralBoolAnd:
		return e.left.String() + " && " + e.right.String()
	case spectralBoolOr:
		return e.left.String() + " || " + e.right.String()
	case spectralBoolCompare:
		return e.left.String() + " " + e.op.String() + " " + e.right.String()
	case spectralBoolParen:
		return "(" + e.left.String() + ")"
	default:
		return ""
	}
}

type spectralValueKind uint8

const (
	spectralValueLiteral spectralValueKind = iota
	spectralValueCurrent
	spectralValueRoot
	spectralValueContext
	spectralValueRegex
	spectralValueUndefined
	spectralValueFunction
)

type spectralPathSegment struct {
	name  string
	index *int64
}

type spectralPostfixKind uint8

const (
	spectralPostfixMatch spectralPostfixKind = iota
	spectralPostfixIndexOf
	spectralPostfixLength
	spectralPostfixConstructorName
	spectralPostfixIncludes
)

type spectralPostfix struct {
	kind      spectralPostfixKind
	regex     *spectralRegex
	search    string
	fromIndex *float64
	argument  literal
}

type spectralRegex struct {
	source   string
	pattern  string
	flags    string
	compiled *regexp.Regexp
}

type spectralValueExpr struct {
	kind     spectralValueKind
	literal  literal
	context  contextVarKind
	regex    *spectralRegex
	function *functionExpr
	segments []spectralPathSegment
	postfix  []spectralPostfix
}

func (e *spectralValueExpr) String() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	switch e.kind {
	case spectralValueLiteral:
		b.WriteString(e.literal.ToString())
	case spectralValueCurrent:
		b.WriteByte('@')
	case spectralValueRoot:
		b.WriteByte('$')
	case spectralValueContext:
		b.WriteString(contextVariable{kind: e.context}.ToString())
	case spectralValueRegex:
		b.WriteString(e.regex.source)
	case spectralValueUndefined:
		b.WriteString("void 0")
	case spectralValueFunction:
		b.WriteString(e.function.ToString())
	}
	for _, segment := range e.segments {
		if segment.index != nil {
			b.WriteByte('[')
			b.WriteString(strconv.FormatInt(*segment.index, 10))
			b.WriteByte(']')
		} else if spectralDotMemberName(segment.name) {
			b.WriteByte('.')
			b.WriteString(segment.name)
		} else {
			b.WriteString("['")
			b.WriteString(escapeString(segment.name))
			b.WriteString("']")
		}
	}
	for _, postfix := range e.postfix {
		switch postfix.kind {
		case spectralPostfixMatch:
			b.WriteString(".match(")
			b.WriteString(postfix.regex.source)
			b.WriteByte(')')
		case spectralPostfixIndexOf:
			b.WriteString(".indexOf('")
			b.WriteString(escapeString(postfix.search))
			b.WriteByte('\'')
			if postfix.fromIndex != nil {
				b.WriteString(", ")
				b.WriteString(strconv.FormatFloat(*postfix.fromIndex, 'f', -1, 64))
			}
			b.WriteByte(')')
		case spectralPostfixLength:
			b.WriteString(".length")
		case spectralPostfixConstructorName:
			b.WriteString(".constructor.name")
		case spectralPostfixIncludes:
			b.WriteString(".includes(")
			b.WriteString(postfix.argument.ToString())
			if postfix.fromIndex != nil {
				b.WriteString(", ")
				b.WriteString(strconv.FormatFloat(*postfix.fromIndex, 'f', -1, 64))
			}
			b.WriteByte(')')
		}
	}
	return b.String()
}

func spectralDotMemberName(name string) bool {
	if name == "$ref" {
		return true
	}
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_' || ch >= 0x80 {
			continue
		}
		if i > 0 && '0' <= ch && ch <= '9' {
			continue
		}
		return false
	}
	return true
}

func (p *JSONPath) parseSpectralExpression() (*spectralBoolExpr, error) {
	expr, err := p.parseSpectralOr()
	if err != nil {
		return nil, err
	}
	if p.current >= len(p.tokens) || p.tokens[p.current].Token != token.BRACKET_RIGHT {
		return nil, p.spectralFailure(p.current, "expected the end of the Spectral filter expression")
	}
	return expr, nil
}

func (p *JSONPath) parseSpectralOr() (*spectralBoolExpr, error) {
	left, err := p.parseSpectralAnd()
	if err != nil {
		return nil, err
	}
	for p.spectralNext(token.OR) {
		p.current++
		right, err := p.parseSpectralAnd()
		if err != nil {
			return nil, err
		}
		left = p.spectralBool(spectralBoolOr, left, right, 0)
	}
	return left, nil
}

func (p *JSONPath) parseSpectralAnd() (*spectralBoolExpr, error) {
	left, err := p.parseSpectralUnary()
	if err != nil {
		return nil, err
	}
	for p.spectralNext(token.AND) {
		p.current++
		right, err := p.parseSpectralUnary()
		if err != nil {
			return nil, err
		}
		left = p.spectralBool(spectralBoolAnd, left, right, 0)
	}
	return left, nil
}

func (p *JSONPath) parseSpectralUnary() (*spectralBoolExpr, error) {
	if p.spectralNext(token.NOT) {
		p.current++
		child, err := p.parseSpectralUnary()
		if err != nil {
			return nil, err
		}
		return p.spectralBool(spectralBoolNot, child, nil, 0), nil
	}
	if p.spectralNext(token.PAREN_LEFT) {
		p.current++
		child, err := p.parseSpectralOr()
		if err != nil {
			return nil, err
		}
		if !p.spectralNext(token.PAREN_RIGHT) {
			return nil, p.spectralFailure(p.current, "expected ')' in Spectral filter expression")
		}
		p.current++
		return p.spectralBool(spectralBoolParen, child, nil, 0), nil
	}
	return p.parseSpectralComparison()
}

func (p *JSONPath) parseSpectralComparison() (*spectralBoolExpr, error) {
	left, err := p.parseSpectralValue()
	if err != nil {
		return nil, err
	}
	leftBool := p.spectralValueBool(left)
	if p.current >= len(p.tokens) || !isSpectralComparison(p.tokens[p.current].Token) {
		if len(left.postfix) > 0 && left.postfix[len(left.postfix)-1].kind == spectralPostfixMatch {
			return leftBool, nil
		}
		return leftBool, nil
	}
	op := p.tokens[p.current].Token
	if len(left.postfix) > 0 && left.postfix[len(left.postfix)-1].kind == spectralPostfixMatch {
		return nil, p.spectralFailure(p.current, "match(...) results are only supported as truthy tests and cannot be compared or chained")
	}
	p.current++
	right, err := p.parseSpectralValue()
	if err != nil {
		return nil, err
	}
	if len(right.postfix) > 0 && right.postfix[len(right.postfix)-1].kind == spectralPostfixMatch {
		return nil, p.spectralFailure(p.current-1, "match(...) results are only supported as truthy tests and cannot be compared or chained")
	}
	return p.spectralBool(spectralBoolCompare, leftBool, p.spectralValueBool(right), op), nil
}

func (p *JSONPath) parseSpectralValue() (*spectralValueExpr, error) {
	if p.current >= len(p.tokens) {
		return nil, p.spectralFailure(p.current, "expected a Spectral value expression")
	}
	tok := p.tokens[p.current]
	value := &spectralValueExpr{}
	switch tok.Token {
	case token.STRING_LITERAL, token.INTEGER, token.FLOAT, token.TRUE, token.FALSE, token.NULL:
		lit, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		value.kind = spectralValueLiteral
		value.literal = *lit
	case token.CURRENT:
		value.kind = spectralValueCurrent
		p.current++
	case token.ROOT, token.CONTEXT_ROOT:
		value.kind = spectralValueRoot
		p.current++
	case token.CONTEXT_PROPERTY, token.CONTEXT_PARENT, token.CONTEXT_PARENT_PROPERTY, token.CONTEXT_PATH, token.CONTEXT_INDEX:
		value.kind = spectralValueContext
		value.context = contextVarTokenMap[tok.Token]
		p.current++
	case token.REGEX:
		rx, err := compileSpectralRegex(tok.Literal)
		if err != nil {
			return nil, p.spectralFailure(p.current, err.Error())
		}
		value.kind = spectralValueRegex
		value.regex = rx
		p.current++
	case token.FUNCTION:
		function, err := p.parseFunctionExpr()
		if err != nil {
			return nil, err
		}
		value.kind = spectralValueFunction
		value.function = function
	case token.STRING:
		if tok.Literal != "void" || p.current+1 >= len(p.tokens) || p.tokens[p.current+1].Token != token.INTEGER || p.tokens[p.current+1].Literal != "0" {
			return nil, p.spectralFailure(p.current, "unsupported identifier; only the safe undefined expression 'void 0' is supported")
		}
		value.kind = spectralValueUndefined
		p.current += 2
	default:
		return nil, p.spectralFailure(p.current, "expected a literal, query, context value or regex literal")
	}

	for p.current < len(p.tokens) {
		if len(value.postfix) > 0 && (p.tokens[p.current].Token == token.CHILD || p.tokens[p.current].Token == token.BRACKET_LEFT) {
			return nil, p.spectralFailure(p.current, "postfix results cannot be chained in Spectral compatibility mode")
		}
		if p.tokens[p.current].Token == token.BRACKET_LEFT {
			if value.kind != spectralValueCurrent && value.kind != spectralValueRoot {
				return nil, p.spectralFailure(p.current, "computed member access is not supported in Spectral compatibility mode")
			}
			if err := p.parseSpectralBracketSegment(value); err != nil {
				return nil, err
			}
			continue
		}
		if p.tokens[p.current].Token != token.CHILD {
			break
		}
		childAt := p.current
		p.current++
		if p.current >= len(p.tokens) {
			return nil, p.spectralFailure(childAt, "expected a member or method name after '.'")
		}
		nameToken := p.tokens[p.current]
		nameTokenWidth := 1
		if nameToken.Token == token.ROOT {
			if p.current+1 >= len(p.tokens) || p.tokens[p.current+1].Token != token.STRING || p.tokens[p.current+1].Literal != "ref" {
				return nil, p.spectralFailure(p.current, "only $ref is supported as a '$'-prefixed member name")
			}
			nameToken.Literal = "$ref"
			nameTokenWidth = 2
		} else if nameToken.Token != token.STRING && nameToken.Token != token.FUNCTION {
			return nil, p.spectralFailure(p.current, "expected a member or method name after '.'")
		}
		name := nameToken.Literal
		p.current += nameTokenWidth
		if p.spectralNext(token.PAREN_LEFT) {
			if err := p.parseSpectralMethod(value, name, childAt); err != nil {
				return nil, err
			}
			continue
		}
		switch name {
		case "length":
			value.postfix = append(value.postfix, spectralPostfix{kind: spectralPostfixLength})
		case "constructor":
			if !p.spectralNext(token.CHILD) || p.current+1 >= len(p.tokens) || p.tokens[p.current+1].Literal != "name" {
				return nil, p.spectralFailure(childAt, "only the complete read-only constructor.name shape is supported")
			}
			p.current += 2
			value.postfix = append(value.postfix, spectralPostfix{kind: spectralPostfixConstructorName})
		case "__proto__", "prototype", "__defineGetter__", "__defineSetter__":
			return nil, p.spectralFailure(childAt, "prototype and executable-object access is not supported")
		default:
			if len(value.postfix) > 0 {
				return nil, p.spectralFailure(childAt, "postfix results cannot be chained in Spectral compatibility mode")
			}
			if value.kind != spectralValueCurrent && value.kind != spectralValueRoot {
				return nil, p.spectralFailure(childAt, "member access is only supported on singular queries")
			}
			value.segments = append(value.segments, spectralPathSegment{name: name})
		}
	}
	return value, nil
}

func (p *JSONPath) parseSpectralBracketSegment(value *spectralValueExpr) error {
	start := p.current
	p.current++
	if p.current >= len(p.tokens) {
		return p.spectralFailure(start, "unterminated computed member access")
	}
	tok := p.tokens[p.current]
	segment := spectralPathSegment{}
	switch tok.Token {
	case token.STRING_LITERAL:
		if tok.Literal == "constructor" || tok.Literal == "__proto__" || tok.Literal == "prototype" {
			return p.spectralFailure(p.current, "computed constructor and prototype access is not supported")
		}
		segment.name = tok.Literal
	case token.INTEGER:
		index, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			return p.spectralFailure(p.current, "invalid singular query index")
		}
		segment.index = &index
	default:
		return p.spectralFailure(p.current, "only literal member names and indexes are supported in singular queries")
	}
	p.current++
	if !p.spectralNext(token.BRACKET_RIGHT) {
		return p.spectralFailure(p.current, "expected ']' after singular query member")
	}
	p.current++
	value.segments = append(value.segments, segment)
	return nil
}

func (p *JSONPath) parseSpectralMethod(value *spectralValueExpr, name string, at int) error {
	if len(value.postfix) > 0 {
		return p.spectralFailure(at, "method results cannot be chained in Spectral compatibility mode")
	}
	p.current++
	switch name {
	case "match":
		if !p.spectralNext(token.REGEX) {
			return p.spectralFailure(p.current, "match requires one regex literal argument")
		}
		rx, err := compileSpectralRegex(p.tokens[p.current].Literal)
		if err != nil {
			return p.spectralFailure(p.current, err.Error())
		}
		p.current++
		if !p.spectralNext(token.PAREN_RIGHT) {
			return p.spectralFailure(p.current, "match requires exactly one regex literal argument")
		}
		p.current++
		value.postfix = append(value.postfix, spectralPostfix{kind: spectralPostfixMatch, regex: rx})
	case "indexOf":
		if !p.spectralNext(token.STRING_LITERAL) {
			return p.spectralFailure(p.current, "indexOf requires a string argument")
		}
		postfix := spectralPostfix{kind: spectralPostfixIndexOf, search: p.tokens[p.current].Literal}
		p.current++
		if p.spectralNext(token.COMMA) {
			p.current++
			if p.current >= len(p.tokens) || (p.tokens[p.current].Token != token.INTEGER && p.tokens[p.current].Token != token.FLOAT) {
				return p.spectralFailure(p.current, "indexOf fromIndex must be numeric")
			}
			from, err := strconv.ParseFloat(p.tokens[p.current].Literal, 64)
			if err != nil {
				return p.spectralFailure(p.current, "invalid indexOf fromIndex")
			}
			postfix.fromIndex = &from
			p.current++
		}
		if !p.spectralNext(token.PAREN_RIGHT) {
			return p.spectralFailure(p.current, "indexOf accepts one string and an optional numeric fromIndex")
		}
		p.current++
		value.postfix = append(value.postfix, postfix)
	case "includes":
		argument, err := p.parseLiteral()
		if err != nil {
			return p.spectralFailure(p.current, "includes requires a scalar literal argument")
		}
		postfix := spectralPostfix{kind: spectralPostfixIncludes, argument: *argument}
		if p.spectralNext(token.COMMA) {
			p.current++
			if p.current >= len(p.tokens) || (p.tokens[p.current].Token != token.INTEGER && p.tokens[p.current].Token != token.FLOAT) {
				return p.spectralFailure(p.current, "includes fromIndex must be numeric")
			}
			from, err := strconv.ParseFloat(p.tokens[p.current].Literal, 64)
			if err != nil {
				return p.spectralFailure(p.current, "invalid includes fromIndex")
			}
			postfix.fromIndex = &from
			p.current++
		}
		if !p.spectralNext(token.PAREN_RIGHT) {
			return p.spectralFailure(p.current, "includes accepts one scalar and an optional numeric fromIndex")
		}
		p.current++
		value.postfix = append(value.postfix, postfix)
	default:
		return p.spectralFailure(at, fmt.Sprintf("unsupported method %q; supported methods are match, indexOf and includes", name))
	}
	return nil
}

func (p *JSONPath) spectralBool(kind spectralBoolKind, left, right *spectralBoolExpr, op token.Token) *spectralBoolExpr {
	expr := &spectralBoolExpr{kind: kind, left: left, right: right, op: op}
	if left != nil {
		expr.usage = left.usage
		expr.usesPropertyNameSelector = left.usesPropertyNameSelector
	}
	if right != nil {
		expr.usage.property = expr.usage.property || right.usage.property
		expr.usage.parent = expr.usage.parent || right.usage.parent
		expr.usage.parentProperty = expr.usage.parentProperty || right.usage.parentProperty
		expr.usage.path = expr.usage.path || right.usage.path
		expr.usage.index = expr.usage.index || right.usage.index
		expr.usesPropertyNameSelector = expr.usesPropertyNameSelector || right.usesPropertyNameSelector
	}
	return expr
}

func (p *JSONPath) spectralValueBool(value *spectralValueExpr) *spectralBoolExpr {
	expr := &spectralBoolExpr{kind: spectralBoolValue, value: value}
	switch value.kind {
	case spectralValueContext:
		expr.usage.mark(value.context)
	case spectralValueFunction:
		value.function.collectContextVarUsage(&expr.usage)
		expr.usesPropertyNameSelector = value.function.hasPropertyNameReferences()
	}
	return expr
}

func (p *JSONPath) spectralNext(tok token.Token) bool {
	return p.current < len(p.tokens) && p.tokens[p.current].Token == tok
}

func (p *JSONPath) spectralFailure(at int, message string) error {
	if len(p.tokens) == 0 {
		return fmt.Errorf("spectral compatibility mode: %s", message)
	}
	if at >= len(p.tokens) {
		at = len(p.tokens) - 1
	}
	return p.parseFailure(&p.tokens[at], "Spectral compatibility mode: "+message)
}

func isSpectralComparison(tok token.Token) bool {
	switch tok {
	case token.EQ, token.NE, token.STRICT_EQ, token.STRICT_NE, token.GT, token.GE, token.LT, token.LE:
		return true
	default:
		return false
	}
}

func compileSpectralRegex(source string) (*spectralRegex, error) {
	if len(source) < 2 || source[0] != '/' {
		return nil, fmt.Errorf("invalid regex literal")
	}
	closing := -1
	escaped := false
	inClass := false
	for i := 1; i < len(source); i++ {
		ch := source[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '[' {
			inClass = true
		} else if ch == ']' {
			inClass = false
		} else if ch == '/' && !inClass {
			closing = i
			break
		}
	}
	if closing < 0 {
		return nil, fmt.Errorf("unterminated regex literal")
	}
	pattern := source[1:closing]
	flags := source[closing+1:]
	seen := [256]bool{}
	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		if seen[flag] {
			return nil, fmt.Errorf("duplicate regex flag %q", flag)
		}
		seen[flag] = true
	}
	var modes strings.Builder
	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		switch flag {
		case 'i', 'm', 's':
			modes.WriteByte(flag)
		case 'g':
		case 'u':
			if strings.Contains(pattern, `\u{`) {
				return nil, fmt.Errorf("regex u flag code-point escapes are not supported by the safe RE2 evaluator")
			}
		case 'd', 'v', 'y':
			return nil, fmt.Errorf("regex flag %q is not supported by the safe RE2 evaluator", flag)
		default:
			return nil, fmt.Errorf("unknown regex flag %q", flag)
		}
	}
	if err := validateSpectralRegexPattern(pattern); err != nil {
		return nil, err
	}
	pattern = strings.ReplaceAll(pattern, `\/`, `/`)
	compiledPattern := pattern
	if modes.Len() > 0 {
		compiledPattern = "(?" + modes.String() + ")" + pattern
	}
	compiled, err := regexp.Compile(compiledPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid or unsupported regex: %v", err)
	}
	return &spectralRegex{source: source, pattern: pattern, flags: flags, compiled: compiled}, nil
}

func validateSpectralRegexPattern(pattern string) error {
	inClass := false
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 >= len(pattern) {
				return fmt.Errorf("malformed regex escape")
			}
			if pattern[i+1] >= '1' && pattern[i+1] <= '9' {
				return fmt.Errorf("regex backreferences are not supported by the safe RE2 evaluator")
			}
			i++
		case '[':
			if !inClass {
				inClass = true
			}
		case ']':
			if inClass {
				inClass = false
			}
		case '(':
			if inClass || i+2 >= len(pattern) || pattern[i+1] != '?' {
				continue
			}
			if pattern[i+2] == '=' || pattern[i+2] == '!' ||
				(pattern[i+2] == '<' && i+3 < len(pattern) && (pattern[i+3] == '=' || pattern[i+3] == '!')) {
				return fmt.Errorf("regex lookaround is not supported by the safe RE2 evaluator")
			}
		}
	}
	return nil
}
