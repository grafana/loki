package jsonpath

import (
    "errors"
    "fmt"
    "github.com/pb33f/jsonpath/pkg/jsonpath/config"
    "github.com/pb33f/jsonpath/pkg/jsonpath/token"
    "strconv"
    "strings"
)

const MaxSafeFloat int64 = 9007199254740991

type mode int

const (
    modeNormal mode = iota
    modeSingular
)

// contextVarTokenMap maps context variable tokens to their kinds
// CONTEXT_ROOT is handled separately as it requires path parsing
var contextVarTokenMap = map[token.Token]contextVarKind{
    token.CONTEXT_PROPERTY:        contextVarProperty,
    token.CONTEXT_PARENT:          contextVarParent,
    token.CONTEXT_PARENT_PROPERTY: contextVarParentProperty,
    token.CONTEXT_PATH:            contextVarPath,
    token.CONTEXT_INDEX:           contextVarIndex,
}

// JSONPath represents a JSONPath parser.
type JSONPath struct {
    tokenizer *token.Tokenizer
    tokens    []token.TokenInfo
    ast       jsonPathAST
    current   int
    mode      []mode
    config    config.Config
}

// newParserPrivate creates a new JSONPath with the given tokens.
func newParserPrivate(tokenizer *token.Tokenizer, tokens []token.TokenInfo, opts ...config.Option) *JSONPath {
    return &JSONPath{tokenizer, tokens, jsonPathAST{}, 0, []mode{modeNormal}, config.New(opts...)}
}

// parse parses the JSONPath tokens and returns the root node of the AST.
//
//	jsonpath-query      = root-identifier segments
func (p *JSONPath) parse() error {
    if len(p.tokens) == 0 {
        return fmt.Errorf("empty JSONPath expression")
    }

    if p.tokens[p.current].Token != token.ROOT {
        return p.parseFailure(&p.tokens[p.current], "expected '$'")
    }
    p.current++

    for p.current < len(p.tokens) {
        segment, err := p.parseSegment()
        if err != nil {
            return err
        }
        p.ast.segments = append(p.ast.segments, segment)
    }
    return nil
}

func (p *JSONPath) parseFailure(target *token.TokenInfo, msg string) error {
    return errors.New(p.tokenizer.ErrorString(target, msg))
}

// peek returns true if the upcoming token matches the given token type.
func (p *JSONPath) peek(token token.Token) bool {
    return p.current+1 < len(p.tokens) && p.tokens[p.current+1].Token == token
}

// peek returns true if the upcoming token matches the given token type.
func (p *JSONPath) next(token token.Token) bool {
    return p.current < len(p.tokens) && p.tokens[p.current].Token == token
}

// expect consumes the current token if it matches the given token type.
func (p *JSONPath) expect(token token.Token) bool {
    if p.peek(token) {
        p.current++
        return true
    }
    return false
}

// isComparisonOperator returns true if the given token is a comparison operator.
func (p *JSONPath) isComparisonOperator(tok token.Token) bool {
    return tok == token.EQ || tok == token.NE || tok == token.GT || tok == token.GE || tok == token.LT || tok == token.LE
}

func (p *JSONPath) parseSegment() (*segment, error) {
    currentToken := p.tokens[p.current]
    if currentToken.Token == token.RECURSIVE {
        if p.mode[len(p.mode)-1] == modeSingular {
            return nil, p.parseFailure(&p.tokens[p.current], "unexpected recursive descent in singular query")
        }
        p.current++
        child, err := p.parseInnerSegment()
        if err != nil {
            return nil, err
        }
        return &segment{kind: segmentKindDescendant, descendant: child}, nil
    } else if currentToken.Token == token.CHILD || currentToken.Token == token.BRACKET_LEFT {
        if currentToken.Token == token.CHILD {
            p.current++
        }
        child, err := p.parseInnerSegment()
        if err != nil {
            return nil, err
        }
        return &segment{kind: segmentKindChild, child: child}, nil
    } else if p.config.PropertyNameEnabled() && currentToken.Token == token.PROPERTY_NAME {
        p.current++
        return &segment{kind: segmentKindProperyName}, nil
    } else if p.config.JSONPathPlusEnabled() && currentToken.Token == token.PARENT_SELECTOR {
        // JSONPath Plus parent selector: ^ returns parent of current node
        p.current++
        return &segment{kind: segmentKindParent}, nil
    }
    return nil, p.parseFailure(&currentToken, "unexpected token when parsing segment")
}

func (p *JSONPath) parseInnerSegment() (retValue *innerSegment, err error) {
    defer func() {
        if p.mode[len(p.mode)-1] == modeSingular && retValue != nil {
            if len(retValue.selectors) > 1 {
                retValue = nil
                err = p.parseFailure(&p.tokens[p.current], "unexpected multiple selectors in singular query")
                return
            } else if retValue.kind == segmentDotWildcard {
                retValue = nil
                err = p.parseFailure(&p.tokens[p.current], "unexpected wildcard in singular query")
                return
            }
        }
    }()
    // .*
    // .STRING
    // []
    if p.current >= len(p.tokens) {
        return nil, p.parseFailure(nil, "unexpected end of input")
    }
    firstToken := p.tokens[p.current]
    if firstToken.Token == token.WILDCARD {
        p.current += 1
        return &innerSegment{segmentDotWildcard, "", nil}, nil
    } else if firstToken.Token == token.STRING {
        dotName := p.tokens[p.current].Literal
        p.current += 1
        return &innerSegment{segmentDotMemberName, dotName, nil}, nil
    } else if firstToken.Token == token.BRACKET_LEFT {
        prior := p.current
        p.current += 1
        selectors := []*selector{}
        for p.current < len(p.tokens) {
            innerSelector, err := p.parseSelector()
            if err != nil {
                p.current = prior
                return nil, err
            }
            selectors = append(selectors, innerSelector)
            if len(p.tokens) <= p.current {
                return nil, p.parseFailure(&p.tokens[p.current-1], "unexpected end of input")
            }
            if p.tokens[p.current].Token == token.BRACKET_RIGHT {
                break
            } else if p.tokens[p.current].Token == token.COMMA {
                p.current++
            }
        }
        if p.tokens[p.current].Token != token.BRACKET_RIGHT {
            prior = p.current
            return nil, p.parseFailure(&p.tokens[p.current], "expected ']'")
        }
        p.current += 1
        return &innerSegment{kind: segmentLongHand, dotName: "", selectors: selectors}, nil
    }
    return nil, p.parseFailure(&firstToken, "unexpected token when parsing inner segment")
}

func (p *JSONPath) parseSelector() (retSelector *selector, err error) {
    //selector            = name-selector /
    //                      wildcard-selector /
    //                      slice-selector /
    //                      index-selector /
    //                      filter-selector
    initial := p.current
    defer func() {
        if p.mode[len(p.mode)-1] == modeSingular && retSelector != nil {
            if retSelector.kind == selectorSubKindWildcard {
                err = p.parseFailure(&p.tokens[initial], "unexpected wildcard in singular query")
                retSelector = nil
            } else if retSelector.kind == selectorSubKindArraySlice {
                err = p.parseFailure(&p.tokens[initial], "unexpected slice in singular query")
                retSelector = nil
            }
        }
    }()

    //    name-selector       = string-literal
    if p.tokens[p.current].Token == token.STRING_LITERAL {
        name := p.tokens[p.current].Literal
        p.current++
        return &selector{kind: selectorSubKindName, name: name}, nil
        //    wildcard-selector   = "*"
    } else if p.tokens[p.current].Token == token.WILDCARD {
        p.current++
        return &selector{kind: selectorSubKindWildcard}, nil
    } else if p.tokens[p.current].Token == token.INTEGER {
        // peek ahead to see if it's a slice
        if p.peek(token.ARRAY_SLICE) {
            slice, err := p.parseSliceSelector()
            if err != nil {
                return nil, err
            }
            return &selector{kind: selectorSubKindArraySlice, slice: slice}, nil
        }
        // peek ahead to see if we close the array index properly
        if !p.peek(token.BRACKET_RIGHT) && !p.peek(token.COMMA) {
            return nil, p.parseFailure(&p.tokens[p.current], "expected ']' or ','")
        }
        // else it's an index
        lit := p.tokens[p.current].Literal
        // make sure it's not -0
        if lit == "-0" {
            return nil, p.parseFailure(&p.tokens[p.current], "-0 unexpected")
        }
        // make sure lit is an integer
        i, err := strconv.ParseInt(lit, 10, 64)
        if err != nil {
            return nil, p.parseFailure(&p.tokens[p.current], "expected an integer")
        }
        err = p.checkSafeInteger(i, lit)
        if err != nil {
            return nil, err
        }

        p.current++

        return &selector{kind: selectorSubKindArrayIndex, index: i}, nil
    } else if p.tokens[p.current].Token == token.ARRAY_SLICE {
        slice, err := p.parseSliceSelector()
        if err != nil {
            return nil, err
        }
        return &selector{kind: selectorSubKindArraySlice, slice: slice}, nil
    } else if p.tokens[p.current].Token == token.FILTER {
        return p.parseFilterSelector()
    }

    return nil, p.parseFailure(&p.tokens[p.current], "unexpected token when parsing selector")
}

func (p *JSONPath) parseSliceSelector() (*slice, error) {
    // slice-selector = [start S] ":" S [end S] [":" [S step]]
    var start, end, step *int64

    // parse the start index
    if p.tokens[p.current].Token == token.INTEGER {
        literal := p.tokens[p.current].Literal
        i, err := strconv.ParseInt(literal, 10, 64)
        if err != nil {
            return nil, p.parseFailure(&p.tokens[p.current], "expected an integer")
        }
        err = p.checkSafeInteger(i, literal)
        if err != nil {
            return nil, err
        }

        start = &i
        p.current += 1
    }

    // Expect a colon
    if p.tokens[p.current].Token != token.ARRAY_SLICE {
        return nil, p.parseFailure(&p.tokens[p.current], "expected ':'")
    }
    p.current++

    // parse the end index
    if p.tokens[p.current].Token == token.INTEGER {
        literal := p.tokens[p.current].Literal
        i, err := strconv.ParseInt(literal, 10, 64)
        if err != nil {
            return nil, p.parseFailure(&p.tokens[p.current], "expected an integer")
        }
        err = p.checkSafeInteger(i, literal)
        if err != nil {
            return nil, err
        }

        end = &i
        p.current++
    }

    // Check for an optional second colon and step value
    if p.tokens[p.current].Token == token.ARRAY_SLICE {
        p.current++
        if p.tokens[p.current].Token == token.INTEGER {
            literal := p.tokens[p.current].Literal
            i, err := strconv.ParseInt(literal, 10, 64)
            if err != nil {
                return nil, p.parseFailure(&p.tokens[p.current], "expected an integer")
            }
            err = p.checkSafeInteger(i, literal)
            if err != nil {
                return nil, err
            }

            step = &i
            p.current++
        }
    }
    if p.tokens[p.current].Token != token.BRACKET_RIGHT {
        return nil, p.parseFailure(&p.tokens[p.current], "expected ']'")
    }

    return &slice{start: start, end: end, step: step}, nil
}

func (p *JSONPath) checkSafeInteger(i int64, literal string) error {
    if i > MaxSafeFloat || i < -MaxSafeFloat {
        return p.parseFailure(&p.tokens[p.current], "outside bounds for safe integers")
    }
    if literal == "-0" {
        return p.parseFailure(&p.tokens[p.current], "-0 unexpected")
    }
    return nil
}

func (p *JSONPath) parseFilterSelector() (*selector, error) {

    if p.tokens[p.current].Token != token.FILTER {
        return nil, p.parseFailure(&p.tokens[p.current], "expected '?'")
    }
    p.current++

    expr, err := p.parseLogicalOrExpr()
    if err != nil {
        return nil, err
    }

    return &selector{kind: selectorSubKindFilter, filter: &filterSelector{expr}}, nil
}

func (p *JSONPath) parseLogicalOrExpr() (*logicalOrExpr, error) {
    var expr logicalOrExpr

    for {
        andExpr, err := p.parseLogicalAndExpr()
        if err != nil {
            return nil, err
        }
        expr.expressions = append(expr.expressions, andExpr)

        if !p.next(token.OR) {
            break
        }
        p.current++
    }

    return &expr, nil
}

func (p *JSONPath) parseLogicalAndExpr() (*logicalAndExpr, error) {
    var expr logicalAndExpr

    for {
        basicExpr, err := p.parseBasicExpr()
        if err != nil {
            return nil, err
        }
        expr.expressions = append(expr.expressions, basicExpr)

        if !p.next(token.AND) {
            break
        }
        p.current++
    }

    return &expr, nil
}

func (p *JSONPath) parseBasicExpr() (*basicExpr, error) {
    //basic-expr          = paren-expr /
    //	                    comparison-expr /
    //                      test-expr

    switch p.tokens[p.current].Token {
    case token.NOT:
        p.current++
        expr, err := p.parseLogicalOrExpr()
        if err != nil {
            return nil, err
        }
        // Inspect if the expr is topped by a parenExpr -- if so we can simplify
        if len(expr.expressions) == 1 && len(expr.expressions[0].expressions) == 1 && expr.expressions[0].expressions[0].parenExpr != nil {
            child := expr.expressions[0].expressions[0].parenExpr
            child.not = !child.not
            return &basicExpr{parenExpr: child}, nil
        }
        return &basicExpr{parenExpr: &parenExpr{not: true, expr: expr}}, nil
    case token.PAREN_LEFT:
        p.current++
        expr, err := p.parseLogicalOrExpr()
        if err != nil {
            return nil, err
        }
        if p.tokens[p.current].Token != token.PAREN_RIGHT {
            return nil, p.parseFailure(&p.tokens[p.current], "expected ')'")
        }
        p.current++
        return &basicExpr{parenExpr: &parenExpr{not: false, expr: expr}}, nil
    }
    prevCurrent := p.current
    comparisonExpr, comparisonErr := p.parseComparisonExpr()
    if comparisonErr == nil {
        return &basicExpr{comparisonExpr: comparisonExpr}, nil
    }
    p.current = prevCurrent
    testExpr, testErr := p.parseTestExpr()
    if testErr == nil {
        return &basicExpr{testExpr: testExpr}, nil
    }
    p.current = prevCurrent
    return nil, p.parseFailure(&p.tokens[p.current], fmt.Sprintf("could not parse query: expected either testExpr [err: %s] or comparisonExpr: [err: %s]", testErr.Error(), comparisonErr.Error()))
}

func (p *JSONPath) parseComparisonExpr() (*comparisonExpr, error) {
    left, err := p.parseComparable()
    if err != nil {
        return nil, err
    }

    if !p.isComparisonOperator(p.tokens[p.current].Token) {
        return nil, p.parseFailure(&p.tokens[p.current], "expected comparison operator")
    }
    operator := p.tokens[p.current].Token
    var op comparisonOperator
    switch operator {
    case token.EQ:
        op = equalTo
    case token.NE:
        op = notEqualTo
    case token.LT:
        op = lessThan
    case token.LE:
        op = lessThanEqualTo
    case token.GT:
        op = greaterThan
    case token.GE:
        op = greaterThanEqualTo
    default:
        return nil, p.parseFailure(&p.tokens[p.current], "expected comparison operator")
    }
    p.current++

    right, err := p.parseComparable()
    if err != nil {
        return nil, err
    }

    return &comparisonExpr{left: left, op: op, right: right}, nil
}

func (p *JSONPath) parseComparable() (*comparable, error) {
    //	comparable = literal /
    //	singular-query / ; singular query value
    //	function-expr    ; ValueType
    //	context-variable ; JSONPath Plus extension
    if literal, err := p.parseLiteral(); err == nil {
        return &comparable{literal: literal}, nil
    }
    if funcExpr, err := p.parseFunctionExpr(); err == nil {
        if funcExpr.funcType == functionTypeMatch {
            return nil, p.parseFailure(&p.tokens[p.current], "match result cannot be compared")
        } else if funcExpr.funcType == functionTypeSearch {
            return nil, p.parseFailure(&p.tokens[p.current], "search result cannot be compared")
        }
        return &comparable{functionExpr: funcExpr}, nil
    }
    switch p.tokens[p.current].Token {
    case token.ROOT:
        p.current++
        query, err := p.parseSingleQuery()
        if err != nil {
            return nil, err
        }
        return &comparable{singularQuery: &singularQuery{absQuery: &absQuery{segments: query.segments}}}, nil
    case token.CURRENT:
        p.current++
        query, err := p.parseSingleQuery()
        if err != nil {
            return nil, err
        }
        return &comparable{singularQuery: &singularQuery{relQuery: &relQuery{segments: query.segments}}}, nil

    case token.CONTEXT_ROOT:
        // @root followed by a path - parse as a query starting from root
        p.current++
        query, err := p.parseSingleQuery()
        if err != nil {
            return nil, err
        }
        return &comparable{singularQuery: &singularQuery{absQuery: &absQuery{segments: query.segments}}}, nil

    default:
        // Check for JSONPath Plus context variables
        if varKind, ok := contextVarTokenMap[p.tokens[p.current].Token]; ok {
            p.current++
            return &comparable{contextVar: &contextVariable{kind: varKind}}, nil
        }
        return nil, p.parseFailure(&p.tokens[p.current], "expected literal or query")
    }
}

func (p *JSONPath) parseQuery() (*jsonPathAST, error) {
    var query jsonPathAST
    p.mode = append(p.mode, modeNormal)

    for p.current < len(p.tokens) {
        prior := p.current
        segment, err := p.parseSegment()
        if err != nil {
            p.current = prior
            break
        }
        query.segments = append(query.segments, segment)
    }
    p.mode = p.mode[:len(p.mode)-1]
    return &query, nil
}

func (p *JSONPath) parseTestExpr() (*testExpr, error) {
    //test-expr           = [logical-not-op S]
    //                  (filter-query / ; existence/non-existence
    //                   function-expr) ; LogicalType or NodesType
    //filter-query        = rel-query / jsonpath-query
    //rel-query           = current-node-identifier segments
    //current-node-identifier = "@"
    not := false
    if p.tokens[p.current].Token == token.NOT {
        not = true
        p.current++
    }
    switch p.tokens[p.current].Token {
    case token.CURRENT:
        p.current++
        query, err := p.parseQuery()
        if err != nil {
            return nil, err
        }
        return &testExpr{filterQuery: &filterQuery{relQuery: &relQuery{segments: query.segments}}, not: not}, nil
    case token.ROOT:
        p.current++
        query, err := p.parseQuery()
        if err != nil {
            return nil, err
        }
        return &testExpr{filterQuery: &filterQuery{jsonPathQuery: &jsonPathAST{segments: query.segments}}, not: not}, nil
    default:
        funcExpr, err := p.parseFunctionExpr()
        if err != nil {
            return nil, err
        }
        if funcExpr.funcType == functionTypeCount {
            return nil, p.parseFailure(&p.tokens[p.current], "count function must be compared")
        }
        if funcExpr.funcType == functionTypeLength {
            return nil, p.parseFailure(&p.tokens[p.current], "length function must be compared")
        }
        if funcExpr.funcType == functionTypeValue {
            return nil, p.parseFailure(&p.tokens[p.current], "length function must be compared")
        }
        return &testExpr{functionExpr: funcExpr, not: not}, nil
    }

    return nil, p.parseFailure(&p.tokens[p.current], "unexpected token when parsing test expression")
}

func (p *JSONPath) parseFunctionExpr() (*functionExpr, error) {
    // RFC 9535: function name must be immediately followed by '(' (no whitespace)
    // The tokenizer only emits FUNCTION token when function name is directly followed by '('
    if p.tokens[p.current].Token != token.FUNCTION {
        return nil, p.parseFailure(&p.tokens[p.current], "expected function")
    }
    functionName := p.tokens[p.current].Literal
    if p.current+1 >= len(p.tokens) || p.tokens[p.current+1].Token != token.PAREN_LEFT {
        return nil, p.parseFailure(&p.tokens[p.current], "expected '(' after function")
    }
    p.current += 2
    args := []*functionArgument{}

    // Check type selector functions first (JSONPath Plus)
    // These take a single argument and return boolean
    if funcType, ok := typeSelectorFunctionMap[functionName]; ok {
        arg, err := p.parseFunctionArgument(false)
        if err != nil {
            return nil, err
        }
        args = append(args, arg)
        if p.tokens[p.current].Token != token.PAREN_RIGHT {
            return nil, p.parseFailure(&p.tokens[p.current], "expected ')'")
        }
        p.current++
        return &functionExpr{funcType: funcType, args: args}, nil
    }

    switch functionTypeMap[functionName] {
    case functionTypeLength:
        arg, err := p.parseFunctionArgument(true)
        if err != nil {
            return nil, err
        }
        args = append(args, arg)
    case functionTypeCount:
        arg, err := p.parseFunctionArgument(false)
        if err != nil {
            return nil, err
        }
        if arg.literal != nil && arg.literal.node == nil {
            return nil, p.parseFailure(&p.tokens[p.current], "count function only supports containers")
        }
        args = append(args, arg)
    case functionTypeValue:
        arg, err := p.parseFunctionArgument(false)
        if err != nil {
            return nil, err
        }
        args = append(args, arg)
    case functionTypeMatch:
        fallthrough
    case functionTypeSearch:
        arg, err := p.parseFunctionArgument(false)
        if err != nil {
            return nil, err
        }
        args = append(args, arg)
        if p.tokens[p.current].Token != token.COMMA {
            return nil, p.parseFailure(&p.tokens[p.current], "expected ','")
        }
        p.current++
        arg, err = p.parseFunctionArgument(false)
        if err != nil {
            return nil, err
        }
        args = append(args, arg)
    default:
        return nil, p.parseFailure(&p.tokens[p.current], "unknown function: "+functionName)
    }
    if p.tokens[p.current].Token != token.PAREN_RIGHT {
        return nil, p.parseFailure(&p.tokens[p.current], "expected ')'")
    }
    p.current++
    return &functionExpr{funcType: functionTypeMap[functionName], args: args}, nil
}

func (p *JSONPath) parseSingleQuery() (*jsonPathAST, error) {
    var query jsonPathAST
    for p.current < len(p.tokens) {
        try := p.current
        p.mode = append(p.mode, modeSingular)
        segment, err := p.parseSegment()
        if err != nil {
            // rollback
            p.mode = p.mode[:len(p.mode)-1]
            p.current = try
            break
        }
        p.mode = p.mode[:len(p.mode)-1]
        query.segments = append(query.segments, segment)
    }
    //if len(query.segments) == 0 {
    //	return nil, p.parseFailure(p.tokens[p.current], "expected at least one segment")
    //}
    return &query, nil
}

func (p *JSONPath) parseFunctionArgument(single bool) (*functionArgument, error) {
    //function-argument   = literal /
    //	filter-query / ; (includes singular-query)
    //  logical-expr /
    //	function-expr

    if lit, err := p.parseLiteral(); err == nil {
        return &functionArgument{literal: lit}, nil
    }
    switch p.tokens[p.current].Token {
    case token.CURRENT:
        p.current++
        var query *jsonPathAST
        var err error
        if single {
            query, err = p.parseSingleQuery()
        } else {
            query, err = p.parseQuery()
        }
        if err != nil {
            return nil, err
        }
        return &functionArgument{filterQuery: &filterQuery{relQuery: &relQuery{segments: query.segments}}}, nil
    case token.ROOT:
        p.current++
        var query *jsonPathAST
        var err error
        if single {
            query, err = p.parseSingleQuery()
        } else {
            query, err = p.parseQuery()
        }
        if err != nil {
            return nil, err
        }
        return &functionArgument{filterQuery: &filterQuery{jsonPathQuery: &jsonPathAST{segments: query.segments}}}, nil
    }

    // Check for JSONPath Plus context variables as function arguments
    if varKind, ok := contextVarTokenMap[p.tokens[p.current].Token]; ok {
        p.current++
        return &functionArgument{contextVar: &contextVariable{kind: varKind}}, nil
    }

    if expr, err := p.parseLogicalOrExpr(); err == nil {
        return &functionArgument{logicalExpr: expr}, nil
    }
    if funcExpr, err := p.parseFunctionExpr(); err == nil {
        return &functionArgument{functionExpr: funcExpr}, nil
    }

    return nil, p.parseFailure(&p.tokens[p.current], "unexpected token for function argument")
}

func (p *JSONPath) parseLiteral() (*literal, error) {
    switch p.tokens[p.current].Token {
    case token.STRING_LITERAL:
        lit := p.tokens[p.current].Literal
        p.current++
        return &literal{string: &lit}, nil
    case token.INTEGER:
        lit := p.tokens[p.current].Literal
        p.current++
        i, err := strconv.Atoi(lit)
        if err != nil {
            return nil, p.parseFailure(&p.tokens[p.current], "expected integer")
        }
        return &literal{integer: &i}, nil
    case token.FLOAT:
        lit := p.tokens[p.current].Literal
        p.current++
        f, err := strconv.ParseFloat(lit, 64)
        if err != nil {
            return nil, p.parseFailure(&p.tokens[p.current], "expected float")
        }
        return &literal{float64: &f}, nil
    case token.TRUE:
        p.current++
        res := true
        return &literal{bool: &res}, nil
    case token.FALSE:
        p.current++
        res := false
        return &literal{bool: &res}, nil
    case token.NULL:
        p.current++
        res := true
        return &literal{null: &res}, nil
    }
    return nil, p.parseFailure(&p.tokens[p.current], "expected literal")
}

type jsonPathAST struct {
    // "$"
    segments []*segment
}

func (q jsonPathAST) ToString() string {
    b := strings.Builder{}
    b.WriteString("$")
    for _, seg := range q.segments {
        b.WriteString(seg.ToString())
    }
    return b.String()
}
