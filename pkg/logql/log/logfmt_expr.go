package log

type LogfmtExpression struct {
	Identifier string
	Expression string
}

func NewLogfmtExpr(identifier, expression string) LogfmtExpression {
	return LogfmtExpression{
		Identifier: identifier,
		Expression: expression,
	}
}
