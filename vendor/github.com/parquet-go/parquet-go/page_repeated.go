package parquet

import (
	"io"
	"sync/atomic"

	"github.com/parquet-go/parquet-go/encoding"
)

type repeatedPage struct {
	base               Page
	numRows            atomic.Int64 // -1 means the row count has not been computed
	maxRepetitionLevel byte
	maxDefinitionLevel byte
	definitionLevels   []byte
	repetitionLevels   []byte
}

func newRepeatedPage(base Page, maxRepetitionLevel, maxDefinitionLevel byte, repetitionLevels, definitionLevels []byte) *repeatedPage {
	page := &repeatedPage{
		base:               base,
		maxRepetitionLevel: maxRepetitionLevel,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels,
		repetitionLevels:   repetitionLevels,
	}
	page.numRows.Store(-1)
	return page
}

func (page *repeatedPage) Type() Type { return page.base.Type() }

func (page *repeatedPage) Column() int { return page.base.Column() }

func (page *repeatedPage) Dictionary() Dictionary { return page.base.Dictionary() }

func (page *repeatedPage) NumRows() int64 {
	if n := page.numRows.Load(); n >= 0 {
		return n
	}

	n := int64(countLevelsEqual(page.repetitionLevels, 0))
	page.numRows.Store(n)
	return n
}

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

	rowIndex1 := findRowStart(repetitionLevels, i)
	rowIndex2 := len(repetitionLevels)

	numNulls1 := countLevelsNotEqual(definitionLevels[:rowIndex1], maxDefinitionLevel)
	baseI := int64(rowIndex1 - numNulls1)

	var baseJ int64
	if j == numRows {
		baseJ = page.base.NumValues()
	} else {
		rowIndex2 = rowIndex1 + findRowStart(
			repetitionLevels[rowIndex1:],
			j-i,
		)
		numNulls2 := countLevelsNotEqual(
			definitionLevels[rowIndex1:rowIndex2],
			maxDefinitionLevel,
		)
		baseJ = int64(rowIndex2 - (numNulls1 + numNulls2))
	}

	slicedNumRows := j - i
	sliced := &repeatedPage{
		base:               page.base.Slice(baseI, baseJ),
		maxRepetitionLevel: maxRepetitionLevel,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels[rowIndex1:rowIndex2:rowIndex2],
		repetitionLevels:   repetitionLevels[rowIndex1:rowIndex2:rowIndex2],
	}
	sliced.numRows.Store(slicedNumRows)
	return sliced
}

// findRowStart returns the index of the repetition level that starts rowIndex,
// or len(repetitionLevels) when rowIndex is the number of rows.
func findRowStart(repetitionLevels []byte, rowIndex int64) int {
	const blockSize = 256

	offset := 0
	if rowIndex > 0 {
		for len(repetitionLevels) > blockSize {
			block := repetitionLevels[:blockSize]
			numRows := int64(countLevelsEqual(block, 0))
			if numRows > rowIndex {
				repetitionLevels = block
				break
			}
			rowIndex -= numRows
			offset += blockSize
			repetitionLevels = repetitionLevels[blockSize:]
		}
	}

	for i, level := range repetitionLevels {
		if level == 0 {
			if rowIndex == 0 {
				return offset + i
			}
			rowIndex--
		}
	}
	return offset + len(repetitionLevels)
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
	columnIndex := ^uint16(r.page.Column())

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
			j, err := r.values.ReadValues(values[n:i])
			if j == 0 && err == io.EOF {
				// Underlying page exhausted before definitionLevels - corrupted file
				return n, io.EOF
			}
			for ; j > 0; j-- {
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
