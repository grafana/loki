package physical

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestInferExpressionType(t *testing.T) {
	tests := []struct {
		name string
		expr Expression
		// expected is the inferred data type; nil means the type cannot be
		// determined statically.
		expected    types.DataType
		expectError string
	}{
		{
			name:     "string literal",
			expr:     NewLiteral("foo"),
			expected: types.Loki.String,
		},
		{
			name:     "duration literal",
			expr:     NewLiteral(types.Duration(300)),
			expected: types.Loki.Duration,
		},
		{
			name:     "label column is a string",
			expr:     newColumnExpr("app", types.ColumnTypeLabel),
			expected: types.Loki.String,
		},
		{
			name:     "metadata column is a string",
			expr:     newColumnExpr("trace_id", types.ColumnTypeMetadata),
			expected: types.Loki.String,
		},
		{
			name:     "parsed column is a string",
			expr:     newColumnExpr("status", types.ColumnTypeParsed),
			expected: types.Loki.String,
		},
		{
			name:     "ambiguous column is a string",
			expr:     newColumnExpr("duration", types.ColumnTypeAmbiguous),
			expected: types.Loki.String,
		},
		{
			name:     "builtin timestamp column",
			expr:     newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
			expected: types.Loki.Timestamp,
		},
		{
			name:     "builtin message column",
			expr:     newColumnExpr(types.ColumnNameBuiltinMessage, types.ColumnTypeBuiltin),
			expected: types.Loki.String,
		},
		{
			name:     "generated value column",
			expr:     newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated),
			expected: types.Loki.Float,
		},
		{
			name:     "generated error column",
			expr:     newColumnExpr(types.ColumnNameError, types.ColumnTypeGenerated),
			expected: types.Loki.String,
		},
		{
			name:     "unknown generated column cannot be inferred",
			expr:     newColumnExpr("value_left", types.ColumnTypeGenerated),
			expected: nil,
		},
		{
			name: "label filter compares string with string",
			expr: &BinaryExpr{
				Left:  newColumnExpr("env", types.ColumnTypeLabel),
				Right: NewLiteral("prod"),
				Op:    types.BinaryOpEq,
			},
			expected: types.Loki.Bool,
		},
		{
			name: "timestamp range comparison",
			expr: &BinaryExpr{
				Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
				Right: NewLiteral(types.Timestamp(1742826126000000000)),
				Op:    types.BinaryOpGte,
			},
			expected: types.Loki.Bool,
		},
		{
			name: "arithmetic on the value column",
			expr: &BinaryExpr{
				Left:  newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated),
				Right: NewLiteral(float64(300)),
				Op:    types.BinaryOpDiv,
			},
			expected: types.Loki.Float,
		},
		{
			name: "conjunction of comparisons",
			expr: &BinaryExpr{
				Left: &BinaryExpr{
					Left:  newColumnExpr("env", types.ColumnTypeLabel),
					Right: NewLiteral("prod"),
					Op:    types.BinaryOpEq,
				},
				Right: &BinaryExpr{
					Left:  newColumnExpr("msg", types.ColumnTypeParsed),
					Right: NewLiteral("error"),
					Op:    types.BinaryOpMatchRe,
				},
				Op: types.BinaryOpAnd,
			},
			expected: types.Loki.Bool,
		},
		{
			name: "not of a comparison",
			expr: &UnaryExpr{
				Left: &BinaryExpr{
					Left:  newColumnExpr("env", types.ColumnTypeLabel),
					Right: NewLiteral("prod"),
					Op:    types.BinaryOpEq,
				},
				Op: types.UnaryOpNot,
			},
			expected: types.Loki.Bool,
		},
		{
			name: "unwrap cast of an ambiguous column",
			expr: &UnaryExpr{
				Left: newColumnExpr("duration", types.ColumnTypeAmbiguous),
				Op:   types.UnaryOpCastDuration,
			},
			expected: types.Loki.Float,
		},
		{
			name: "unknown operand type defers validation to execution time",
			expr: &BinaryExpr{
				Left:  newColumnExpr("value_left", types.ColumnTypeGenerated),
				Right: newColumnExpr("value_right", types.ColumnTypeGenerated),
				Op:    types.BinaryOpDiv,
			},
			expected: nil,
		},
		{
			name: "string label compared with integer literal",
			expr: &BinaryExpr{
				Left:  newColumnExpr("age", types.ColumnTypeMetadata),
				Right: NewLiteral(int64(21)),
				Op:    types.BinaryOpGt,
			},
			expectError: "types do not match",
		},
		{
			name: "string label compared with duration literal",
			expr: &BinaryExpr{
				Left:  newColumnExpr("duration", types.ColumnTypeAmbiguous),
				Right: NewLiteral(types.Duration(5000000000)),
				Op:    types.BinaryOpGt,
			},
			expectError: "types do not match",
		},
		{
			name: "arithmetic on string columns",
			expr: &BinaryExpr{
				Left:  newColumnExpr("app", types.ColumnTypeLabel),
				Right: NewLiteral("foo"),
				Op:    types.BinaryOpAdd,
			},
			expectError: "failed to lookup binary function",
		},
		{
			name: "regex match on the timestamp column",
			expr: &BinaryExpr{
				Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
				Right: NewLiteral(types.Timestamp(1742826126000000000)),
				Op:    types.BinaryOpMatchRe,
			},
			expectError: "failed to lookup binary function",
		},
		{
			name: "not of a string column",
			expr: &UnaryExpr{
				Left: newColumnExpr("app", types.ColumnTypeLabel),
				Op:   types.UnaryOpNot,
			},
			expectError: "failed to lookup unary function",
		},
		{
			name: "cast of the value column",
			expr: &UnaryExpr{
				Left: newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated),
				Op:   types.UnaryOpCastFloat,
			},
			expectError: "failed to lookup unary function",
		},
		{
			name: "error inside nested expression is reported",
			expr: &BinaryExpr{
				Left: &BinaryExpr{
					Left:  newColumnExpr("age", types.ColumnTypeMetadata),
					Right: NewLiteral(int64(21)),
					Op:    types.BinaryOpGt,
				},
				Right: NewLiteral(true),
				Op:    types.BinaryOpAnd,
			},
			expectError: "types do not match",
		},
		{
			name: "parse expression validates its arguments",
			expr: &VariadicExpr{
				Op: types.VariadicOpParseLogfmt,
				Expressions: []Expression{
					&BinaryExpr{
						Left:  newColumnExpr("age", types.ColumnTypeMetadata),
						Right: NewLiteral(int64(21)),
						Op:    types.BinaryOpGt,
					},
				},
			},
			expectError: "types do not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dt, err := inferExpressionType(tt.expr)
			if tt.expectError != "" {
				require.ErrorContains(t, err, tt.expectError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, dt)
		})
	}
}

func TestTypeCheckNode(t *testing.T) {
	t.Run("filter with boolean predicate is valid", func(t *testing.T) {
		node := &Filter{
			Predicates: []Expression{
				&BinaryExpr{
					Left:  newColumnExpr("env", types.ColumnTypeLabel),
					Right: NewLiteral("prod"),
					Op:    types.BinaryOpEq,
				},
			},
		}
		require.NoError(t, typeCheckNode(node))
	})

	t.Run("filter with invalid predicate signature", func(t *testing.T) {
		node := &Filter{
			Predicates: []Expression{
				&BinaryExpr{
					Left:  newColumnExpr("age", types.ColumnTypeMetadata),
					Right: NewLiteral(int64(21)),
					Op:    types.BinaryOpGt,
				},
			},
		}
		require.ErrorContains(t, typeCheckNode(node), "types do not match")
	})

	t.Run("filter with non-boolean predicate", func(t *testing.T) {
		node := &Filter{
			Predicates: []Expression{
				newColumnExpr("env", types.ColumnTypeLabel),
			},
		}
		require.ErrorContains(t, typeCheckNode(node), "predicate must evaluate to a boolean")
	})

	t.Run("filter with predicate of unknown type is not checked", func(t *testing.T) {
		node := &Filter{
			Predicates: []Expression{
				newColumnExpr("some_generated_column", types.ColumnTypeGenerated),
			},
		}
		require.NoError(t, typeCheckNode(node))
	})

	t.Run("projection with valid expressions", func(t *testing.T) {
		node := &Projection{
			Expressions: []Expression{
				&UnaryExpr{
					Left: newColumnExpr("duration", types.ColumnTypeAmbiguous),
					Op:   types.UnaryOpCastDuration,
				},
				&BinaryExpr{
					Left:  newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated),
					Right: NewLiteral(float64(300)),
					Op:    types.BinaryOpDiv,
				},
			},
		}
		require.NoError(t, typeCheckNode(node))
	})

	t.Run("projection with invalid expression", func(t *testing.T) {
		node := &Projection{
			Expressions: []Expression{
				&BinaryExpr{
					Left:  newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated),
					Right: NewLiteral(int64(300)),
					Op:    types.BinaryOpDiv,
				},
			},
		}
		require.ErrorContains(t, typeCheckNode(node), "types do not match")
	})
}

func TestTypeCheckPlan(t *testing.T) {
	plan := &Plan{}
	filter := &Filter{
		NodeID: ulid.Make(),
		Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("duration", types.ColumnTypeAmbiguous),
				Right: NewLiteral(int64(100)),
				Op:    types.BinaryOpGt,
			},
		},
	}
	scan := &DataObjScan{NodeID: ulid.Make()}
	plan.graph.Add(filter)
	plan.graph.Add(scan)
	require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan}))

	err := typeCheckPlan(plan)
	require.ErrorContains(t, err, "failed to lookup binary function for signature GT(utf8,int64)")
	require.ErrorContains(t, err, "types do not match")
}
