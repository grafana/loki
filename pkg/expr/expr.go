// Package expr provides utilities for evaluating expressions against a
// [columnar.RecordBatch] with a selection vector.
//
// Package expr is EXPERIMENTAL and currently only intended to be used by
// [github.com/grafana/loki/v3/pkg/dataobj].
package expr

import (
	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// Expression represents an operation that can be evaluated to produce a result.
type Expression interface{ isExpr() }

// Types implementing [Expression].
type (
	// Constant is an [Expression] that produces a single scalar value when
	// evaluated.
	Constant struct{ Value columnar.Scalar }

	// Column is an [Expression] that looks up the column by name in the record
	// batch supplied to [Evaluate].
	//
	// If the column doesn't exist, a Null column is produced.
	Column struct{ Name string }

	// Unary is an [Expression] that performs a unary operation against a single
	// argument.
	//
	// The result of the expression depends on value of [UnaryOp]. The documentation
	// of [UnaryOp] will describe the behavior of the expression.
	Unary struct {
		Op    UnaryOp
		Value Expression
	}

	// Binary is an [Expression] that performs a binary operation against a left and
	// a right expression.
	//
	// The result of the expression depends on value of [BinaryOp]. The documentation
	// of [BinaryOp] will describe the behavior of the expression.
	Binary struct {
		Left  Expression
		Op    BinaryOp
		Right Expression
	}

	// Regexp is an [Expression] used as the right-hand side of a
	// [BinaryOpMatchRegex].
	//
	// Regexp cannot be evaluated directly into a datum.
	Regexp struct{ Expression *regexp.Regexp }

	// ValueSet is an [Expression] used as the right-hand side of a [BinaryOpIn].
	//
	// ValueSet cannot be evaluated directly into a datum.
	ValueSet struct{ Values *columnar.Set }
)

func (*Constant) isExpr() {}
func (*Column) isExpr()   {}
func (*Unary) isExpr()    {}
func (*Binary) isExpr()   {}
func (*Regexp) isExpr()   {}
func (*ValueSet) isExpr() {}
