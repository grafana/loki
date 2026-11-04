package dataset

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

// retainedColumn keeps the values of one column for the rows that passed the early predicates, in
// row order. It stores numbers as plain integers and binary values in one block. A row costs a nil
// flag and eight bytes, plus the binary data, instead of a [Value].
type retainedColumn struct {
	index   int                    // Index of the column in Row.Values.
	kind    datasetmd.PhysicalType // Physical type of the column.
	nulls   []bool                 // Whether each value is nil.
	numbers []uint64               // For a numeric column, the value of each row.
	ends    []int                  // For a binary column, where each value ends in data.
	data    []byte                 // For a binary column, the values one after another.
}

// newRetainedColumn returns an empty retainedColumn. It reports false when the physical type is
// not one that it can store.
func newRetainedColumn(index int, kind datasetmd.PhysicalType) (retainedColumn, bool) {
	switch kind {
	case datasetmd.PHYSICAL_TYPE_INT64, datasetmd.PHYSICAL_TYPE_UINT64, datasetmd.PHYSICAL_TYPE_BINARY:
		return retainedColumn{index: index, kind: kind}, true
	}
	return retainedColumn{}, false
}

// add appends the value of the next row.
func (c *retainedColumn) add(v *Value) {
	null := v.IsNil()
	c.nulls = append(c.nulls, null)

	switch c.kind {
	case datasetmd.PHYSICAL_TYPE_INT64:
		c.numbers = append(c.numbers, uint64(v.int64()))
	case datasetmd.PHYSICAL_TYPE_UINT64:
		c.numbers = append(c.numbers, v.uint64())
	case datasetmd.PHYSICAL_TYPE_BINARY:
		if !null {
			c.data = append(c.data, v.Binary()...)
		}
		c.ends = append(c.ends, len(c.data))
	}
}

// restore sets dst to the value of row i. A binary value goes into the buffer of dst, so dst never
// aliases the retained data.
func (c *retainedColumn) restore(i int, dst *Value) {
	if c.nulls[i] {
		dst.Zero()
		return
	}

	switch c.kind {
	case datasetmd.PHYSICAL_TYPE_INT64:
		*dst = Int64Value(int64(c.numbers[i]))
	case datasetmd.PHYSICAL_TYPE_UINT64:
		*dst = Uint64Value(c.numbers[i])
	case datasetmd.PHYSICAL_TYPE_BINARY:
		var start int
		if i > 0 {
			start = c.ends[i-1]
		}
		src := c.data[start:c.ends[i]]
		buf := slicegrow.GrowToCap(dst.Buffer(), len(src))[:len(src)]
		copy(buf, src)
		*dst = BinaryValue(buf)
	}
}

// size returns the bytes of values that the column holds.
func (c *retainedColumn) size() int {
	return len(c.nulls) + 8*len(c.numbers) + 8*len(c.ends) + len(c.data)
}

// retainedValues keeps the values of several columns for the rows that passed the early
// predicates. It counts the rows, so a caller can check that it holds one entry per survivor.
type retainedValues struct {
	columns []retainedColumn
	rows    int // Number of rows added.
}

// add appends the retained columns of each row.
func (rv *retainedValues) add(rows []Row) {
	for c := range rv.columns {
		col := &rv.columns[c]
		for i := range rows {
			col.add(&rows[i].Values[col.index])
		}
	}
	rv.rows += len(rows)
}

// restore sets the retained columns of rows. Row i gets the values of the row that was added at
// position first+i.
func (rv *retainedValues) restore(first int, rows []Row) {
	for c := range rv.columns {
		col := &rv.columns[c]
		for i := range rows {
			col.restore(first+i, &rows[i].Values[col.index])
		}
	}
}

// size returns the bytes of values that the retained columns hold.
func (rv *retainedValues) size() int {
	var total int
	for c := range rv.columns {
		total += rv.columns[c].size()
	}
	return total
}
