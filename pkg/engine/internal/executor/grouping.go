package executor

import (
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// ExpressionEvaluatorForGrouping is an interface for expression evaluation during grouping.
// This allows different implementations for different use cases (e.g., full aggregation vs sharding).
type ExpressionEvaluatorForGrouping interface {
	EvalForGrouping(expr physical.Expression, rec arrow.RecordBatch) (arrow.Array, error)
}

func collectGroupingColumns(record arrow.RecordBatch, grouping physical.Grouping, evaluator *expressionEvaluator, identCache *semconv.IdentifierCache) ([]*array.String, []arrow.Field, error) {
	if grouping.Without {
		return collectWithoutGroupingColumns(record, grouping, identCache)
	}
	return CollectByGroupingColumns(record, grouping, evaluator)
}

// CollectByGroupingColumns collects grouping columns from a record batch.
// This is exported so it can be reused for sharding logic.
func CollectByGroupingColumns(record arrow.RecordBatch, grouping physical.Grouping, evaluator ExpressionEvaluatorForGrouping) ([]*array.String, []arrow.Field, error) {
	arrays := make([]*array.String, 0, len(grouping.Columns))
	fields := make([]arrow.Field, 0, len(grouping.Columns))

	for _, columnExpr := range grouping.Columns {
		arr, err := evaluator.EvalForGrouping(columnExpr, record)
		if err != nil {
			return nil, nil, err
		}

		if arr.DataType().ID() != types.Arrow.String.ID() {
			return nil, nil, fmt.Errorf("unsupported datatype for grouping %s", arr.DataType())
		}

		stringArr, ok := arr.(*array.String)
		if !ok {
			return nil, nil, fmt.Errorf("expected string array for grouping, got %T", arr)
		}
		arrays = append(arrays, stringArr)

		colExpr, ok := columnExpr.(*physical.ColumnExpr)
		if !ok {
			return nil, nil, fmt.Errorf("invalid column expression type %T", columnExpr)
		}
		ident := semconv.NewIdentifier(colExpr.Ref.Column, colExpr.Ref.Type, types.Loki.String)
		fields = append(fields, semconv.FieldFromIdent(ident, true))
	}

	return arrays, fields, nil
}

// collectWithoutGroupingColumns collects columns from the input record excluding
// those that match the grouping expressions.
//
// The returned fields & arrays are sorted in the order of their column names.
// Sorting is necessary to ensure that the grouping keys are in the same order
// irrespective of the order of columns in the input record.
//
// And columns with the same short name are coalesced into a single array.
// Without this, columns with same short name but different [types.ColumnType]
// would be treated as separate grouping keys, which is not the intended behavior.
func collectWithoutGroupingColumns(record arrow.RecordBatch, grouping physical.Grouping, identCache *semconv.IdentifierCache) ([]*array.String, []arrow.Field, error) {
	shortNames := make([]string, 0)
	columns := make(map[string][]*columnWithType)

	for i, field := range record.Schema().Fields() {
		ident, err := identCache.ParseFQN(field.Name)
		if err != nil {
			return nil, nil, err
		}

		if !isGroupingCandidate(ident.ColumnType()) {
			continue
		}

		match, err := identMatchesGrouping(grouping.Columns, ident)
		if err != nil {
			return nil, nil, err
		}

		// exclude columns that match `without` grouping keys
		if match {
			continue
		}

		arr, ok := record.Column(i).(*array.String)
		if !ok {
			return nil, nil, fmt.Errorf("unsupported datatype for grouping %s", record.Column(i).DataType())
		}

		shortName := ident.ShortName()
		if _, exists := columns[shortName]; !exists {
			shortNames = append(shortNames, shortName)
		}

		columns[shortName] = append(columns[shortName], &columnWithType{
			col: arr,
			ct:  ident.ColumnType(),
		})
	}

	// sort names to ensure deterministic order of grouping keys.
	// input records may have columns in any order.
	slices.Sort(shortNames)

	arrays := make([]*array.String, 0, len(shortNames))
	fields := make([]arrow.Field, 0, len(shortNames))
	for _, shortName := range shortNames {
		cols := columns[shortName]

		var arr arrow.Array
		if len(cols) == 1 {
			arr = cols[0].col
		} else {
			arr = NewCoalesce(cols)
		}

		arrays = append(arrays, arr.(*array.String))

		// always set to ambiguous type.
		// Imagine two records with the same short name but different types.
		// Record 1 has `utf8.label.env`
		// Record 2 has `utf8.metadata.env`
		//
		// If the original type is preserved, aggregation will treat them
		// as different groups, which is not the intended behavior.
		//
		// TODO: aggregator.go should be updated to use short names
		// instead of full identifiers for grouping keys.
		ident := semconv.NewIdentifier(shortName, types.ColumnTypeAmbiguous, types.Loki.String)
		fields = append(fields, semconv.FieldFromIdent(ident, true))
	}

	return arrays, fields, nil
}

func isGroupingCandidate(columnType types.ColumnType) bool {
	return columnType == types.ColumnTypeLabel ||
		columnType == types.ColumnTypeMetadata ||
		columnType == types.ColumnTypeParsed ||
		// aggregation node downstream of another aggregation node may
		// receive grouping keys with ambiguous type.
		columnType == types.ColumnTypeAmbiguous
}

func identMatchesGrouping(grouping []physical.ColumnExpression, ident *semconv.Identifier) (bool, error) {
	for _, g := range grouping {
		colExpr, ok := g.(*physical.ColumnExpr)
		if !ok {
			return false, fmt.Errorf("unknown column expression %v", g)
		}

		// Match ambiguous columns only by name.
		if colExpr.Ref.Type == types.ColumnTypeAmbiguous && colExpr.Ref.Column == ident.ShortName() {
			return true, nil
		}

		// Match all other columns by name and type.
		if colExpr.Ref.Column == ident.ShortName() && colExpr.Ref.Type == ident.ColumnType() {
			return true, nil
		}
	}

	return false, nil
}

// ComputeHashFromValues computes a hash from field names and their corresponding values.
// This is the core hash computation used for grouping and aggregation.
func ComputeHashFromValues(digest *xxhash.Digest, fields []arrow.Field, values []string) uint64 {
	if len(fields) == 0 {
		return 0
	}

	digest.Reset()
	for i, val := range values {
		if i > 0 {
			_, _ = digest.Write([]byte{0}) // separator
		}

		_, _ = digest.WriteString(fields[i].Name)
		_, _ = digest.Write([]byte("="))
		_, _ = digest.WriteString(val)
	}
	return digest.Sum64()
}

// ComputeGroupingHash computes the hash for a single row across grouping columns.
// This is the same hash computation used by the aggregator for determining unique series.
// The arrays and fields must be in the same order as returned by collectGroupingColumns.
func ComputeGroupingHash(arrays []*array.String, fields []arrow.Field, rowIdx int, digest *xxhash.Digest) uint64 {
	if len(arrays) == 0 {
		return 0
	}

	values := make([]string, len(arrays))
	for i, arr := range arrays {
		if arr.IsNull(rowIdx) {
			values[i] = ""
		} else {
			values[i] = arr.Value(rowIdx)
		}
	}

	return ComputeHashFromValues(digest, fields, values)
}
