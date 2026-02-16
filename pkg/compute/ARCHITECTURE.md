# Selection Vectors

Selection vectors enable efficient row-level filtering in columnar compute
operations without materializing filtered subsets. This is critical for
performance when processing large datasets with selective filters.

Key benefits of selection vectors:
  - Lazy evaluation: filtered data is not copied or materialized
  - Memory efficiency: unselected rows remain in place
  - Composability: selections can be combined using bitwise operations
  - Consistent null-handling: unselected rows are treated as null

## Selection Vector Representation

Selection vectors are represented as [memory.Bitmap]. Each bit in the bitmap
corresponds to a row in the input data:
  - true bit = row is selected (include in computation)
  - false bit = row is not selected (treat as null)

## Selection Vector Conventions

All rows selected:

When the selection parameter has Len() == 0, all rows are selected. This is
the default behavior and has zero performance overhead compared to operations
without selection support.

	allSelected := memory.Bitmap{} // Len() == 0
	result := compute.Equals(mem, left, right, allSelected) // all rows selected

Selective filtering:

A selection bitmap with Len() > 0 indicates that only rows where the
corresponding bit is true are selected. Unselected rows are treated as null.

	selection := memory.NewBitmap(mem, 10) // 10 rows
	selection.Set(0) // select first row
	selection.Set(5) // select sixth row
	result := compute.Equals(mem, left, right, selection)
	// only rows 0 and 5 are computed, others are null

## Null-Marking Behavior

When a selection vector is applied, unselected rows are marked as null in
the output. This is implemented by ANDing the data's validity bitmap with
the selection bitmap:

	result_validity = data_validity AND selection

This approach has important properties:
  - Already-null rows remain null (null AND true = null)
  - Unselected rows become null (valid AND false = null)
  - Selected valid rows remain valid (valid AND true = valid)
  - No data (array values) copying or array resizing required

## Dispatch Pattern

Compute functions use a dispatch pattern to handle different input
combinations (scalar-scalar, scalar-array, array-scalar, array-array).
Selection vectors are only applied to array results:
  - Scalar-Scalar (SS): No selection (result is scalar)
  - Scalar-Array (SA): Apply selection to result array
  - Array-Scalar (AS): Apply selection to result array
  - Array-Array (AA): Apply selection to result array
