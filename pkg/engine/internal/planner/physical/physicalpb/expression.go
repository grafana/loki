package physicalpb

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
