// Package compute implements stateless computational operations over columnar
// data.
//
// Compute functions operate on one or more [columnar.Datum] values, which is
// an opaque representation of either an array or a scalar value of a specific
// value kind.
//
// Unless otherwise specified, all compute functions return data allocated with
// the [memory.Allocator] provided to the compute function.
//
// # Selection Vectors
//
// Selection vectors enable efficient row-level filtering in columnar compute
// operations without materializing filtered subsets. This is critical for
// performance when processing large datasets with selective filters.
//
// Key benefits of selection vectors:
//   - Lazy evaluation: filtered data is not copied or materialized
//   - Memory efficiency: unselected rows remain in place
//   - Composability: selections can be combined using bitwise operations
//   - Consistent null-handling: unselected rows are treated as null
//
// ## Selection Vector Representation
//
// Selection vectors are represented as [memory.Bitmap] from Apache Arrow.
// Each bit in the bitmap corresponds to a row in the input data:
//   - true bit = row is selected (include in computation)
//   - false bit = row is not selected (treat as null)
//
// ## Selection Vector Conventions
//
// Empty selection (all rows selected):
//
// When the selection parameter has Len() == 0, all rows are selected. This is
// the default behavior and has zero performance overhead compared to operations
// without selection support.
//
//	emptySelection := memory.Bitmap{}  // Len() == 0
//	result := compute.Equals(mem, left, right, emptySelection)  // all rows selected
//
// Non-empty selection (selective filtering):
//
// A selection bitmap with Len() > 0 indicates that only rows where the
// corresponding bit is true are selected. Unselected rows are treated as null.
//
//	selection := memory.NewBitmap(mem, 10)  // 10 rows
//	selection.Set(0)  // select first row
//	selection.Set(5)  // select sixth row
//	result := compute.Equals(mem, left, right, selection)
//	// only rows 0 and 5 are computed, others are null
//
// ## Null-Marking Behavior
//
// When a selection vector is applied, unselected rows are marked as null in
// the output. This is implemented by ANDing the data's validity bitmap with
// the selection bitmap:
//
//	result_validity = data_validity AND selection
//
// This approach has important properties:
//   - Already-null rows remain null (null AND true = null)
//   - Unselected rows become null (valid AND false = null)
//   - Selected valid rows remain valid (valid AND true = valid)
//   - No data copying or array resizing required
//
// ## Dispatch Pattern
//
// Compute functions use a dispatch pattern to handle different input
// combinations (scalar-scalar, scalar-array, array-scalar, array-array).
// Selection vectors are only applied to array results:
//   - Scalar-Scalar (SS): No selection (result is scalar)
//   - Scalar-Array (SA): Apply selection to result array
//   - Array-Scalar (AS): Apply selection to result array
//   - Array-Array (AA): Apply selection to result array
//
// ## Selection Application Patterns
//
// There are two patterns for applying selections within compute operations,
// chosen based on per-row operation cost:
//
// Pattern A: Early termination (for expensive operations like RegexpMatch):
//
//	for i := range array.Len() {
//	    if selection.Len() > 0 && !selection.Get(i) {
//	        results.AppendNull()
//	        continue
//	    }
//	    // expensive operation on selected rows only
//	}
//
// Pattern B: Post-processing (for cheap operations like Equals):
//
//	// Compute all rows (selection checking would add more overhead)
//	result := computeAllRows(...)
//	// Mask unselected rows as null in result
//	return applySelectionToArray(result, selection)
//
// UTF8 operations use Pattern A to avoid expensive string operations on
// unselected rows. Equality and logical operations use Pattern B because
// checking selection per-row adds more overhead than computing all rows.
//
// ## Expression Evaluation
//
// Selection vectors are propagated through expression evaluation in the
// [github.com/grafana/loki/v3/pkg/expr] package. When evaluating an
// expression tree, the selection is threaded through all compute function
// calls, ensuring consistent filtering semantics across nested operations.
//
// Package compute is EXPERIMENTAL and currently only intended to be used by
// [github.com/grafana/loki/v3/pkg/dataobj].
package compute

import (
	_ "github.com/grafana/loki/v3/pkg/columnar" // blank import for package doc comment
	_ "github.com/grafana/loki/v3/pkg/memory"   // blank import for package doc comment
)
