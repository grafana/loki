package logql

// ASTMapper is the exported interface for mapping between multiple AST representations
type ASTMapper interface {
	Map(Expr) (Expr, error)
}

// CloneExpr is a helper function to clone a node.
func CloneExpr(expr Expr) (Expr, error) {
	return ParseExpr(expr.String())
}
