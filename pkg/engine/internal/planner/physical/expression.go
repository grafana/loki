package physical

import fmt "fmt"

func (e *BinaryExpression) ToExpression() *Expression {
	return &Expression{Kind: &Expression_BinaryExpression{BinaryExpression: e}}
}

func (e *ColumnExpression) ToExpression() *Expression {
	return &Expression{Kind: &Expression_ColumnExpression{ColumnExpression: e}}
}

func (e *LiteralExpression) ToExpression() *Expression {
	return &Expression{Kind: &Expression_LiteralExpression{LiteralExpression: e}}
}

func (e *UnaryExpression) ToExpression() *Expression {
	return &Expression{Kind: &Expression_UnaryExpression{UnaryExpression: e}}
}

func cloneExpressions(exprs []*Expression) []*Expression {
	clonedExprs := make([]*Expression, len(exprs))
	for i, expr := range exprs {
		tmp := expr.Clone()
		clonedExprs[i] = &tmp
	}
	return clonedExprs
}

func cloneColExpressions(colExprs []*ColumnExpression) []*ColumnExpression {
	clonedExprs := make([]*ColumnExpression, len(colExprs))
	for i, expr := range colExprs {
		tmp := expr.Clone()
		clonedExprs[i] = tmp.GetColumnExpression()
	}
	return clonedExprs
}

func (e *Expression) Clone() Expression {
	switch k := e.Kind.(type) {
	case *Expression_UnaryExpression:
		return k.UnaryExpression.Clone()
	case *Expression_BinaryExpression:
		return k.BinaryExpression.Clone()
	case *Expression_LiteralExpression:
		return k.LiteralExpression.Clone()
	case *Expression_ColumnExpression:
		return k.ColumnExpression.Clone()
	default:
		panic(fmt.Sprintf("unknown expression type %T", k))
	}
}

// Clone returns a copy of the [UnaryExpr].
func (e *UnaryExpression) Clone() Expression {
	tmp := e.Value.Clone()
	return *(&UnaryExpression{
		Value: &tmp,
		Op:    e.Op,
	}).ToExpression()
}

// Clone returns a copy of the [BinaryExpr].
func (e *BinaryExpression) Clone() Expression {
	tmpLeft := e.Left.Clone()
	tmpRight := e.Right.Clone()
	return *(&BinaryExpression{
		Left:  &tmpLeft,
		Right: &tmpRight,
		Op:    e.Op,
	}).ToExpression()
}

// Clone returns a copy of the [LiteralExpr].
func (e *LiteralExpression) Clone() Expression {
	// No need to clone literals.
	return *(&LiteralExpression{Kind: e.Kind}).ToExpression()
}

// Clone returns a copy of the [ColumnExpr].
func (e *ColumnExpression) Clone() Expression {
	return *(&ColumnExpression{Name: e.Name, Type: e.Type}).ToExpression()
}
