package expr

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Evaluate processes expr against the provided batch, producing a datum as a
// result using alloc.
//
// # Selection Vectors
//
// Selection defines which rows are evaluated. Selected rows have a true value
// in the selection vector. When selection has Len() == 0, all rows are selected.
// When selection has Len() > 0, only rows with a true bit are selected. Rows that
// are not selected are treated as null in array results.
//
// # Short-Circuit Optimization (AND/OR)
//
// For logical AND and OR operations, Evaluate implements short-circuit evaluation
// using selection vector propagation to avoid unnecessary computation:
//
//   - AND optimization: When left side is false or null, right side is skipped
//     for those rows (since false AND x = false, null AND x = null)
//
//   - OR optimization: When left side is true, right side is skipped for those
//     rows (since true OR x = true)
//
// Example: For expression "(status = 200) AND (body =~ '.*error.*')", if the
// left side evaluates to false for 90% of rows, the expensive regex match on
// the right side is skipped for 90% of rows.
//
// The return type of Evaluate depends on the expression provided. See the
// documentation for implementations of Expression for what they produce when
// evaluated.
func Evaluate(alloc *memory.Allocator, expr Expression, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	nrows := 0
	if batch != nil {
		nrows = int(batch.NumRows())
	}
	if selection.Len() > 0 && selection.Len() != nrows {
		return nil, fmt.Errorf("selection length mismatch: %d != %d", selection.Len(), nrows)
	}

	switch expr := expr.(type) {
	case *Constant:
		return expr.Value, nil

	case *Column:
		columnIndex := -1
		if schema := batch.Schema(); schema != nil {
			_, columnIndex = schema.ColumnIndex(expr.Name)
		}

		if columnIndex == -1 {
			validity := memory.NewBitmap(alloc, int(batch.NumRows()))
			validity.AppendCount(false, int(batch.NumRows()))
			return columnar.NewNull(validity), nil
		}
		col := batch.Column(int64(columnIndex))
		// Apply selection to column data. Note: compute functions also apply
		// selection, resulting in idempotent double application which is safe.
		return applySelectionToColumn(alloc, col, selection)

	case *Unary:
		return evaluateUnary(alloc, expr, batch, selection)

	case *Binary:
		return evaluateBinary(alloc, expr, batch, selection)

	case *Regexp:
		return nil, fmt.Errorf("regexp can only be evaluated as the right-hand side of regex match operations")

	default:
		panic(fmt.Sprintf("unexpected expression type %T", expr))
	}
}

func evaluateUnary(alloc *memory.Allocator, expr *Unary, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	switch expr.Op {
	case UnaryOpNOT:
		value, err := Evaluate(alloc, expr.Value, batch, selection)
		if err != nil {
			return nil, err
		}
		return compute.Not(alloc, value, selection)
	default:
		return nil, fmt.Errorf("unexpected unary operator %s", expr.Op)
	}
}

func evaluateBinary(alloc *memory.Allocator, expr *Binary, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	// Check for special operators that need different handling of their arguments.
	switch expr.Op {
	case BinaryOpMatchRegex:
		return evaluateSpecialBinary(alloc, expr, batch, selection)
	}

	// Short-circuit optimization for AND/OR operations (selection vector propagation).
	// For AND: skip right-side evaluation when left is false/null (false AND x = false)
	// For OR: skip right-side evaluation when left is true (true OR x = true)
	// Other operations evaluate both sides unconditionally.
	left, err := Evaluate(alloc, expr.Left, batch, selection)
	if err != nil {
		return nil, err
	}

	switch expr.Op {
	case BinaryOpAND:
		// Short-circuit optimization: refine selection based on left result.
		// For AND operations, we can skip evaluating right side for rows where:
		// - left is false (false AND x = false, regardless of x)
		// - left is null (null AND x = null, regardless of x)
		//
		// The refined selection marks as "unselected":
		// - Rows already unselected in original selection
		// - Rows where left is false or null (deterministic for AND)
		//
		// We then evaluate right with this refined selection, potentially skipping
		// expensive operations. Since right isn't evaluated for false/null rows,
		// those rows become null in right. After computing AND, we fix up rows
		// where left is false to ensure the result is false (not null), maintaining
		// proper AND semantics (false AND null = false).
		refinedSelection, err := compute.CombineSelectionsAnd(alloc, left, selection)
		if err != nil {
			return nil, err
		}

		right, err := Evaluate(alloc, expr.Right, batch, refinedSelection)
		if err != nil {
			return nil, err
		}

		// Compute AND with empty selection (both inputs already have appropriate nulls)
		result, err := compute.And(alloc, left, right, memory.Bitmap{})
		if err != nil {
			return nil, err
		}

		// Fix up the result: for rows where left is false, ensure result is false
		// (not null), since false AND anything = false. This handles cases where
		// right was artificially null due to refined selection.
		return fixAndResultForFalseLeft(alloc, left, result, selection)
	case BinaryOpOR:
		// Short-circuit optimization: refine selection based on left result.
		// For OR operations, we can skip evaluating right side for rows where:
		// - left is true (true OR x = true, regardless of x)
		//
		// Note: we cannot skip null rows, since null OR true = true.
		//
		// The refined selection marks as "unselected":
		// - Rows already unselected in original selection
		// - Rows where left is true (deterministic for OR)
		//
		// We then evaluate right with this refined selection, potentially skipping
		// expensive operations. Since right isn't evaluated for true rows,
		// those rows become null in right. After computing OR, we fix up rows
		// where left is true to ensure the result is true (not null), maintaining
		// proper OR semantics (true OR null = true).
		refinedSelection, err := compute.CombineSelectionsOr(alloc, left, selection)
		if err != nil {
			return nil, err
		}

		right, err := Evaluate(alloc, expr.Right, batch, refinedSelection)
		if err != nil {
			return nil, err
		}

		// Compute OR with empty selection (both inputs already have appropriate nulls)
		result, err := compute.Or(alloc, left, right, memory.Bitmap{})
		if err != nil {
			return nil, err
		}

		// Fix up the result: for rows where left is true, ensure result is true
		// (not null), since true OR anything = true. This handles cases where
		// right was artificially null due to refined selection.
		return fixOrResultForTrueLeft(alloc, left, result, selection)
	}

	right, err := Evaluate(alloc, expr.Right, batch, selection)
	if err != nil {
		return nil, err
	}

	switch expr.Op {
	case BinaryOpEQ:
		return compute.Equals(alloc, left, right, selection)
	case BinaryOpNEQ:
		return compute.NotEquals(alloc, left, right, selection)
	case BinaryOpGT:
		return compute.GreaterThan(alloc, left, right, selection)
	case BinaryOpGTE:
		return compute.GreaterOrEqual(alloc, left, right, selection)
	case BinaryOpLT:
		return compute.LessThan(alloc, left, right, selection)
	case BinaryOpLTE:
		return compute.LessOrEqual(alloc, left, right, selection)
	case BinaryOpAND:
		return compute.And(alloc, left, right, selection)
	case BinaryOpOR:
		return compute.Or(alloc, left, right, selection)
	case BinaryOpHasSubstr:
		return compute.Substr(alloc, left, right, selection)
	case BinaryOpHasSubstrIgnoreCase:
		return compute.SubstrInsensitive(alloc, left, right, selection)
	default:
		return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
	}
}

// fixAndResultForFalseLeft fixes AND operation results to ensure false AND x = false
// (not null). When using refined selection optimization, rows where left is false
// may produce null in the AND result (because right was artificially null). This
// function corrects those rows to have false values with valid (non-null) status.
func fixAndResultForFalseLeft(alloc *memory.Allocator, left, result columnar.Datum, originalSelection memory.Bitmap) (columnar.Datum, error) {
	// Handle scalar result case - no fixing needed
	if _, ok := result.(columnar.Scalar); ok {
		return result, nil
	}

	// Only handle boolean arrays
	leftArr, leftOk := left.(*columnar.Bool)
	resultArr, resultOk := result.(*columnar.Bool)
	if !leftOk || !resultOk {
		return result, nil
	}

	length := resultArr.Len()
	if leftArr.Len() != length {
		return result, fmt.Errorf("length mismatch in fixAndResultForFalseLeft: %d != %d", leftArr.Len(), length)
	}

	// Check if there are any rows that need fixing:
	// - left is false (value bit = 0, validity bit = 1)
	// - result is null (validity bit = 0)
	needsFix := false
	for i := 0; i < length; i++ {
		if !leftArr.IsNull(i) && !leftArr.Get(i) && resultArr.IsNull(i) {
			needsFix = true
			break
		}
	}

	if !needsFix {
		return result, nil
	}

	// Create fixed result by copying and updating validity for false-left rows
	newValues := memory.NewBitmap(alloc, length)
	newValues.Resize(length)

	newValidity := memory.NewBitmap(alloc, length)
	newValidity.Resize(length)

	// Build new values and validity bitmaps
	for i := 0; i < length; i++ {
		if !leftArr.IsNull(i) && !leftArr.Get(i) {
			// Left is false (not null), so result must be false (not null)
			newValues.Set(i, false)  // set value to false
			newValidity.Set(i, true) // mark as valid
		} else if !resultArr.IsNull(i) {
			// Result already valid, keep it
			newValues.Set(i, resultArr.Get(i))
			newValidity.Set(i, true)
		} else {
			// Result is null, keep it null (validity bit already false from Resize)
			newValues.Set(i, false) // set value to false (doesn't matter, but avoid garbage)
			newValidity.Set(i, false)
		}
	}

	fixed := columnar.NewBool(newValues, newValidity)

	// Apply original selection to mark originally-unselected rows as null
	return compute.ApplySelectionToBoolArray(alloc, fixed, originalSelection)
}

// fixOrResultForTrueLeft fixes OR operation results to ensure true OR x = true
// (not null). When using refined selection optimization, rows where left is true
// may produce null in the OR result (because right was artificially null). This
// function corrects those rows to have true values with valid (non-null) status.
func fixOrResultForTrueLeft(alloc *memory.Allocator, left, result columnar.Datum, originalSelection memory.Bitmap) (columnar.Datum, error) {
	// Handle scalar result case - no fixing needed
	if _, ok := result.(columnar.Scalar); ok {
		return result, nil
	}

	// Only handle boolean arrays
	leftArr, leftOk := left.(*columnar.Bool)
	resultArr, resultOk := result.(*columnar.Bool)
	if !leftOk || !resultOk {
		return result, nil
	}

	length := resultArr.Len()
	if leftArr.Len() != length {
		return result, fmt.Errorf("length mismatch in fixOrResultForTrueLeft: %d != %d", leftArr.Len(), length)
	}

	// Check if there are any rows that need fixing:
	// - left is true (value bit = 1, validity bit = 1)
	// - result is null (validity bit = 0)
	needsFix := false
	for i := 0; i < length; i++ {
		if !leftArr.IsNull(i) && leftArr.Get(i) && resultArr.IsNull(i) {
			needsFix = true
			break
		}
	}

	if !needsFix {
		return result, nil
	}

	// Create fixed result by copying and updating validity for true-left rows
	newValues := memory.NewBitmap(alloc, length)
	newValues.Resize(length)

	newValidity := memory.NewBitmap(alloc, length)
	newValidity.Resize(length)

	// Build new values and validity bitmaps
	for i := 0; i < length; i++ {
		if !leftArr.IsNull(i) && leftArr.Get(i) {
			// Left is true (not null), so result must be true (not null)
			newValues.Set(i, true)   // set value to true
			newValidity.Set(i, true) // mark as valid
		} else if !resultArr.IsNull(i) {
			// Result already valid, keep it
			newValues.Set(i, resultArr.Get(i))
			newValidity.Set(i, true)
		} else {
			// Result is null, keep it null (validity bit already false from Resize)
			newValues.Set(i, false) // set value to false (doesn't matter, but avoid garbage)
			newValidity.Set(i, false)
		}
	}

	fixed := columnar.NewBool(newValues, newValidity)

	// Apply original selection to mark originally-unselected rows as null
	return compute.ApplySelectionToBoolArray(alloc, fixed, originalSelection)
}

// applySelectionToColumn applies a selection bitmap to column data by marking
// unselected rows as null. This delegates to compute package functions to avoid
// code duplication.
func applySelectionToColumn(alloc *memory.Allocator, col columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	if selection.Len() == 0 {
		return col, nil
	}

	// Scalars don't need selection applied
	arr, ok := col.(columnar.Array)
	if !ok {
		return col, nil
	}

	// Apply selection using compute package utilities based on array type
	switch a := arr.(type) {
	case *columnar.Bool:
		return compute.ApplySelectionToBoolArray(alloc, a, selection)
	case *columnar.Number[int64]:
		return compute.ApplySelectionToNumberArray(alloc, a, selection)
	case *columnar.Number[uint64]:
		return compute.ApplySelectionToNumberArray(alloc, a, selection)
	case *columnar.UTF8:
		return compute.ApplySelectionToUTF8Array(alloc, a, selection)
	case *columnar.Null:
		return col, nil
	default:
		return nil, fmt.Errorf("unsupported column type for selection: %T", a)
	}
}

// evaluateSpecialBinary evaluates binary expressions for which one of the
// arguments does not evaluate into an expression of its own.
func evaluateSpecialBinary(alloc *memory.Allocator, expr *Binary, batch *columnar.RecordBatch, selection memory.Bitmap) (columnar.Datum, error) {
	switch expr.Op {
	case BinaryOpMatchRegex:
		left, err := Evaluate(alloc, expr.Left, batch, selection)
		if err != nil {
			return nil, err
		}

		right, ok := expr.Right.(*Regexp)
		if !ok {
			return nil, fmt.Errorf("right-hand side of regex match operation must be a regexp, got %T", expr.Right)
		}

		return compute.RegexpMatch(alloc, left, right.Expression, selection)
	}

	return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
}
