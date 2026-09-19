// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build go1.22

package compute

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/compute/exec"
	"github.com/apache/arrow-go/v18/arrow/compute/internal/kernels"
)

var (
	sortIndicesDoc = FunctionDoc{
		Summary: "Return the indices that would sort the input",
		Description: `This function computes an array of indices that define a stable sort.
Supports arrays, chunked arrays, record batches, and tables.
For arrays and chunked arrays, use a single SortKey (ColumnIndex is ignored).
For record batches and tables, use []SortKey to specify columns and sort
order; at least one key is required. Each key must reference a valid column.`,
		ArgNames:    []string{"input"},
		OptionsType: "SortKeys",
	}

	sortIndicesMetaFunc = NewMetaFunction("sort_indices", Unary(), sortIndicesDoc,
		func(ctx context.Context, opts FunctionOptions, args ...Datum) (Datum, error) {
			input := args[0]
			switch input.Kind() {
			case KindArray, KindChunked, KindRecord, KindTable:
				return sortIndicesImpl(ctx, opts, input)
			}

			return nil, fmt.Errorf("%w: unsupported type for sort_indices operation: %s",
				arrow.ErrNotImplemented, input)
		})

	sortDoc = FunctionDoc{
		Summary: "Return a sorted copy of the input",
		Description: `This function sorts the input using the same ordering as sort_indices
and returns the reordered values. It is equivalent to take(input,
sort_indices(input, options)).
Supports arrays, chunked arrays, record batches, and tables with the same
SortKeys options as sort_indices.`,
		ArgNames:    []string{"input"},
		OptionsType: "SortKeys",
	}

	sortMetaFunc = NewMetaFunction("sort", Unary(), sortDoc,
		func(ctx context.Context, opts FunctionOptions, args ...Datum) (Datum, error) {
			input := args[0]
			switch input.Kind() {
			case KindArray, KindChunked, KindRecord, KindTable:
			default:
				return nil, fmt.Errorf("%w: unsupported type for sort: %s", arrow.ErrNotImplemented, input)
			}

			indices, err := CallFunction(ctx, "sort_indices", opts, input)
			if err != nil {
				return nil, err
			}
			defer indices.Release()

			return Take(ctx, *DefaultTakeOptions(), input, indices)
		})
)

const (
	SortOrderAscending  = kernels.Ascending
	SortOrderDescending = kernels.Descending
	SortNullsAtEnd      = kernels.NullsAtEnd
	SortNullsAtStart    = kernels.NullsAtStart
)

// SortKey defines a column to sort by with its ordering and null placement options.
type SortKey = kernels.SortKey

// SortOptions defines the desired sort order for the input.
type SortOptions []SortKey

// TypeName implements FunctionOptions.
func (SortOptions) TypeName() string { return "SortKeys" }

// DefaultSortKey returns the default sort key: ascending order with nulls last.
func DefaultSortKey() SortKey {
	return SortKey{
		ColumnIndex:   0,
		Order:         kernels.Ascending,
		NullPlacement: kernels.NullsAtEnd,
	}
}

// sortIndicesImpl adapts any supported Datum to kernels.SortIndices (internal/kernels), which
// implements a stable lexicographic sort over []*arrow.Chunked (one logical column per sort key,
// same row count).
//
// Only the columns referenced by sort keys are passed to the kernel; the rest of the batch/table
// is irrelevant to index computation. Chunked wrappers we allocate with arrow.NewChunked must be
// released in the defer below (needsRelease); table column *arrow.Chunked values are borrowed from
// the table and must not be released here.
func sortIndicesImpl(ctx context.Context, opts FunctionOptions, input Datum) (Datum, error) {
	inputSortKeys := opts.(SortOptions)
	if len(inputSortKeys) == 0 {
		return nil, fmt.Errorf("%w: must provide at least one sort key", arrow.ErrInvalid)
	}

	var sortColumns []*arrow.Chunked
	// For KindRecord/KindTable, sortKeys stays aligned with inputSortKeys (multi-column sort).
	// For KindArray/KindChunked, sortKeys is replaced with a single key (see those cases).
	sortKeys := []kernels.SortKey(inputSortKeys)
	var needsRelease []bool

	switch input.Kind() {
	case KindArray:
		// Single column: one Array wrapped as a one-chunk Chunked (kernel API).
		arr := input.(*ArrayDatum).MakeArray()
		defer arr.Release()
		chunked := arrow.NewChunked(arr.DataType(), []arrow.Array{arr})
		sortColumns = []*arrow.Chunked{chunked}
		needsRelease = []bool{true}

		// Only the first key is used; ColumnIndex is meaningless for a bare array—copy the key and
		// set index 0 so the kernel sees a consistent (column, key) pair (order/null placement preserved).
		key := inputSortKeys[0]
		key.ColumnIndex = 0
		sortKeys = []kernels.SortKey{key}

	case KindChunked:
		// Single column: use the Chunked as-is (caller-owned; do not Release).
		chunked := input.(*ChunkedDatum).Value
		sortColumns = []*arrow.Chunked{chunked}
		needsRelease = []bool{false}

		key := inputSortKeys[0]
		key.ColumnIndex = 0
		sortKeys = []kernels.SortKey{key}

	case KindRecord:
		batch := input.(*RecordDatum).Value

		sortColumns = make([]*arrow.Chunked, len(inputSortKeys))
		needsRelease = make([]bool, len(inputSortKeys))
		for i, key := range inputSortKeys {
			if key.ColumnIndex < 0 || int64(key.ColumnIndex) >= batch.NumCols() {
				return nil, fmt.Errorf("%w: sort key %d has invalid column index %d", arrow.ErrInvalid, i, key.ColumnIndex)
			}
			col := batch.Column(key.ColumnIndex)
			// One batch column as a single-chunk Chunked per key; we own these Chunked values.
			sortColumns[i] = arrow.NewChunked(col.DataType(), []arrow.Array{col})
			needsRelease[i] = true
		}

	case KindTable:
		tbl := input.(*TableDatum).Value

		sortColumns = make([]*arrow.Chunked, len(inputSortKeys))
		needsRelease = make([]bool, len(inputSortKeys))
		for i, key := range inputSortKeys {
			if key.ColumnIndex < 0 || int64(key.ColumnIndex) >= tbl.NumCols() {
				return nil, fmt.Errorf("%w: sort key %d has invalid column index %d", arrow.ErrInvalid, i, key.ColumnIndex)
			}
			// Table columns are already Chunked; borrow from the table (do not Release).
			sortColumns[i] = tbl.Column(key.ColumnIndex).Data()
			needsRelease[i] = false
		}

	default:
		return nil, fmt.Errorf("%w: unsupported type for sort_indices operation: %s", arrow.ErrNotImplemented, input)
	}

	defer func() {
		for i, shouldRelease := range needsRelease {
			if shouldRelease {
				sortColumns[i].Release()
			}
		}
	}()

	allocator := exec.GetAllocator(ctx)
	execCtx := &exec.KernelCtx{Ctx: exec.WithAllocator(ctx, allocator)}

	result, err := kernels.SortIndices(execCtx, sortColumns, sortKeys)
	if err != nil {
		return nil, err
	}

	return &ArrayDatum{Value: result.MakeData()}, nil
}

// SortIndices computes the indices that would sort the input.
// For arrays and chunked arrays, pass a single SortKey (ColumnIndex is ignored).
// For record batches and tables, pass []SortKey to specify multi-column sort order.
func SortIndices(ctx context.Context, input Datum, keys SortOptions) (Datum, error) {
	return CallFunction(ctx, "sort_indices", keys, input)
}

// SortIndicesArray computes the indices that would sort the input array.
func SortIndicesArray(ctx context.Context, input arrow.Array, key SortKey) (arrow.Array, error) {
	v := NewDatumWithoutOwning(input)

	indices, err := SortIndices(ctx, v, SortOptions{key})
	if err != nil {
		return nil, err
	}
	defer indices.Release()

	return indices.(*ArrayDatum).MakeArray(), nil
}

// SortIndicesChunked computes the indices that would sort the input chunked array.
func SortIndicesChunked(ctx context.Context, input *arrow.Chunked, key SortKey) (arrow.Array, error) {
	v := NewDatumWithoutOwning(input)

	indices, err := SortIndices(ctx, v, SortOptions{key})
	if err != nil {
		return nil, err
	}
	defer indices.Release()

	return indices.(*ArrayDatum).MakeArray(), nil
}

// SortIndicesRecordBatch computes the indices that would sort the record batch (stable, lexicographic by keys).
func SortIndicesRecordBatch(ctx context.Context, batch arrow.RecordBatch, keys []SortKey) (arrow.Array, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: at least one sort key is required", arrow.ErrInvalid)
	}

	batchDatum := NewDatumWithoutOwning(batch)

	indices, err := SortIndices(ctx, batchDatum, SortOptions(keys))
	if err != nil {
		return nil, err
	}
	defer indices.Release()

	return indices.(*ArrayDatum).MakeArray(), nil
}

// SortIndicesTable computes the indices that would sort the table (stable, lexicographic by keys).
func SortIndicesTable(ctx context.Context, tbl arrow.Table, keys []SortKey) (arrow.Array, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: at least one sort key is required", arrow.ErrInvalid)
	}

	tblDatum := NewDatumWithoutOwning(tbl)

	indices, err := SortIndices(ctx, tblDatum, SortOptions(keys))
	if err != nil {
		return nil, err
	}
	defer indices.Release()

	return indices.(*ArrayDatum).MakeArray(), nil
}

// Sort returns a sorted copy of the input datum by calling the registered "sort" function
// ([SortIndices] then [Take]). Supported kinds are [KindArray], [KindChunked], [KindRecord], and [KindTable].
func Sort(ctx context.Context, input Datum, keys SortOptions) (Datum, error) {
	return CallFunction(ctx, "sort", keys, input)
}

// SortArray returns a sorted copy of the input array.
func SortArray(ctx context.Context, input arrow.Array, key SortKey) (arrow.Array, error) {
	indicesArr, err := SortIndicesArray(ctx, input, key)
	if err != nil {
		return nil, err
	}
	defer indicesArr.Release()

	return TakeArray(ctx, input, indicesArr)
}

// SortChunked returns a sorted copy of the input chunked array.
func SortChunked(ctx context.Context, input *arrow.Chunked, key SortKey) (*arrow.Chunked, error) {
	inputDatum := NewDatumWithoutOwning(input)

	indices, err := SortIndices(ctx, inputDatum, SortOptions{key})
	if err != nil {
		return nil, err
	}
	defer indices.Release()

	resultDatum, err := Take(ctx, *DefaultTakeOptions(), inputDatum, indices)
	if err != nil {
		return nil, err
	}
	defer resultDatum.Release()

	result := resultDatum.(*ChunkedDatum).Value
	result.Retain()
	return result, nil
}

// SortRecordBatch returns a sorted copy of the record batch using lexicographic ordering across the specified columns.
// Each SortKey specifies a column index and its sort order and null placement.
// When multiple keys are provided, ties in earlier columns are broken by later columns.
//
// Example:
//
//	keys := []kernels.SortKey{
//	    {ColumnIndex: 0, Order: kernels.Ascending, NullPlacement: kernels.NullsLast},
//	    {ColumnIndex: 1, Order: kernels.Descending, NullPlacement: kernels.NullsFirst},
//	}
//	sorted, err := compute.SortRecordBatch(ctx, batch, keys)
func SortRecordBatch(ctx context.Context, batch arrow.RecordBatch, keys []kernels.SortKey) (arrow.RecordBatch, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: at least one sort key is required", arrow.ErrInvalid)
	}

	batchDatum := NewDatumWithoutOwning(batch)

	indices, err := SortIndices(ctx, batchDatum, SortOptions(keys))
	if err != nil {
		return nil, err
	}
	defer indices.Release()

	resultDatum, err := Take(ctx, *DefaultTakeOptions(), batchDatum, indices)
	if err != nil {
		return nil, err
	}

	resultBatch := resultDatum.(*RecordDatum).Value
	resultBatch.Retain()
	resultDatum.Release()

	return resultBatch, nil
}

// SortTable returns a sorted copy of the table using lexicographic ordering across the specified columns.
// Each SortKey specifies a column index and its sort order and null placement.
// When multiple keys are provided, ties in earlier columns are broken by later columns.
//
// Example:
//
//	keys := []kernels.SortKey{
//	    {ColumnIndex: 0, Order: kernels.Ascending, NullPlacement: kernels.NullsLast},
//	    {ColumnIndex: 1, Order: kernels.Descending, NullPlacement: kernels.NullsFirst},
//	}
//	sorted, err := compute.SortTable(ctx, table, keys)
func SortTable(ctx context.Context, tbl arrow.Table, keys []kernels.SortKey) (arrow.Table, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: at least one sort key is required", arrow.ErrInvalid)
	}

	tblDatum := NewDatumWithoutOwning(tbl)

	indices, err := SortIndices(ctx, tblDatum, SortOptions(keys))
	if err != nil {
		return nil, err
	}
	defer indices.Release()

	resultDatum, err := Take(ctx, *DefaultTakeOptions(), tblDatum, indices)
	if err != nil {
		return nil, err
	}

	resultTable := resultDatum.(*TableDatum).Value
	resultTable.Retain()
	resultDatum.Release()

	return resultTable, nil
}

// RegisterVectorSort registers the sort_indices and sort functions.
func RegisterVectorSort(reg FunctionRegistry) {
	def := SortOptions{DefaultSortKey()}
	sortIndicesMetaFunc.defaultOpts = def
	sortMetaFunc.defaultOpts = def
	reg.AddFunction(sortIndicesMetaFunc, false)
	reg.AddFunction(sortMetaFunc, false)
}
