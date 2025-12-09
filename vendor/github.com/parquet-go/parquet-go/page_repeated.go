package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/encoding"
)

type repeatedPage struct {
	base               Page
	maxRepetitionLevel byte
	maxDefinitionLevel byte
	definitionLevels   []byte
	repetitionLevels   []byte
}

func newRepeatedPage(base Page, maxRepetitionLevel, maxDefinitionLevel byte, repetitionLevels, definitionLevels []byte) *repeatedPage {
	return &repeatedPage{
		base:               base,
		maxRepetitionLevel: maxRepetitionLevel,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels,
		repetitionLevels:   repetitionLevels,
	}
}

func (page *repeatedPage) Type() Type { return page.base.Type() }

func (page *repeatedPage) Column() int { return page.base.Column() }

func (page *repeatedPage) Dictionary() Dictionary { return page.base.Dictionary() }

func (page *repeatedPage) NumRows() int64 { return int64(countLevelsEqual(page.repetitionLevels, 0)) }

func (page *repeatedPage) NumValues() int64 { return int64(len(page.definitionLevels)) }

func (page *repeatedPage) NumNulls() int64 {
	return int64(countLevelsNotEqual(page.definitionLevels, page.maxDefinitionLevel))
}

func (page *repeatedPage) Bounds() (min, max Value, ok bool) { return page.base.Bounds() }

func (page *repeatedPage) Size() int64 {
	return int64(len(page.repetitionLevels)) + int64(len(page.definitionLevels)) + page.base.Size()
}

func (page *repeatedPage) RepetitionLevels() []byte { return page.repetitionLevels }

func (page *repeatedPage) DefinitionLevels() []byte { return page.definitionLevels }

func (page *repeatedPage) Data() encoding.Values { return page.base.Data() }

func (page *repeatedPage) Values() ValueReader {
	return &repeatedPageValues{
		page:   page,
		values: page.base.Values(),
	}
}

func (page *repeatedPage) Slice(i, j int64) Page {
	numRows := page.NumRows()
	if i < 0 || i > numRows {
		panic(errPageBoundsOutOfRange(i, j, numRows))
	}
	if j < 0 || j > numRows {
		panic(errPageBoundsOutOfRange(i, j, numRows))
	}
	if i > j {
		panic(errPageBoundsOutOfRange(i, j, numRows))
	}

	maxRepetitionLevel := page.maxRepetitionLevel
	maxDefinitionLevel := page.maxDefinitionLevel
	repetitionLevels := page.repetitionLevels
	definitionLevels := page.definitionLevels

	rowIndex0 := 0
	rowIndex1 := len(repetitionLevels)
	rowIndex2 := len(repetitionLevels)

	for k, def := range repetitionLevels {
		if def == 0 {
			if rowIndex0 == int(i) {
				rowIndex1 = k
				break
			}
			rowIndex0++
		}
	}

	for k, def := range repetitionLevels[rowIndex1:] {
		if def == 0 {
			if rowIndex0 == int(j) {
				rowIndex2 = rowIndex1 + k
				break
			}
			rowIndex0++
		}
	}

	numNulls1 := countLevelsNotEqual(definitionLevels[:rowIndex1], maxDefinitionLevel)
	numNulls2 := countLevelsNotEqual(definitionLevels[rowIndex1:rowIndex2], maxDefinitionLevel)

	i = int64(rowIndex1 - numNulls1)
	j = int64(rowIndex2 - (numNulls1 + numNulls2))

	return newRepeatedPage(
		page.base.Slice(i, j),
		maxRepetitionLevel,
		maxDefinitionLevel,
		repetitionLevels[rowIndex1:rowIndex2:rowIndex2],
		definitionLevels[rowIndex1:rowIndex2:rowIndex2],
	)
}

type repeatedPageValues struct {
	page   *repeatedPage
	values ValueReader
	offset int
}

func (r *repeatedPageValues) ReadValues(values []Value) (n int, err error) {
	maxDefinitionLevel := r.page.maxDefinitionLevel
	definitionLevels := r.page.definitionLevels
	repetitionLevels := r.page.repetitionLevels
	columnIndex := ^int16(r.page.Column())

	// While we haven't exceeded the output buffer and we haven't exceeded the page size.
	for n < len(values) && r.offset < len(definitionLevels) {

		// While we haven't exceeded the output buffer and we haven't exceeded the
		// page size AND the current element's definitionLevel is not the
		// maxDefinitionLevel (this is a null value), Create the zero values to be
		// returned in this run.
		for n < len(values) && r.offset < len(definitionLevels) && definitionLevels[r.offset] != maxDefinitionLevel {
			values[n] = Value{
				repetitionLevel: repetitionLevels[r.offset],
				definitionLevel: definitionLevels[r.offset],
				columnIndex:     columnIndex,
			}
			r.offset++
			n++
		}

		i := n
		j := r.offset
		// Get the length of the run of non-zero values to be copied.
		for i < len(values) && j < len(definitionLevels) && definitionLevels[j] == maxDefinitionLevel {
			i++
			j++
		}

		// Copy all the non-zero values in this run.
		if n < i {
			for j, err = r.values.ReadValues(values[n:i]); j > 0; j-- {
				values[n].repetitionLevel = repetitionLevels[r.offset]
				values[n].definitionLevel = maxDefinitionLevel
				r.offset++
				n++
			}
			if err != nil && err != io.EOF {
				return n, err
			}
			err = nil
		}
	}

	if r.offset == len(definitionLevels) {
		err = io.EOF
	}
	return n, err
}
