package jsonpath

import (
	"strconv"

	"go.yaml.in/yaml/v4"
)

type Evaluator interface {
	Query(current *yaml.Node, root *yaml.Node) []*yaml.Node
}

// index is the basic interface for tracking property key relationships.
// This is kept for backward compatibility; FilterContext extends this.
type index interface {
	setPropertyKey(key *yaml.Node, value *yaml.Node)
	getPropertyKey(key *yaml.Node) *yaml.Node
	setParentNode(child *yaml.Node, parent *yaml.Node)
	getParentNode(child *yaml.Node) *yaml.Node
}

type _index struct {
	propertyKeys map[*yaml.Node]*yaml.Node
	parentNodes  map[*yaml.Node]*yaml.Node // Maps child nodes to their parent nodes
}

func (i *_index) setPropertyKey(key *yaml.Node, value *yaml.Node) {
	if i != nil && i.propertyKeys != nil {
		i.propertyKeys[key] = value
	}
}

func (i *_index) getPropertyKey(key *yaml.Node) *yaml.Node {
	if i != nil {
		return i.propertyKeys[key]
	}
	return nil
}

func (i *_index) setParentNode(child *yaml.Node, parent *yaml.Node) {
	if i != nil && i.parentNodes != nil {
		i.parentNodes[child] = parent
	}
}

func (i *_index) getParentNode(child *yaml.Node) *yaml.Node {
	if i != nil && i.parentNodes != nil {
		return i.parentNodes[child]
	}
	return nil
}

// jsonPathAST can be Evaluated
var _ Evaluator = jsonPathAST{}

func (q jsonPathAST) Query(current *yaml.Node, root *yaml.Node) []*yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}

	ctx := NewFilterContext(root)

	// Only enable parent tracking if the query uses ^ or @parent
	if q.hasParentReferences() {
		ctx.EnableParentTracking()
	}

	result := make([]*yaml.Node, 0)
	result = append(result, root)

	for _, segment := range q.segments {
		newValue := []*yaml.Node{}
		for _, value := range result {
			newValue = append(newValue, segment.Query(ctx, value, root)...)
		}
		result = newValue
	}
	return result
}

// hasParentReferences checks if the AST uses parent selectors (^) or @parent context variable
func (q jsonPathAST) hasParentReferences() bool {
	for _, seg := range q.segments {
		if seg.hasParentReferences() {
			return true
		}
	}
	return false
}

func (s *segment) hasParentReferences() bool {
	if s.kind == segmentKindParent {
		return true
	}
	if s.child != nil && s.child.hasParentReferences() {
		return true
	}
	if s.descendant != nil && s.descendant.hasParentReferences() {
		return true
	}
	return false
}

func (s *innerSegment) hasParentReferences() bool {
	for _, sel := range s.selectors {
		if sel.hasParentReferences() {
			return true
		}
	}
	return false
}

func (s *selector) hasParentReferences() bool {
	if s.filter != nil && s.filter.hasParentReferences() {
		return true
	}
	return false
}

func (f *filterSelector) hasParentReferences() bool {
	if f.expression != nil {
		return f.expression.hasParentReferences()
	}
	return false
}

func (e *logicalOrExpr) hasParentReferences() bool {
	for _, expr := range e.expressions {
		if expr.hasParentReferences() {
			return true
		}
	}
	return false
}

func (e *logicalAndExpr) hasParentReferences() bool {
	for _, expr := range e.expressions {
		if expr.hasParentReferences() {
			return true
		}
	}
	return false
}

func (e *basicExpr) hasParentReferences() bool {
	if e.parenExpr != nil && e.parenExpr.expr != nil {
		return e.parenExpr.expr.hasParentReferences()
	}
	if e.comparisonExpr != nil {
		return e.comparisonExpr.hasParentReferences()
	}
	if e.testExpr != nil {
		return e.testExpr.hasParentReferences()
	}
	return false
}

func (e *comparisonExpr) hasParentReferences() bool {
	if e.left != nil && e.left.hasParentReferences() {
		return true
	}
	if e.right != nil && e.right.hasParentReferences() {
		return true
	}
	return false
}

func (c *comparable) hasParentReferences() bool {
	if c.contextVar != nil && c.contextVar.kind == contextVarParent {
		return true
	}
	if c.singularQuery != nil {
		if c.singularQuery.relQuery != nil {
			for _, seg := range c.singularQuery.relQuery.segments {
				if seg.hasParentReferences() {
					return true
				}
			}
		}
	}
	return false
}

func (e *testExpr) hasParentReferences() bool {
	if e.filterQuery != nil {
		if e.filterQuery.relQuery != nil {
			for _, seg := range e.filterQuery.relQuery.segments {
				if seg.hasParentReferences() {
					return true
				}
			}
		}
	}
	return false
}

// parentTrackingEnabled checks if parent tracking is enabled in the index
func parentTrackingEnabled(idx index) bool {
	if fc, ok := idx.(FilterContext); ok {
		return fc.ParentTrackingEnabled()
	}
	return false
}

func (s segment) Query(idx index, value *yaml.Node, root *yaml.Node) []*yaml.Node {
    switch s.kind {
    case segmentKindChild:
        return s.child.Query(idx, value, root)
    case segmentKindDescendant:
        // run the inner segment against this node
        var result = []*yaml.Node{}
        children := descend(value, root)
        for _, child := range children {
            result = append(result, s.descendant.Query(idx, child, root)...)
        }
        // make children unique by pointer value
        result = unique(result)
        return result
    case segmentKindProperyName:
        found := idx.getPropertyKey(value)
        if found != nil {
            return []*yaml.Node{found}
        }
        return []*yaml.Node{}
    case segmentKindParent:
        // JSONPath Plus parent selector: ^ returns the parent of the current node
        parent := idx.getParentNode(value)
        if parent != nil {
            return []*yaml.Node{parent}
        }
        // No parent found (could be root node)
        return []*yaml.Node{}
    }
    panic("no segment type")
}

func unique(nodes []*yaml.Node) []*yaml.Node {
    // stably returns a new slice containing only the unique elements from nodes
    res := make([]*yaml.Node, 0)
    seen := make(map[*yaml.Node]bool)
    for _, node := range nodes {
        if _, ok := seen[node]; !ok {
            res = append(res, node)
            seen[node] = true
        }
    }
    return res
}

func (s innerSegment) Query(idx index, value *yaml.Node, root *yaml.Node) []*yaml.Node {
    result := []*yaml.Node{}
    trackParents := parentTrackingEnabled(idx)

    switch s.kind {
    case segmentDotWildcard:
        // Check for inherited pending segment from previous wildcard/slice
        var inheritedPending string
        if fc, ok := idx.(FilterContext); ok {
            inheritedPending = fc.GetAndClearPendingPathSegment(value)
        }

        switch value.Kind {
        case yaml.MappingNode:
            for i, child := range value.Content {
                if i%2 == 1 {
                    keyNode := value.Content[i-1]
                    idx.setPropertyKey(keyNode, value)
                    idx.setPropertyKey(child, keyNode)
                    if trackParents {
                        idx.setParentNode(child, value)
                    }
                    // Track pending path segment and property name for this node
                    if fc, ok := idx.(FilterContext); ok {
                        thisSegment := normalizePathSegment(keyNode.Value)
                        fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
                        fc.SetPendingPropertyName(child, keyNode.Value) // For @parentProperty
                    }
                    result = append(result, child)
                }
            }
        case yaml.SequenceNode:
            for i, child := range value.Content {
                if trackParents {
                    idx.setParentNode(child, value)
                }
                // Track pending path segment and property name for this node
                if fc, ok := idx.(FilterContext); ok {
                    thisSegment := normalizeIndexSegment(i)
                    fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
                    fc.SetPendingPropertyName(child, strconv.Itoa(i)) // For @parentProperty (array index as string)
                }
                result = append(result, child)
            }
        }
        return result
    case segmentDotMemberName:
        if value.Kind == yaml.MappingNode {
            // Check for inherited pending segment from wildcard/slice
            var inheritedPending string
            if fc, ok := idx.(FilterContext); ok {
                inheritedPending = fc.GetAndClearPendingPathSegment(value)
            }

            for i := 0; i < len(value.Content); i += 2 {
                key := value.Content[i]
                val := value.Content[i+1]

                if key.Value == s.dotName {
                    idx.setPropertyKey(key, value)
                    idx.setPropertyKey(val, key)
                    if trackParents {
                        idx.setParentNode(val, value)
                    }
                    if fc, ok := idx.(FilterContext); ok {
                        thisSegment := normalizePathSegment(key.Value)
                        if inheritedPending != "" {
                            // Propagate combined pending to result for later consumption
                            fc.SetPendingPathSegment(val, inheritedPending+thisSegment)
                        } else {
                            // No wildcard ancestry - push directly to path
                            fc.PushPathSegment(thisSegment)
                        }
                        fc.SetPropertyName(key.Value)
                    }
                    result = append(result, val)
                    break
                }
            }
        }

    case segmentLongHand:
        for _, selector := range s.selectors {
            result = append(result, selector.Query(idx, value, root)...)
        }
    default:
        panic("unknown child segment kind")
    }

    return result

}

func (s selector) Query(idx index, value *yaml.Node, root *yaml.Node) []*yaml.Node {
    trackParents := parentTrackingEnabled(idx)

    switch s.kind {
    case selectorSubKindName:
        if value.Kind != yaml.MappingNode {
            return nil
        }
        // Check for inherited pending segment from wildcard/slice
        var inheritedPending string
        if fc, ok := idx.(FilterContext); ok {
            inheritedPending = fc.GetAndClearPendingPathSegment(value)
        }

        var key string
        for i, child := range value.Content {
            if i%2 == 0 {
                key = child.Value
                continue
            }
            if key == s.name && i%2 == 1 {
                idx.setPropertyKey(value.Content[i], value.Content[i-1])
                idx.setPropertyKey(value.Content[i-1], value)
                if trackParents {
                    idx.setParentNode(child, value)
                }
                if fc, ok := idx.(FilterContext); ok {
                    thisSegment := normalizePathSegment(key)
                    if inheritedPending != "" {
                        // Propagate combined pending to result for later consumption
                        fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
                    } else {
                        // No wildcard ancestry - push directly to path
                        fc.PushPathSegment(thisSegment)
                    }
                    fc.SetPropertyName(key)
                }
                return []*yaml.Node{child}
            }
        }
    case selectorSubKindArrayIndex:
        if value.Kind != yaml.SequenceNode {
            return nil
        }
        if s.index >= int64(len(value.Content)) || s.index < -int64(len(value.Content)) {
            return nil
        }
        // Check for inherited pending segment from wildcard/slice
        var inheritedPending string
        if fc, ok := idx.(FilterContext); ok {
            inheritedPending = fc.GetAndClearPendingPathSegment(value)
        }

        var child *yaml.Node
        var actualIndex int
        if s.index < 0 {
            actualIndex = int(int64(len(value.Content)) + s.index)
            child = value.Content[actualIndex]
        } else {
            actualIndex = int(s.index)
            child = value.Content[s.index]
        }
        if trackParents {
            idx.setParentNode(child, value)
        }
        if fc, ok := idx.(FilterContext); ok {
            thisSegment := normalizeIndexSegment(actualIndex)
            if inheritedPending != "" {
                // Propagate combined pending to result for later consumption
                fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
            } else {
                // No wildcard ancestry - push directly to path
                fc.PushPathSegment(thisSegment)
            }
        }
        return []*yaml.Node{child}
    case selectorSubKindWildcard:
        // Check for inherited pending segment from previous wildcard/slice
        var inheritedPending string
        if fc, ok := idx.(FilterContext); ok {
            inheritedPending = fc.GetAndClearPendingPathSegment(value)
        }

        if value.Kind == yaml.SequenceNode {
            for i, child := range value.Content {
                if trackParents {
                    idx.setParentNode(child, value)
                }
                // Track pending path segment and property name for this node
                if fc, ok := idx.(FilterContext); ok {
                    thisSegment := normalizeIndexSegment(i)
                    fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
                    fc.SetPendingPropertyName(child, strconv.Itoa(i)) // For @parentProperty
                }
            }
            return value.Content
        } else if value.Kind == yaml.MappingNode {
            var result []*yaml.Node
            for i, child := range value.Content {
                if i%2 == 1 {
                    keyNode := value.Content[i-1]
                    idx.setPropertyKey(keyNode, value)
                    idx.setPropertyKey(child, keyNode)
                    if trackParents {
                        idx.setParentNode(child, value)
                    }
                    // Track pending path segment and property name for this node
                    if fc, ok := idx.(FilterContext); ok {
                        thisSegment := normalizePathSegment(keyNode.Value)
                        fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
                        fc.SetPendingPropertyName(child, keyNode.Value) // For @parentProperty
                    }
                    result = append(result, child)
                }
            }
            return result
        }
        return nil
    case selectorSubKindArraySlice:
        if value.Kind != yaml.SequenceNode {
            return nil
        }
        if len(value.Content) == 0 {
            return nil
        }
        // Check for inherited pending segment from previous wildcard/slice
        var inheritedPending string
        if fc, ok := idx.(FilterContext); ok {
            inheritedPending = fc.GetAndClearPendingPathSegment(value)
        }

        step := int64(1)
        if s.slice.step != nil {
            step = *s.slice.step
        }
        if step == 0 {
            return nil
        }

        start, end := s.slice.start, s.slice.end
        lower, upper := bounds(start, end, step, int64(len(value.Content)))

        var result []*yaml.Node
        if step > 0 {
            for i := lower; i < upper; i += step {
                child := value.Content[i]
                if trackParents {
                    idx.setParentNode(child, value)
                }
                // Track pending path segment and property name for this node
                if fc, ok := idx.(FilterContext); ok {
                    thisSegment := normalizeIndexSegment(int(i))
                    fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
                    fc.SetPendingPropertyName(child, strconv.Itoa(int(i))) // For @parentProperty
                }
                result = append(result, child)
            }
        } else {
            for i := upper; i > lower; i += step {
                child := value.Content[i]
                if trackParents {
                    idx.setParentNode(child, value)
                }
                // Track pending path segment and property name for this node
                if fc, ok := idx.(FilterContext); ok {
                    thisSegment := normalizeIndexSegment(int(i))
                    fc.SetPendingPathSegment(child, inheritedPending+thisSegment)
                    fc.SetPendingPropertyName(child, strconv.Itoa(int(i))) // For @parentProperty
                }
                result = append(result, child)
            }
        }

        return result
    case selectorSubKindFilter:
        var result []*yaml.Node
        // Get parent property name - prefer pending property name from wildcard/slice,
        // fall back to current PropertyName
        var parentPropName string
        var pushedPendingSegment bool
        if fc, ok := idx.(FilterContext); ok {
            // First check for pending property name from wildcard/slice
            if pendingPropName := fc.GetAndClearPendingPropertyName(value); pendingPropName != "" {
                parentPropName = pendingPropName
            } else {
                parentPropName = fc.PropertyName()
            }
            // Check if this node has a pending path segment from a wildcard/slice
            if pendingSeg := fc.GetAndClearPendingPathSegment(value); pendingSeg != "" {
                fc.PushPathSegment(pendingSeg)
                pushedPendingSegment = true
            }
        }
        switch value.Kind {
        case yaml.MappingNode:
            for i := 1; i < len(value.Content); i += 2 {
                keyNode := value.Content[i-1]
                valueNode := value.Content[i]
                idx.setPropertyKey(keyNode, value)
                idx.setPropertyKey(valueNode, keyNode)
                if trackParents {
                    idx.setParentNode(valueNode, value)
                }

                if fc, ok := idx.(FilterContext); ok {
                    fc.SetParentPropertyName(parentPropName)
                    fc.SetPropertyName(keyNode.Value)
                    fc.SetParent(value)
                    fc.SetIndex(-1)
                    fc.PushPathSegment(normalizePathSegment(keyNode.Value))
                }

                if s.filter.Matches(idx, valueNode, root) {
                    result = append(result, valueNode)
                }

                if fc, ok := idx.(FilterContext); ok {
                    fc.PopPathSegment()
                }
            }
        case yaml.SequenceNode:
            for i, child := range value.Content {
                if trackParents {
                    idx.setParentNode(child, value)
                }

                if fc, ok := idx.(FilterContext); ok {
                    fc.SetParentPropertyName(parentPropName)
                    fc.SetPropertyName(strconv.Itoa(i))
                    fc.SetParent(value)
                    fc.SetIndex(i)
                    fc.PushPathSegment(normalizeIndexSegment(i))
                }

                if s.filter.Matches(idx, child, root) {
                    result = append(result, child)
                }

                if fc, ok := idx.(FilterContext); ok {
                    fc.PopPathSegment()
                }
            }
        }
        // Pop the pending segment if we pushed one
        if pushedPendingSegment {
            if fc, ok := idx.(FilterContext); ok {
                fc.PopPathSegment()
            }
        }
        return result
    }
    return nil
}

func normalize(i, length int64) int64 {
    if i >= 0 {
        return i
    }
    return length + i
}

func bounds(start, end *int64, step, length int64) (int64, int64) {
    var nStart, nEnd int64
    if start != nil {
        nStart = normalize(*start, length)
    } else if step > 0 {
        nStart = 0
    } else {
        nStart = length - 1
    }
    if end != nil {
        nEnd = normalize(*end, length)
    } else if step > 0 {
        nEnd = length
    } else {
        nEnd = -1
    }

    var lower, upper int64
    if step >= 0 {
        lower = max(min(nStart, length), 0)
        upper = min(max(nEnd, 0), length)
    } else {
        upper = min(max(nStart, -1), length-1)
        lower = min(max(nEnd, -1), length-1)
    }

    return lower, upper
}

func (s filterSelector) Matches(idx index, node *yaml.Node, root *yaml.Node) bool {
    return s.expression.Matches(idx, node, root)
}

func (e logicalOrExpr) Matches(idx index, node *yaml.Node, root *yaml.Node) bool {
    for _, expr := range e.expressions {
        if expr.Matches(idx, node, root) {
            return true
        }
    }
    return false
}

func (e logicalAndExpr) Matches(idx index, node *yaml.Node, root *yaml.Node) bool {
    for _, expr := range e.expressions {
        if !expr.Matches(idx, node, root) {
            return false
        }
    }
    return true
}

func (e basicExpr) Matches(idx index, node *yaml.Node, root *yaml.Node) bool {
    if e.parenExpr != nil {
        result := e.parenExpr.expr.Matches(idx, node, root)
        if e.parenExpr.not {
            return !result
        }
        return result
    } else if e.comparisonExpr != nil {
        return e.comparisonExpr.Matches(idx, node, root)
    } else if e.testExpr != nil {
        return e.testExpr.Matches(idx, node, root)
    }
    return false
}

func (e comparisonExpr) Matches(idx index, node *yaml.Node, root *yaml.Node) bool {
    leftValue := e.left.Evaluate(idx, node, root)
    rightValue := e.right.Evaluate(idx, node, root)

    switch e.op {
    case equalTo:
        return leftValue.Equals(rightValue)
    case notEqualTo:
        return !leftValue.Equals(rightValue)
    case lessThan:
        return leftValue.LessThan(rightValue)
    case lessThanEqualTo:
        return leftValue.LessThanOrEqual(rightValue)
    case greaterThan:
        return rightValue.LessThan(leftValue)
    case greaterThanEqualTo:
        return rightValue.LessThanOrEqual(leftValue)
    default:
        return false
    }
}

func (e testExpr) Matches(idx index, node *yaml.Node, root *yaml.Node) bool {
    var result bool
    if e.filterQuery != nil {
        result = len(e.filterQuery.Query(idx, node, root)) > 0
    } else if e.functionExpr != nil {
        funcResult := e.functionExpr.Evaluate(idx, node, root)
        if funcResult.bool != nil {
            result = *funcResult.bool
        } else if funcResult.null == nil {
            result = true
        }
    }
    if e.not {
        return !result
    }
    return result
}

func (q filterQuery) Query(idx index, node *yaml.Node, root *yaml.Node) []*yaml.Node {
    if q.relQuery != nil {
        return q.relQuery.Query(idx, node, root)
    }
    if q.jsonPathQuery != nil {
        return q.jsonPathQuery.Query(node, root)
    }
    return nil
}

func (q relQuery) Query(idx index, node *yaml.Node, root *yaml.Node) []*yaml.Node {
    result := []*yaml.Node{node}
    for _, seg := range q.segments {
        var newResult []*yaml.Node
        for _, value := range result {
            newResult = append(newResult, seg.Query(idx, value, root)...)
        }
        result = newResult
    }
    return result
}

func (q absQuery) Query(idx index, node *yaml.Node, root *yaml.Node) []*yaml.Node {
    result := []*yaml.Node{root}
    for _, seg := range q.segments {
        var newResult []*yaml.Node
        for _, value := range result {
            newResult = append(newResult, seg.Query(idx, value, root)...)
        }
        result = newResult
    }
    return result
}
