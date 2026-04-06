package jsonpath

import (
    "go.yaml.in/yaml/v4"
    "strconv"
    "strings"
)

// filter-selector     = "?" S logical-expr
type filterSelector struct {
    // logical-expr        = logical-or-expr
    expression *logicalOrExpr
}

func (s filterSelector) ToString() string {
    return s.expression.ToString()
}

// logical-or-expr     = logical-and-expr *(S "||" S logical-and-expr)
type logicalOrExpr struct {
    expressions []*logicalAndExpr
}

func (e logicalOrExpr) ToString() string {
    builder := strings.Builder{}
    for i, expr := range e.expressions {
        if i > 0 {
            builder.WriteString(" || ")
        }
        builder.WriteString(expr.ToString())
    }
    return builder.String()
}

// logical-and-expr    = basic-expr *(S "&&" S basic-expr)
type logicalAndExpr struct {
    expressions []*basicExpr
}

func (e logicalAndExpr) ToString() string {
    builder := strings.Builder{}
    for i, expr := range e.expressions {
        if i > 0 {
            builder.WriteString(" && ")
        }
        builder.WriteString(expr.ToString())
    }
    return builder.String()
}

// relQuery rel-query = current-node-identifier segments
// current-node-identifier = "@"
type relQuery struct {
    segments []*segment
}

func (q relQuery) ToString() string {
    builder := strings.Builder{}
    builder.WriteString("@")
    for _, segment := range q.segments {
        builder.WriteString(segment.ToString())
    }
    return builder.String()
}

// filterQuery filter-query        = rel-query / jsonpath-query
type filterQuery struct {
    relQuery      *relQuery
    jsonPathQuery *jsonPathAST
}

func (q filterQuery) ToString() string {
    if q.relQuery != nil {
        return q.relQuery.ToString()
    } else if q.jsonPathQuery != nil {
        return q.jsonPathQuery.ToString()
    }
    return ""
}

// functionArgument function-argument   = literal /
//
//	filter-query / ; (includes singular-query)
//	logical-expr /
//	function-expr
type functionArgument struct {
    literal      *literal
    filterQuery  *filterQuery
    logicalExpr  *logicalOrExpr
    functionExpr *functionExpr
    contextVar   *contextVariable // JSONPath Plus context variables
}

type functionArgType int

const (
    functionArgTypeLiteral functionArgType = iota
    functionArgTypeNodes
)

type resolvedArgument struct {
    kind    functionArgType
    literal *literal
    nodes   []*literal
}

func (a functionArgument) Eval(idx index, node *yaml.Node, root *yaml.Node) resolvedArgument {
    if a.literal != nil {
        return resolvedArgument{kind: functionArgTypeLiteral, literal: a.literal}
    } else if a.filterQuery != nil {
        result := a.filterQuery.Query(idx, node, root)
        lits := make([]*literal, len(result))
        for i, node := range result {
            lit := nodeToLiteral(node)
            lits[i] = &lit
        }
        if len(result) != 1 {
            return resolvedArgument{kind: functionArgTypeNodes, nodes: lits}
        } else {
            return resolvedArgument{kind: functionArgTypeLiteral, literal: lits[0]}
        }
    } else if a.logicalExpr != nil {
        res := a.logicalExpr.Matches(idx, node, root)
        return resolvedArgument{kind: functionArgTypeLiteral, literal: &literal{bool: &res}}
    } else if a.functionExpr != nil {
        res := a.functionExpr.Evaluate(idx, node, root)
        return resolvedArgument{kind: functionArgTypeLiteral, literal: &res}
    } else if a.contextVar != nil {
        // Evaluate context variable and return as literal
        res := a.contextVar.Evaluate(idx, node, root)
        return resolvedArgument{kind: functionArgTypeLiteral, literal: &res}
    }
    return resolvedArgument{}
}

func (a functionArgument) ToString() string {
    builder := strings.Builder{}
    if a.literal != nil {
        builder.WriteString(a.literal.ToString())
    } else if a.filterQuery != nil {
        builder.WriteString(a.filterQuery.ToString())
    } else if a.logicalExpr != nil {
        builder.WriteString(a.logicalExpr.ToString())
    } else if a.functionExpr != nil {
        builder.WriteString(a.functionExpr.ToString())
    } else if a.contextVar != nil {
        builder.WriteString(a.contextVar.ToString())
    }
    return builder.String()
}

//function-name       = function-name-first *function-name-char
//function-name-first = LCALPHA
//function-name-char  = function-name-first / "_" / DIGIT
//LCALPHA             = %x61-7A  ; "a".."z"
//

type functionType int

const (
    functionTypeLength functionType = iota
    functionTypeCount
    functionTypeMatch
    functionTypeSearch
    functionTypeValue
    // JSONPath Plus type selector functions
    functionTypeIsNull
    functionTypeIsBoolean
    functionTypeIsNumber
    functionTypeIsString
    functionTypeIsArray
    functionTypeIsObject
    functionTypeIsInteger
)

var functionTypeMap = map[string]functionType{
    "length": functionTypeLength,
    "count":  functionTypeCount,
    "match":  functionTypeMatch,
    "search": functionTypeSearch,
    "value":  functionTypeValue,
}

// typeSelectorFunctionMap maps JSONPath Plus type selector function names to their types.
// These are extensions enabled when JSONPath Plus mode is active.
var typeSelectorFunctionMap = map[string]functionType{
    "isNull":    functionTypeIsNull,
    "isBoolean": functionTypeIsBoolean,
    "isNumber":  functionTypeIsNumber,
    "isString":  functionTypeIsString,
    "isArray":   functionTypeIsArray,
    "isObject":  functionTypeIsObject,
    "isInteger": functionTypeIsInteger,
}

func (f functionType) String() string {
    for k, v := range functionTypeMap {
        if v == f {
            return k
        }
    }
    for k, v := range typeSelectorFunctionMap {
        if v == f {
            return k
        }
    }
    return "unknown"
}

// functionExpr function-expr       = function-name "(" S [function-argument
// *(S "," S function-argument)] S ")"
type functionExpr struct {
    funcType functionType
    args     []*functionArgument
}

func (e functionExpr) ToString() string {
    builder := strings.Builder{}
    builder.WriteString(e.funcType.String())
    builder.WriteString("(")
    for i, arg := range e.args {
        if i > 0 {
            builder.WriteString(", ")
        }
        builder.WriteString(arg.ToString())
    }
    builder.WriteString(")")
    return builder.String()
}

// testExpr test-expr           = [logical-not-op S]
//
//	(filter-query / ; existence/non-existence
//	 function-expr) ; LogicalType or NodesType
type testExpr struct {
    not          bool
    filterQuery  *filterQuery
    functionExpr *functionExpr
}

func (e testExpr) ToString() string {
    builder := strings.Builder{}
    if e.not {
        builder.WriteString("!")
    }
    if e.filterQuery != nil {
        builder.WriteString(e.filterQuery.ToString())
    } else if e.functionExpr != nil {
        builder.WriteString(e.functionExpr.ToString())
    }
    return builder.String()
}

// basicExpr basic-expr          =
//
//	 paren-expr /
//		comparison-expr /
//		test-expr
type basicExpr struct {
    parenExpr      *parenExpr
    comparisonExpr *comparisonExpr
    testExpr       *testExpr
}

func (e basicExpr) ToString() string {
    if e.parenExpr != nil {
        return e.parenExpr.ToString()
    } else if e.comparisonExpr != nil {
        return e.comparisonExpr.ToString()
    } else if e.testExpr != nil {
        return e.testExpr.ToString()
    }
    return ""
}

// literal literal = number /
// . string-literal /
// . true / false / null
type literal struct {
    // we generally decompose these into their component parts for easier evaluation
    integer *int
    float64 *float64
    string  *string
    bool    *bool
    null    *bool
    node    *yaml.Node
}

func (l literal) ToString() string {
    if l.integer != nil {
        return strconv.Itoa(*l.integer)
    } else if l.float64 != nil {
        return strconv.FormatFloat(*l.float64, 'f', -1, 64)
    } else if l.string != nil {
        builder := strings.Builder{}
        builder.WriteString("'")
        builder.WriteString(escapeString(*l.string))
        builder.WriteString("'")
        return builder.String()
    } else if l.bool != nil {
        if *l.bool {
            return "true"
        } else {
            return "false"
        }
    } else if l.null != nil {
        if *l.null {
            return "null"
        } else {
            return "null"
        }
    } else if l.node != nil {
        switch l.node.Kind {
        case yaml.ScalarNode:
            return l.node.Value
        case yaml.SequenceNode:
            builder := strings.Builder{}
            builder.WriteString("[")
            for i, child := range l.node.Content {
                if i > 0 {
                    builder.WriteString(",")
                }
                builder.WriteString(literal{node: child}.ToString())
            }
            builder.WriteString("]")
            return builder.String()
        case yaml.MappingNode:
            builder := strings.Builder{}
            builder.WriteString("{")
            for i, child := range l.node.Content {
                if i > 0 {
                    builder.WriteString(",")
                }
                builder.WriteString(literal{node: child}.ToString())
            }
            builder.WriteString("}")
            return builder.String()
        }
    }
    return ""
}

func escapeString(value string) string {
    b := strings.Builder{}
    for i := 0; i < len(value); i++ {
        if value[i] == '\n' {
            b.WriteString("\\\\n")
        } else if value[i] == '\\' {
            b.WriteString("\\\\")
        } else if value[i] == '\'' {
            b.WriteString("\\'")
        } else {
            b.WriteByte(value[i])
        }
    }
    return b.String()
}

type absQuery jsonPathAST

func (q absQuery) ToString() string {
    builder := strings.Builder{}
    builder.WriteString("$")
    for _, segment := range q.segments {
        builder.WriteString(segment.ToString())
    }
    return builder.String()
}

// singularQuery singular-query = rel-singular-query / abs-singular-query
type singularQuery struct {
    relQuery *relQuery
    absQuery *absQuery
}

func (q singularQuery) ToString() string {
    if q.relQuery != nil {
        return q.relQuery.ToString()
    } else if q.absQuery != nil {
        return q.absQuery.ToString()
    }
    return ""
}

// contextVarKind represents the type of context variable
type contextVarKind int

const (
    contextVarProperty       contextVarKind = iota // @property - current property name
    contextVarRoot                                 // @root - root node access
    contextVarParent                               // @parent - parent node
    contextVarParentProperty                       // @parentProperty - parent's property name
    contextVarPath                                 // @path - absolute path to current node
    contextVarIndex                                // @index - current array index
)

// contextVariable represents a JSONPath Plus context variable in filter expressions.
// These provide access to metadata about the current node being evaluated.
type contextVariable struct {
    kind contextVarKind
}

func (cv contextVariable) ToString() string {
    switch cv.kind {
    case contextVarProperty:
        return "@property"
    case contextVarRoot:
        return "@root"
    case contextVarParent:
        return "@parent"
    case contextVarParentProperty:
        return "@parentProperty"
    case contextVarPath:
        return "@path"
    case contextVarIndex:
        return "@index"
    default:
        return "@unknown"
    }
}

// comparable
//
//	comparable = literal /
//	singular-query / ; singular query value
//	function-expr    ; ValueType
//	context-variable ; JSONPath Plus extension
type comparable struct {
    literal       *literal
    singularQuery *singularQuery
    functionExpr  *functionExpr
    contextVar    *contextVariable // JSONPath Plus extension
}

func (c comparable) ToString() string {
    if c.literal != nil {
        return c.literal.ToString()
    } else if c.singularQuery != nil {
        return c.singularQuery.ToString()
    } else if c.functionExpr != nil {
        return c.functionExpr.ToString()
    } else if c.contextVar != nil {
        return c.contextVar.ToString()
    }
    return ""
}

// comparisonExpr represents a comparison expression
//
//	comparison-expr     = comparable S comparison-op S comparable
//	literal             = number / string-literal /
//	                      true / false / null
//	comparable          = literal /
//	                      singular-query / ; singular query value
//	                      function-expr    ; ValueType
//	comparison-op       = "==" / "!=" /
//	                      "<=" / ">=" /
//	                      "<"  / ">"
type comparisonExpr struct {
    left  *comparable
    op    comparisonOperator
    right *comparable
}

func (e comparisonExpr) ToString() string {
    builder := strings.Builder{}
    builder.WriteString(e.left.ToString())
    builder.WriteString(" ")
    builder.WriteString(e.op.ToString())
    builder.WriteString(" ")
    builder.WriteString(e.right.ToString())
    return builder.String()
}

// existExpr represents an existence expression
type existExpr struct {
    query string
}

// parenExpr represents a parenthesized expression
//
//	paren-expr          = [logical-not-op S] "(" S logical-expr S ")"
type parenExpr struct {
    // "!"
    not bool
    // "(" logicalOrExpr ")"
    expr *logicalOrExpr
}

func (e parenExpr) ToString() string {
    builder := strings.Builder{}
    if e.not {
        builder.WriteString("!")
    }
    builder.WriteString("(")
    builder.WriteString(e.expr.ToString())
    builder.WriteString(")")
    return builder.String()
}

// comparisonOperator represents a comparison operator
type comparisonOperator int

const (
    equalTo comparisonOperator = iota
    notEqualTo
    lessThan
    lessThanEqualTo
    greaterThan
    greaterThanEqualTo
)

func (o comparisonOperator) ToString() string {
    switch o {
    case equalTo:
        return "=="
    case notEqualTo:
        return "!="
    case lessThan:
        return "<"
    case lessThanEqualTo:
        return "<="
    case greaterThan:
        return ">"
    case greaterThanEqualTo:
        return ">="
    }
    return ""
}
