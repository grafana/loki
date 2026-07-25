package jsonpath

import (
	"math"
	"strconv"
	"unicode/utf16"

	"github.com/pb33f/jsonpath/pkg/jsonpath/token"
	"go.yaml.in/yaml/v4"
)

type spectralRuntimeKind uint8

const (
	spectralMissing spectralRuntimeKind = iota
	spectralInvalid
	spectralNull
	spectralBoolean
	spectralNumber
	spectralString
	spectralNode
	spectralRegexValue
)

type spectralRuntimeValue struct {
	kind                   spectralRuntimeKind
	boolean                bool
	number                 float64
	string                 string
	node                   *yaml.Node
	regex                  *spectralRegex
	propertyMethodCoercion bool
}

type spectralEvalContext struct {
	index   index
	current *yaml.Node
	root    *yaml.Node
}

func (e *spectralBoolExpr) Matches(idx index, node *yaml.Node, root *yaml.Node) bool {
	result, valid := e.evaluate(spectralEvalContext{index: idx, current: node, root: unwrapDocument(root)})
	return valid && result
}

func (e *spectralBoolExpr) evaluate(ctx spectralEvalContext) (bool, bool) {
	if e == nil {
		return false, false
	}
	switch e.kind {
	case spectralBoolValue:
		value := e.value.evaluate(ctx)
		return value.truthy(), value.kind != spectralInvalid
	case spectralBoolNot:
		result, valid := e.left.evaluate(ctx)
		return !result, valid
	case spectralBoolAnd:
		left, valid := e.left.evaluate(ctx)
		if !valid || !left {
			return left, valid
		}
		return e.right.evaluate(ctx)
	case spectralBoolOr:
		left, valid := e.left.evaluate(ctx)
		if !valid || left {
			return left, valid
		}
		return e.right.evaluate(ctx)
	case spectralBoolCompare:
		left := e.left.value.evaluate(ctx)
		right := e.right.value.evaluate(ctx)
		if left.kind == spectralInvalid || right.kind == spectralInvalid {
			return false, false
		}
		return spectralCompare(left, right, e.op), true
	case spectralBoolParen:
		return e.left.evaluate(ctx)
	default:
		return false, false
	}
}

func (e *spectralValueExpr) evaluate(ctx spectralEvalContext) spectralRuntimeValue {
	if e == nil {
		return spectralRuntimeValue{}
	}
	var value spectralRuntimeValue
	switch e.kind {
	case spectralValueLiteral:
		value = spectralRuntimeFromLiteral(e.literal)
	case spectralValueCurrent:
		value = spectralRuntimeValue{kind: spectralNode, node: ctx.current}
	case spectralValueRoot:
		value = spectralRuntimeValue{kind: spectralNode, node: ctx.root}
	case spectralValueContext:
		value = spectralContextValue(e.context, ctx)
	case spectralValueRegex:
		value = spectralRuntimeValue{kind: spectralRegexValue, regex: e.regex}
	case spectralValueUndefined:
		value = spectralRuntimeValue{}
	case spectralValueFunction:
		value = spectralRuntimeFromLiteral(e.function.Evaluate(ctx.index, ctx.current, ctx.root))
	}

	if len(e.segments) > 0 {
		if value.kind != spectralNode || value.node == nil {
			return spectralRuntimeValue{}
		}
		resolved := value.node
		for _, segment := range e.segments {
			resolved = spectralResolveSegment(resolved, segment)
			if resolved == nil {
				return spectralRuntimeValue{}
			}
		}
		value = spectralRuntimeFromNode(resolved)
	} else if value.kind == spectralNode {
		value = spectralRuntimeFromNode(value.node)
	}

	for _, postfix := range e.postfix {
		value = spectralApplyPostfix(value, postfix)
		if value.kind == spectralMissing {
			break
		}
	}
	return value
}

func spectralRuntimeFromLiteral(value literal) spectralRuntimeValue {
	switch {
	case value.integer != nil:
		return spectralRuntimeValue{kind: spectralNumber, number: float64(*value.integer)}
	case value.float64 != nil:
		return spectralRuntimeValue{kind: spectralNumber, number: *value.float64}
	case value.string != nil:
		return spectralRuntimeValue{kind: spectralString, string: *value.string}
	case value.bool != nil:
		return spectralRuntimeValue{kind: spectralBoolean, boolean: *value.bool}
	case value.null != nil:
		return spectralRuntimeValue{kind: spectralNull}
	case value.node != nil:
		return spectralRuntimeFromNode(value.node)
	default:
		return spectralRuntimeValue{}
	}
}

func spectralRuntimeFromNode(node *yaml.Node) spectralRuntimeValue {
	node = unwrapDocument(node)
	if node == nil {
		return spectralRuntimeValue{}
	}
	if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode {
		return spectralRuntimeValue{kind: spectralNode, node: node}
	}
	switch node.Tag {
	case "!!null":
		return spectralRuntimeValue{kind: spectralNull}
	case "!!bool":
		value, err := strconv.ParseBool(node.Value)
		if err != nil {
			return spectralRuntimeValue{}
		}
		return spectralRuntimeValue{kind: spectralBoolean, boolean: value}
	case "!!int":
		value, err := strconv.ParseFloat(node.Value, 64)
		if err != nil {
			return spectralRuntimeValue{}
		}
		return spectralRuntimeValue{kind: spectralNumber, number: value}
	case "!!float":
		value, err := strconv.ParseFloat(node.Value, 64)
		if err != nil {
			return spectralRuntimeValue{}
		}
		return spectralRuntimeValue{kind: spectralNumber, number: value}
	case "!!str":
		return spectralRuntimeValue{kind: spectralString, string: node.Value}
	default:
		if node.Kind == yaml.ScalarNode {
			return spectralRuntimeValue{kind: spectralString, string: node.Value}
		}
		return spectralRuntimeValue{kind: spectralNode, node: node}
	}
}

func spectralContextValue(kind contextVarKind, ctx spectralEvalContext) spectralRuntimeValue {
	filterCtx, ok := ctx.index.(FilterContext)
	if !ok {
		return spectralRuntimeValue{}
	}
	switch kind {
	case contextVarProperty:
		if filterCtx.Index() >= 0 {
			return spectralRuntimeValue{kind: spectralNumber, number: float64(filterCtx.Index()), propertyMethodCoercion: true}
		}
		return spectralRuntimeValue{kind: spectralString, string: filterCtx.PropertyName()}
	case contextVarRoot:
		return spectralRuntimeFromNode(ctx.root)
	case contextVarParent:
		return spectralRuntimeFromNode(filterCtx.Parent())
	case contextVarParentProperty:
		parent := filterCtx.Parent()
		if parent != nil {
			if parentContainer := ctx.index.getParentNode(parent); parentContainer != nil && parentContainer.Kind == yaml.SequenceNode {
				if parentIndex, err := strconv.Atoi(filterCtx.ParentPropertyName()); err == nil {
					return spectralRuntimeValue{kind: spectralNumber, number: float64(parentIndex), propertyMethodCoercion: true}
				}
			}
		}
		return spectralRuntimeValue{kind: spectralString, string: filterCtx.ParentPropertyName()}
	case contextVarPath:
		return spectralRuntimeValue{kind: spectralString, string: filterCtx.Path()}
	case contextVarIndex:
		return spectralRuntimeValue{kind: spectralNumber, number: float64(filterCtx.Index())}
	default:
		return spectralRuntimeValue{}
	}
}

func spectralResolveSegment(node *yaml.Node, segment spectralPathSegment) *yaml.Node {
	node = unwrapDocument(node)
	if node == nil {
		return nil
	}
	if segment.index != nil {
		if node.Kind != yaml.SequenceNode {
			return nil
		}
		index := *segment.index
		if index < 0 {
			index += int64(len(node.Content))
		}
		if index < 0 || index >= int64(len(node.Content)) {
			return nil
		}
		return node.Content[index]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == segment.name {
			return node.Content[i+1]
		}
	}
	return nil
}

func spectralApplyPostfix(value spectralRuntimeValue, postfix spectralPostfix) spectralRuntimeValue {
	if value.kind == spectralNumber && value.propertyMethodCoercion &&
		(postfix.kind == spectralPostfixMatch || postfix.kind == spectralPostfixIndexOf || postfix.kind == spectralPostfixIncludes) {
		value = spectralRuntimeValue{kind: spectralString, string: strconv.FormatFloat(value.number, 'f', -1, 64)}
	}
	switch postfix.kind {
	case spectralPostfixMatch:
		if value.kind != spectralString {
			return spectralRuntimeValue{kind: spectralInvalid}
		}
		return spectralRuntimeValue{kind: spectralBoolean, boolean: postfix.regex.compiled.MatchString(value.string)}
	case spectralPostfixIndexOf:
		if value.kind != spectralString {
			return spectralRuntimeValue{kind: spectralInvalid}
		}
		from := 0
		if postfix.fromIndex != nil {
			from = int(math.Trunc(*postfix.fromIndex))
		}
		return spectralRuntimeValue{kind: spectralNumber, number: float64(spectralUTF16IndexOf(value.string, postfix.search, from))}
	case spectralPostfixLength:
		switch value.kind {
		case spectralString:
			return spectralRuntimeValue{kind: spectralNumber, number: float64(len(utf16.Encode([]rune(value.string))))}
		case spectralNode:
			if value.node != nil && value.node.Kind == yaml.SequenceNode {
				return spectralRuntimeValue{kind: spectralNumber, number: float64(len(value.node.Content))}
			}
		}
		return spectralRuntimeValue{kind: spectralInvalid}
	case spectralPostfixConstructorName:
		name := ""
		switch value.kind {
		case spectralString:
			name = "String"
		case spectralNumber:
			name = "Number"
		case spectralBoolean:
			name = "Boolean"
		case spectralNode:
			if value.node != nil && value.node.Kind == yaml.SequenceNode {
				name = "Array"
			} else if value.node != nil && value.node.Kind == yaml.MappingNode {
				name = "Object"
			}
		}
		if name == "" {
			return spectralRuntimeValue{kind: spectralInvalid}
		}
		return spectralRuntimeValue{kind: spectralString, string: name}
	case spectralPostfixIncludes:
		argument := spectralRuntimeFromLiteral(postfix.argument)
		from := 0
		if postfix.fromIndex != nil {
			from = int(math.Trunc(*postfix.fromIndex))
		}
		switch value.kind {
		case spectralString:
			if argument.kind != spectralString {
				return spectralRuntimeValue{kind: spectralInvalid}
			}
			if from < 0 {
				from = 0
			}
			return spectralRuntimeValue{kind: spectralBoolean, boolean: spectralUTF16IndexOf(value.string, argument.string, from) >= 0}
		case spectralNode:
			if value.node == nil || value.node.Kind != yaml.SequenceNode {
				return spectralRuntimeValue{kind: spectralInvalid}
			}
			if from < 0 {
				from += len(value.node.Content)
				if from < 0 {
					from = 0
				}
			}
			if from > len(value.node.Content) {
				return spectralRuntimeValue{kind: spectralBoolean, boolean: false}
			}
			for _, item := range value.node.Content[from:] {
				if spectralStrictEqual(spectralRuntimeFromNode(item), argument) {
					return spectralRuntimeValue{kind: spectralBoolean, boolean: true}
				}
			}
			return spectralRuntimeValue{kind: spectralBoolean, boolean: false}
		default:
			return spectralRuntimeValue{kind: spectralInvalid}
		}
	default:
		return spectralRuntimeValue{kind: spectralInvalid}
	}
}

func spectralUTF16IndexOf(value, search string, from int) int {
	valueUnits := utf16.Encode([]rune(value))
	searchUnits := utf16.Encode([]rune(search))
	if from < 0 {
		from = 0
	}
	if from > len(valueUnits) {
		from = len(valueUnits)
	}
	if len(searchUnits) == 0 {
		return from
	}
	for i := from; i+len(searchUnits) <= len(valueUnits); i++ {
		matched := true
		for j := range searchUnits {
			if valueUnits[i+j] != searchUnits[j] {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

func (value spectralRuntimeValue) truthy() bool {
	switch value.kind {
	case spectralMissing, spectralInvalid, spectralNull:
		return false
	case spectralBoolean:
		return value.boolean
	case spectralNumber:
		return value.number != 0 && !math.IsNaN(value.number)
	case spectralString:
		return value.string != ""
	case spectralNode, spectralRegexValue:
		return true
	default:
		return false
	}
}

func spectralCompare(left, right spectralRuntimeValue, operator token.Token) bool {
	switch operator {
	case token.STRICT_EQ:
		return spectralStrictEqual(left, right)
	case token.STRICT_NE:
		return !spectralStrictEqual(left, right)
	case token.EQ:
		return spectralLooseEqual(left, right)
	case token.NE:
		return !spectralLooseEqual(left, right)
	case token.LT, token.LE, token.GT, token.GE:
		comparison, ok := spectralRelationalCompare(left, right)
		if !ok {
			return false
		}
		switch operator {
		case token.LT:
			return comparison < 0
		case token.LE:
			return comparison <= 0
		case token.GT:
			return comparison > 0
		case token.GE:
			return comparison >= 0
		}
	}
	return false
}

func spectralStrictEqual(left, right spectralRuntimeValue) bool {
	if left.kind == spectralInvalid || right.kind == spectralInvalid {
		return false
	}
	if left.kind != right.kind {
		return false
	}
	switch left.kind {
	case spectralMissing, spectralNull:
		return true
	case spectralBoolean:
		return left.boolean == right.boolean
	case spectralNumber:
		return left.number == right.number
	case spectralString:
		return left.string == right.string
	case spectralNode:
		return left.node == right.node
	case spectralRegexValue:
		return left.regex == right.regex
	default:
		return false
	}
}

func spectralLooseEqual(left, right spectralRuntimeValue) bool {
	if spectralStrictEqual(left, right) {
		return true
	}
	if (left.kind == spectralNull && right.kind == spectralMissing) || (left.kind == spectralMissing && right.kind == spectralNull) {
		return true
	}
	if left.kind == spectralBoolean {
		left = spectralRuntimeValue{kind: spectralNumber, number: spectralBooleanNumber(left.boolean)}
	}
	if right.kind == spectralBoolean {
		right = spectralRuntimeValue{kind: spectralNumber, number: spectralBooleanNumber(right.boolean)}
	}
	if spectralStrictEqual(left, right) {
		return true
	}
	if left.kind == spectralString && right.kind == spectralNumber {
		number, ok := spectralStringNumber(left.string)
		return ok && number == right.number
	}
	if left.kind == spectralNumber && right.kind == spectralString {
		number, ok := spectralStringNumber(right.string)
		return ok && left.number == number
	}
	return false
}

func spectralBooleanNumber(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func spectralStringNumber(value string) (float64, bool) {
	if value == "" {
		return 0, true
	}
	number, err := strconv.ParseFloat(value, 64)
	return number, err == nil
}

func spectralRelationalCompare(left, right spectralRuntimeValue) (int, bool) {
	if left.kind == spectralString && right.kind == spectralString {
		switch {
		case left.string < right.string:
			return -1, true
		case left.string > right.string:
			return 1, true
		default:
			return 0, true
		}
	}
	leftNumber, leftOK := spectralComparableNumber(left)
	rightNumber, rightOK := spectralComparableNumber(right)
	if !leftOK || !rightOK || math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
		return 0, false
	}
	switch {
	case leftNumber < rightNumber:
		return -1, true
	case leftNumber > rightNumber:
		return 1, true
	default:
		return 0, true
	}
}

func spectralComparableNumber(value spectralRuntimeValue) (float64, bool) {
	switch value.kind {
	case spectralNumber:
		return value.number, true
	case spectralString:
		return spectralStringNumber(value.string)
	case spectralBoolean:
		return spectralBooleanNumber(value.boolean), true
	case spectralNull:
		return 0, true
	default:
		return 0, false
	}
}

func unwrapDocument(node *yaml.Node) *yaml.Node {
	if node != nil && node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		return node.Content[0]
	}
	return node
}
