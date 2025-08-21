package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

func TestResolveFilterExpression_WithParsedColumns(t *testing.T) {
	t.Run("Filter expression resolves parsed column reference", func(t *testing.T) {
		// Create a column registry with parsed columns
		registry := NewColumnRegistry()

		// Register "level" as a parsed column (simulating what Parse node would do)
		err := registry.RegisterParsedColumns([]string{"level", "status"})
		require.NoError(t, err)

		// Create a logical filter expression: level="error"
		logicalExpr := &logical.BinOp{
			Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous), // Initially ambiguous
			Right: logical.NewLiteral("error"),
			Op:    types.BinaryOpEq,
		}

		// Resolve the expression using the registry
		resolver := NewExpressionResolver(registry)
		physicalExpr, err := resolver.ResolveExpression(logicalExpr)
		require.NoError(t, err)

		// Assert that the physical expression has the correct column type
		binaryExpr, ok := physicalExpr.(*BinaryExpr)
		require.True(t, ok, "Expected BinaryExpr")

		columnExpr, ok := binaryExpr.Left.(*ColumnExpr)
		require.True(t, ok, "Expected ColumnExpr on left side")

		// The key assertion: "level" should be resolved as ColumnTypeParsed, not Ambiguous
		require.Equal(t, types.ColumnTypeParsed, columnExpr.Ref.Type)
		require.Equal(t, "level", columnExpr.Ref.Column)

		// Right side should still be the literal
		literalExpr, ok := binaryExpr.Right.(*LiteralExpr)
		require.True(t, ok, "Expected LiteralExpr on right side")
		require.Equal(t, "error", literalExpr.Any())
	})

	t.Run("Multiple parsed columns in compound filter", func(t *testing.T) {
		// Create a column registry with parsed columns
		registry := NewColumnRegistry()
		err := registry.RegisterParsedColumns([]string{"level", "status"})
		require.NoError(t, err)

		// Create a compound filter: level="error" AND status=500
		logicalExpr := &logical.BinOp{
			Left: &logical.BinOp{
				Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
				Right: logical.NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
			Right: &logical.BinOp{
				Left:  logical.NewColumnRef("status", types.ColumnTypeAmbiguous),
				Right: logical.NewLiteral(int64(500)),
				Op:    types.BinaryOpEq,
			},
			Op: types.BinaryOpAnd,
		}

		// Resolve the expression
		resolver := NewExpressionResolver(registry)
		physicalExpr, err := resolver.ResolveExpression(logicalExpr)
		require.NoError(t, err)

		// Check the compound structure
		andExpr, ok := physicalExpr.(*BinaryExpr)
		require.True(t, ok)
		require.Equal(t, types.BinaryOpAnd, andExpr.Op)

		// Check left side (level="error")
		leftBinary, ok := andExpr.Left.(*BinaryExpr)
		require.True(t, ok)
		leftColumn, ok := leftBinary.Left.(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnTypeParsed, leftColumn.Ref.Type)
		require.Equal(t, "level", leftColumn.Ref.Column)

		// Check right side (status=500)
		rightBinary, ok := andExpr.Right.(*BinaryExpr)
		require.True(t, ok)
		rightColumn, ok := rightBinary.Left.(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnTypeParsed, rightColumn.Ref.Type)
		require.Equal(t, "status", rightColumn.Ref.Column)
	})

	t.Run("Non-parsed column remains as label type", func(t *testing.T) {
		// Create a column registry with only "level" as parsed
		registry := NewColumnRegistry()
		err := registry.RegisterParsedColumns([]string{"level"})
		require.NoError(t, err)

		// Create a filter with a non-parsed column: app="test"
		// "app" is not registered as parsed, so should remain as label
		logicalExpr := &logical.BinOp{
			Left:  logical.NewColumnRef("app", types.ColumnTypeLabel), // Explicitly a label
			Right: logical.NewLiteral("test"),
			Op:    types.BinaryOpEq,
		}

		// Resolve the expression
		resolver := NewExpressionResolver(registry)
		physicalExpr, err := resolver.ResolveExpression(logicalExpr)
		require.NoError(t, err)

		// Check that "app" remains as a label column
		binaryExpr, ok := physicalExpr.(*BinaryExpr)
		require.True(t, ok)
		columnExpr, ok := binaryExpr.Left.(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnTypeLabel, columnExpr.Ref.Type)
		require.Equal(t, "app", columnExpr.Ref.Column)
	})
}

func TestExpressionResolver_AmbiguousColumnPrecedence(t *testing.T) {
	t.Run("Ambiguous column resolution in filter expression", func(t *testing.T) {
		// Create a registry with "app" as both label and parsed
		registry := NewColumnRegistry()
		err := registry.RegisterLabelColumns([]string{"app"})
		require.NoError(t, err)
		err = registry.RegisterParsedColumns([]string{"app"})
		require.NoError(t, err)

		// Create a filter expression with ambiguous "app"
		logicalExpr := &logical.BinOp{
			Left:  logical.NewColumnRef("app", types.ColumnTypeAmbiguous),
			Right: logical.NewLiteral("backend"),
			Op:    types.BinaryOpEq,
		}

		// Resolve the expression
		resolver := NewExpressionResolver(registry)
		physicalExpr, err := resolver.ResolveExpression(logicalExpr)
		require.NoError(t, err)

		// Check that "app" resolved to parsed (higher precedence)
		binaryExpr, ok := physicalExpr.(*BinaryExpr)
		require.True(t, ok)
		columnExpr, ok := binaryExpr.Left.(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnTypeParsed, columnExpr.Ref.Type, "Ambiguous 'app' should resolve to Parsed")
	})
}

func TestColumnTypePrecedence_Constants(t *testing.T) {
	t.Run("Precedence order verification", func(t *testing.T) {
		// Verify the complete precedence order:
		// Generated > Parsed > Metadata > Label > Builtin

		require.Less(t, types.ColumnTypePrecedence(types.ColumnTypeGenerated),
			types.ColumnTypePrecedence(types.ColumnTypeParsed),
			"Generated should have higher precedence (lower number) than Parsed")

		require.Less(t, types.ColumnTypePrecedence(types.ColumnTypeParsed),
			types.ColumnTypePrecedence(types.ColumnTypeMetadata),
			"Parsed should have higher precedence than Metadata")

		require.Less(t, types.ColumnTypePrecedence(types.ColumnTypeMetadata),
			types.ColumnTypePrecedence(types.ColumnTypeLabel),
			"Metadata should have higher precedence than Label")

		require.Less(t, types.ColumnTypePrecedence(types.ColumnTypeLabel),
			types.ColumnTypePrecedence(types.ColumnTypeBuiltin),
			"Label should have higher precedence than Builtin")
	})
}
