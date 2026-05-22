// Package expr provides utilities for evaluating expressions against
// [columnar.Datum] values with a selection vector.
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

	// Identity is an Expression that resolves to the input datum passed to
	// [Evaluate]. It represents "the current data in scope" and is used by
	// leaf-level layout readers where column references have been rewritten
	// to Identity by parent readers.
	Identity struct{}

	// Extract is an [Expression] that evaluates Value and extracts the field
	// with the given Name from the resulting [*columnar.Struct].
	//
	// If the field doesn't exist, a Null column is produced.
	Extract struct {
		Name  string
		Value Expression
	}

	// Include is an [Expression] that evaluates Value and returns a new
	// [*columnar.Struct] containing only the fields with the given Names.
	//
	// Names that don't exist in the source struct are silently skipped.
	Include struct {
		Names []string
		Value Expression
	}

	// Exclude is an [Expression] that evaluates Value and returns a new
	// [*columnar.Struct] with the fields matching Names removed.
	//
	// Names that don't exist in the source struct are silently ignored.
	Exclude struct {
		Names []string
		Value Expression
	}

	// MakeStruct is an [Expression] that evaluates each of the Values
	// expressions and constructs a new [*columnar.Struct] with the given
	// Names. Each value must evaluate to a [columnar.Array], and all arrays
	// must have the same length.
	MakeStruct struct {
		Names  []string
		Values []Expression
	}
)

func (*Constant) isExpr()   {}
func (*Column) isExpr()     {}
func (*Unary) isExpr()      {}
func (*Binary) isExpr()     {}
func (*Regexp) isExpr()     {}
func (*ValueSet) isExpr()   {}
func (*Identity) isExpr()   {}
func (*Extract) isExpr()    {}
func (*Include) isExpr()    {}
func (*Exclude) isExpr()    {}
func (*MakeStruct) isExpr() {}
