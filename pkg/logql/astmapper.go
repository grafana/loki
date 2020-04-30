package logql

import (
	"fmt"

	"github.com/pkg/errors"
)

// ASTMapper is the exported interface for mapping between multiple AST representations
type ASTMapper interface {
	Map(Expr) (Expr, error)
}

// CloneExpr is a helper function to clone a node.
func CloneExpr(expr Expr) (Expr, error) {
	return ParseExpr(expr.String())
}

func badASTMapping(expected string, got Expr) error {
	return fmt.Errorf("Bad AST mapping: expected one type (%s), but got (%T)", expected, got)
}

// MapperUnsupportedType is a helper for signaling that an evaluator does not support an Expr type
func MapperUnsupportedType(expr Expr, m ASTMapper) error {
	return errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
}
