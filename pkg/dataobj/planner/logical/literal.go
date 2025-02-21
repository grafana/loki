// Package logical implements logical query plan operations and expressions
package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time checks to ensure types implement Expr interface
var (
	_ Expr = LiteralString{}
	_ Expr = LiteralI64{}
)

// LiteralString represents a string constant in the query plan
type LiteralString struct {
	// str holds the string value
	str string
}

// LitStr creates a string literal expression
func LitStr(v string) LiteralString {
	return LiteralString{str: v}
}

// ToField converts the string literal to a column schema
func (l LiteralString) ToField(_ Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: l.str,
		Type: datasetmd.VALUE_TYPE_STRING,
	}
}

// LiteralI64 represents a 64-bit integer constant in the query plan
type LiteralI64 struct {
	// n holds the integer value
	n int64
}

// LitI64 creates an int64 literal expression
func LitI64(v int64) LiteralI64 {
	return LiteralI64{n: v}
}

// ToField converts the integer literal to a column schema
func (l LiteralI64) ToField(_ Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: fmt.Sprint(l.n),
		Type: datasetmd.VALUE_TYPE_INT64,
	}
}
