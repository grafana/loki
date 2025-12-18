package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/encoding"
)

type nullPage struct {
	typ    Type
	column int
	count  int
}

func newNullPage(typ Type, columnIndex int16, numValues int32) *nullPage {
	return &nullPage{
		typ:    typ,
		column: int(columnIndex),
		count:  int(numValues),
	}
}

func (page *nullPage) Type() Type                        { return page.typ }
func (page *nullPage) Column() int                       { return page.column }
func (page *nullPage) Dictionary() Dictionary            { return nil }
func (page *nullPage) NumRows() int64                    { return int64(page.count) }
func (page *nullPage) NumValues() int64                  { return int64(page.count) }
func (page *nullPage) NumNulls() int64                   { return int64(page.count) }
func (page *nullPage) Bounds() (min, max Value, ok bool) { return }
func (page *nullPage) Size() int64                       { return 1 }
func (page *nullPage) Values() ValueReader {
	return &nullPageValues{column: page.column, remain: page.count}
}
func (page *nullPage) Slice(i, j int64) Page {
	return &nullPage{column: page.column, count: page.count - int(j-i)}
}
func (page *nullPage) RepetitionLevels() []byte { return nil }
func (page *nullPage) DefinitionLevels() []byte { return nil }
func (page *nullPage) Data() encoding.Values    { return encoding.Values{} }

type nullPageValues struct {
	column int
	remain int
}

func (r *nullPageValues) ReadValues(values []Value) (n int, err error) {
	columnIndex := ^int16(r.column)
	values = values[:min(r.remain, len(values))]
	for i := range values {
		values[i] = Value{columnIndex: columnIndex}
	}
	r.remain -= len(values)
	if r.remain == 0 {
		err = io.EOF
	}
	return len(values), err
}
