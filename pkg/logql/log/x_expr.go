package log

type XExpression struct {
	Identifier string
	Expression string
}

func NewXExpr(identifier, expression string) XExpression {
	return XExpression{
		Identifier: identifier,
		Expression: expression,
	}
}
