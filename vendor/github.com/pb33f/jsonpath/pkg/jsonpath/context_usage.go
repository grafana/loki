package jsonpath

type contextVarUsage struct {
	property       bool
	parent         bool
	parentProperty bool
	path           bool
	index          bool
}

// mark records that a context variable kind is used
func (u *contextVarUsage) mark(kind contextVarKind) {
	switch kind {
	case contextVarProperty:
		u.property = true
	case contextVarParent:
		u.parent = true
	case contextVarParentProperty:
		u.parentProperty = true
	case contextVarPath:
		u.path = true
	case contextVarIndex:
		u.index = true
	}
}

// contextVarUsage collects context variable usage for the full JSONPath AST
func (q jsonPathAST) contextVarUsage() contextVarUsage {
	var usage contextVarUsage
	for _, seg := range q.segments {
		seg.collectContextVarUsage(&usage)
	}
	return usage
}

// collectContextVarUsage records context variable usage for a query, handling nil safely
func (q *jsonPathAST) collectContextVarUsage(usage *contextVarUsage) {
	if q == nil {
		return
	}
	for _, seg := range q.segments {
		seg.collectContextVarUsage(usage)
	}
}

// hasPropertyNameReferences reports whether the query references the property name selector
func (q jsonPathAST) hasPropertyNameReferences() bool {
	for _, seg := range q.segments {
		if seg.hasPropertyNameReferences() {
			return true
		}
	}
	return false
}

// hasPropertyNameReferencesPtr is a nil-safe wrapper around hasPropertyNameReferences
func (q *jsonPathAST) hasPropertyNameReferencesPtr() bool {
	if q == nil {
		return false
	}
	return q.hasPropertyNameReferences()
}

// hasPropertyNameReferences reports whether the segment references the property name selector
func (s *segment) hasPropertyNameReferences() bool {
	if s.kind == segmentKindProperyName {
		return true
	}
	if s.child != nil && s.child.hasPropertyNameReferences() {
		return true
	}
	if s.descendant != nil && s.descendant.hasPropertyNameReferences() {
		return true
	}
	return false
}

// hasPropertyNameReferences reports whether the inner segment references the property name selector
func (s *innerSegment) hasPropertyNameReferences() bool {
	for _, sel := range s.selectors {
		if sel.hasPropertyNameReferences() {
			return true
		}
	}
	return false
}

// hasPropertyNameReferences reports whether the selector references the property name selector
func (s *selector) hasPropertyNameReferences() bool {
	if s.filter != nil && s.filter.hasPropertyNameReferences() {
		return true
	}
	return false
}

// hasPropertyNameReferences reports whether the filter selector references the property name selector
func (f *filterSelector) hasPropertyNameReferences() bool {
	if f.expression == nil {
		return false
	}
	return f.expression.hasPropertyNameReferences()
}

// hasPropertyNameReferences reports whether any logical OR expression references the property name selector
func (e *logicalOrExpr) hasPropertyNameReferences() bool {
	for _, expr := range e.expressions {
		if expr.hasPropertyNameReferences() {
			return true
		}
	}
	return false
}

// hasPropertyNameReferences reports whether any logical AND expression references the property name selector
func (e *logicalAndExpr) hasPropertyNameReferences() bool {
	for _, expr := range e.expressions {
		if expr.hasPropertyNameReferences() {
			return true
		}
	}
	return false
}

// hasPropertyNameReferences reports whether the basic expression references the property name selector
func (e *basicExpr) hasPropertyNameReferences() bool {
	if e.parenExpr != nil && e.parenExpr.expr != nil {
		return e.parenExpr.expr.hasPropertyNameReferences()
	}
	if e.comparisonExpr != nil {
		return e.comparisonExpr.hasPropertyNameReferences()
	}
	if e.testExpr != nil {
		return e.testExpr.hasPropertyNameReferences()
	}
	return false
}

// hasPropertyNameReferences reports whether the comparison expression references the property name selector
func (e *comparisonExpr) hasPropertyNameReferences() bool {
	if e.left != nil && e.left.hasPropertyNameReferences() {
		return true
	}
	if e.right != nil && e.right.hasPropertyNameReferences() {
		return true
	}
	return false
}

// hasPropertyNameReferences reports whether the comparable references the property name selector
func (c *comparable) hasPropertyNameReferences() bool {
	if c.singularQuery != nil && c.singularQuery.hasPropertyNameReferences() {
		return true
	}
	if c.functionExpr != nil && c.functionExpr.hasPropertyNameReferences() {
		return true
	}
	return false
}

// hasPropertyNameReferences reports whether the test expression references the property name selector
func (e *testExpr) hasPropertyNameReferences() bool {
	if e.filterQuery != nil && e.filterQuery.hasPropertyNameReferences() {
		return true
	}
	if e.functionExpr != nil && e.functionExpr.hasPropertyNameReferences() {
		return true
	}
	return false
}

// hasPropertyNameReferences reports whether the function expression references the property name selector
func (e *functionExpr) hasPropertyNameReferences() bool {
	for _, arg := range e.args {
		if arg.hasPropertyNameReferences() {
			return true
		}
	}
	return false
}

// hasPropertyNameReferences reports whether the function argument references the property name selector
func (a *functionArgument) hasPropertyNameReferences() bool {
	if a.filterQuery != nil && a.filterQuery.hasPropertyNameReferences() {
		return true
	}
	if a.logicalExpr != nil && a.logicalExpr.hasPropertyNameReferences() {
		return true
	}
	if a.functionExpr != nil && a.functionExpr.hasPropertyNameReferences() {
		return true
	}
	return false
}

// hasPropertyNameReferences reports whether the filter query references the property name selector
func (q *filterQuery) hasPropertyNameReferences() bool {
	if q == nil {
		return false
	}
	if q.relQuery != nil {
		return q.relQuery.hasPropertyNameReferences()
	}
	if q.jsonPathQuery != nil {
		return q.jsonPathQuery.hasPropertyNameReferencesPtr()
	}
	return false
}

// hasPropertyNameReferences reports whether the relative query references the property name selector
func (q *relQuery) hasPropertyNameReferences() bool {
	for _, seg := range q.segments {
		if seg.hasPropertyNameReferences() {
			return true
		}
	}
	return false
}

// hasPropertyNameReferences reports whether the absolute query references the property name selector
func (q *absQuery) hasPropertyNameReferences() bool {
	for _, seg := range q.segments {
		if seg.hasPropertyNameReferences() {
			return true
		}
	}
	return false
}

// hasPropertyNameReferences reports whether the singular query references the property name selector
func (q *singularQuery) hasPropertyNameReferences() bool {
	if q.relQuery != nil {
		return q.relQuery.hasPropertyNameReferences()
	}
	if q.absQuery != nil {
		return q.absQuery.hasPropertyNameReferences()
	}
	return false
}

// collectContextVarUsage records usage from a segment
func (s *segment) collectContextVarUsage(usage *contextVarUsage) {
	if s.child != nil {
		s.child.collectContextVarUsage(usage)
	}
	if s.descendant != nil {
		s.descendant.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from an inner segment
func (s *innerSegment) collectContextVarUsage(usage *contextVarUsage) {
	for _, sel := range s.selectors {
		sel.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a selector
func (s *selector) collectContextVarUsage(usage *contextVarUsage) {
	if s.filter != nil {
		s.filter.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a filter selector
func (f *filterSelector) collectContextVarUsage(usage *contextVarUsage) {
	if f.expression != nil {
		f.expression.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a logical OR expression
func (e *logicalOrExpr) collectContextVarUsage(usage *contextVarUsage) {
	for _, expr := range e.expressions {
		expr.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a logical AND expression
func (e *logicalAndExpr) collectContextVarUsage(usage *contextVarUsage) {
	for _, expr := range e.expressions {
		expr.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a basic expression
func (e *basicExpr) collectContextVarUsage(usage *contextVarUsage) {
	if e.parenExpr != nil && e.parenExpr.expr != nil {
		e.parenExpr.expr.collectContextVarUsage(usage)
		return
	}
	if e.comparisonExpr != nil {
		e.comparisonExpr.collectContextVarUsage(usage)
		return
	}
	if e.testExpr != nil {
		e.testExpr.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a comparison expression
func (e *comparisonExpr) collectContextVarUsage(usage *contextVarUsage) {
	if e.left != nil {
		e.left.collectContextVarUsage(usage)
	}
	if e.right != nil {
		e.right.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a comparable
func (c *comparable) collectContextVarUsage(usage *contextVarUsage) {
	if c.contextVar != nil {
		usage.mark(c.contextVar.kind)
	}
	if c.singularQuery != nil {
		c.singularQuery.collectContextVarUsage(usage)
	}
	if c.functionExpr != nil {
		c.functionExpr.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a test expression
func (e *testExpr) collectContextVarUsage(usage *contextVarUsage) {
	if e.filterQuery != nil {
		e.filterQuery.collectContextVarUsage(usage)
	}
	if e.functionExpr != nil {
		e.functionExpr.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a function expression
func (e *functionExpr) collectContextVarUsage(usage *contextVarUsage) {
	for _, arg := range e.args {
		arg.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a function argument
func (a *functionArgument) collectContextVarUsage(usage *contextVarUsage) {
	if a.contextVar != nil {
		usage.mark(a.contextVar.kind)
	}
	if a.filterQuery != nil {
		a.filterQuery.collectContextVarUsage(usage)
	}
	if a.logicalExpr != nil {
		a.logicalExpr.collectContextVarUsage(usage)
	}
	if a.functionExpr != nil {
		a.functionExpr.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a filter query
func (q *filterQuery) collectContextVarUsage(usage *contextVarUsage) {
	if q == nil {
		return
	}
	if q.relQuery != nil {
		q.relQuery.collectContextVarUsage(usage)
	}
	if q.jsonPathQuery != nil {
		q.jsonPathQuery.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a relative query
func (q *relQuery) collectContextVarUsage(usage *contextVarUsage) {
	for _, seg := range q.segments {
		seg.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from an absolute query
func (q *absQuery) collectContextVarUsage(usage *contextVarUsage) {
	for _, seg := range q.segments {
		seg.collectContextVarUsage(usage)
	}
}

// collectContextVarUsage records usage from a singular query
func (q *singularQuery) collectContextVarUsage(usage *contextVarUsage) {
	if q.relQuery != nil {
		q.relQuery.collectContextVarUsage(usage)
	}
	if q.absQuery != nil {
		q.absQuery.collectContextVarUsage(usage)
	}
}
