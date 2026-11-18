package dataset

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// compiledPredicate is a [Predicate] with its column references resolved to
// indexes into the columns of a [RowReader].
//
// evalRow tests one row. evalRecordBatch tests the rows of a record batch,
// and must pass exactly the rows that evalRow passes.
type compiledPredicate interface {
	// evalRow reports whether row passes the predicate.
	evalRow(row Row) bool

	// evalRecordBatch returns the rows of sel that pass the predicate, for the rows
	// of the record batch rb. sel holds the rows to test, and it selects
	// at least one row.
	//
	// evalRecordBatch reads the columns that it needs when it runs. If earlier
	// predicates keep no row, the reader does not call it, so the later columns
	// stay unread.
	//
	// evalRecordBatch must not modify sel. The result is a subset of sel. It can
	// be sel itself, or use scratch memory that stays valid until the next record
	// batch. If evalRecordBatch returns an error, the caller must ignore the
	// selection it returns.
	evalRecordBatch(ctx context.Context, rb *recordBatch, sel selection) (selection, error)
}

// compilePredicate resolves the column of every leaf in p to its index in
// lookup and returns an equivalent compiledPredicate. It returns an error if
// a leaf references a column absent from lookup, or if a [FuncPredicate] has no
// Keep function.
func compilePredicate(p Predicate, lookup map[Column]int) (compiledPredicate, error) {
	switch p := p.(type) {
	case nil:
		return compiledConstPredicate(true), nil

	case AndPredicate:
		left, err := compilePredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compilePredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return &compiledAndPredicate{left: left, right: right}, nil

	case OrPredicate:
		left, err := compilePredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compilePredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return &compiledOrPredicate{left: left, right: right}, nil

	case NotPredicate:
		inner, err := compilePredicate(p.Inner, lookup)
		if err != nil {
			return nil, err
		}
		return &compiledNotPredicate{inner: inner}, nil

	case TruePredicate:
		return compiledConstPredicate(true), nil

	case FalsePredicate:
		return compiledConstPredicate(false), nil

	case EqualPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return &compiledEqualPredicate{
			columnIndex: idx,
			value:       p.Value,
			kernel:      newKernelComparison(compareEqual, p.Column, p.Value),
		}, nil

	case InPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return &compiledInPredicate{
			columnIndex: idx,
			physical:    p.Column.ColumnDesc().Type.Physical,
			values:      p.Values,
			int64Values: int64ValuesOf(p),
		}, nil

	case GreaterThanPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return &compiledGreaterThanPredicate{
			columnIndex: idx,
			value:       p.Value,
			kernel:      newKernelComparison(compareGreater, p.Column, p.Value),
		}, nil

	case LessThanPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return &compiledLessThanPredicate{
			columnIndex: idx,
			value:       p.Value,
			kernel:      newKernelComparison(compareLess, p.Column, p.Value),
		}, nil

	case GreaterThanOrEqualPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return &compiledGreaterThanOrEqualPredicate{
			columnIndex: idx,
			value:       p.Value,
			kernel:      newKernelComparison(compareGreaterOrEqual, p.Column, p.Value),
		}, nil

	case LessThanOrEqualPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return &compiledLessThanOrEqualPredicate{
			columnIndex: idx,
			value:       p.Value,
			kernel:      newKernelComparison(compareLessOrEqual, p.Column, p.Value),
		}, nil

	case FuncPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		if p.Keep == nil {
			return nil, fmt.Errorf("func predicate on column %q has no Keep function", p.Column.ColumnDesc().Tag)
		}
		return &compiledFuncPredicate{columnIndex: idx, column: p.Column, keep: p.Keep, keepBatch: p.KeepBatch}, nil

	default:
		panic(fmt.Sprintf("dataset.compilePredicate: unsupported predicate type %T", p))
	}
}

func lookupColumnIndex(lookup map[Column]int, c Column) (int, error) {
	idx, ok := lookup[c]
	if !ok {
		return 0, fmt.Errorf("predicate column %v not found in RowReader columns", c)
	}
	return idx, nil
}

type compiledAndPredicate struct {
	left, right compiledPredicate
}

func (p *compiledAndPredicate) evalRow(row Row) bool {
	return p.left.evalRow(row) && p.right.evalRow(row)
}

type compiledOrPredicate struct {
	left, right compiledPredicate
}

func (p *compiledOrPredicate) evalRow(row Row) bool {
	return p.left.evalRow(row) || p.right.evalRow(row)
}

type compiledNotPredicate struct {
	inner compiledPredicate
}

func (p *compiledNotPredicate) evalRow(row Row) bool {
	return !p.inner.evalRow(row)
}

// compiledConstPredicate is a predicate with a fixed result. It represents
// TruePredicate, FalsePredicate, and a nil predicate.
type compiledConstPredicate bool

func (p compiledConstPredicate) evalRow(Row) bool {
	return bool(p)
}

type compiledEqualPredicate struct {
	columnIndex int
	value       Value
	kernel      *kernelComparison
}

func (p *compiledEqualPredicate) evalRow(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) == 0
}

type compiledInPredicate struct {
	columnIndex int
	physical    datasetmd.PhysicalType
	values      ValueSet

	// int64Values is values as an [int64Lookup]. It is nil unless the column is
	// int64 and values can test an int64 without a [Value].
	int64Values int64Lookup
}

// int64Lookup is a [ValueSet] that can test an int64 without building a [Value].
// ContainsInt64(v) must give the same answer as Contains(Int64Value(v)), because
// evalRow calls Contains and evalRecordBatch calls ContainsInt64.
type int64Lookup interface {
	ValueSet
	ContainsInt64(v int64) bool
}

func (p *compiledInPredicate) evalRow(row Row) bool {
	value := row.Values[p.columnIndex]
	if value.IsNil() || value.Type() != p.physical {
		return false
	}
	return p.values.Contains(value)
}

type compiledGreaterThanPredicate struct {
	columnIndex int
	value       Value
	kernel      *kernelComparison
}

func (p *compiledGreaterThanPredicate) evalRow(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) > 0
}

type compiledLessThanPredicate struct {
	columnIndex int
	value       Value
	kernel      *kernelComparison
}

func (p *compiledLessThanPredicate) evalRow(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) < 0
}

type compiledGreaterThanOrEqualPredicate struct {
	columnIndex int
	value       Value
	kernel      *kernelComparison
}

func (p *compiledGreaterThanOrEqualPredicate) evalRow(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) >= 0
}

type compiledLessThanOrEqualPredicate struct {
	columnIndex int
	value       Value
	kernel      *kernelComparison
}

func (p *compiledLessThanOrEqualPredicate) evalRow(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) <= 0
}

type compiledFuncPredicate struct {
	columnIndex int
	column      Column
	keep        func(column Column, value Value) bool
	keepBatch   func(column Column, values *columnar.UTF8, dstIndexes []int32) []int32
}

func (p *compiledFuncPredicate) evalRow(row Row) bool {
	return p.keep(p.column, row.Values[p.columnIndex])
}
