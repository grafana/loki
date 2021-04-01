package log

type JSONExpression struct {
	Identifier string
	Expression string
}

func NewJSONExpr(identifier, expression string) JSONExpression {
	return JSONExpression{
		Identifier: identifier,
		Expression: expression,
	}
}
