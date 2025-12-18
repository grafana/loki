package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/encoding"
)

type optionalPage struct {
	base               Page
	maxDefinitionLevel byte
	definitionLevels   []byte
}

func newOptionalPage(base Page, maxDefinitionLevel byte, definitionLevels []byte) *optionalPage {
	return &optionalPage{
		base:               base,
		maxDefinitionLevel: maxDefinitionLevel,
		definitionLevels:   definitionLevels,
	}
}

func (page *optionalPage) Type() Type { return page.base.Type() }

func (page *optionalPage) Column() int { return page.base.Column() }

func (page *optionalPage) Dictionary() Dictionary { return page.base.Dictionary() }

func (page *optionalPage) NumRows() int64 { return int64(len(page.definitionLevels)) }

func (page *optionalPage) NumValues() int64 { return int64(len(page.definitionLevels)) }

func (page *optionalPage) NumNulls() int64 {
	return int64(countLevelsNotEqual(page.definitionLevels, page.maxDefinitionLevel))
}

func (page *optionalPage) Bounds() (min, max Value, ok bool) { return page.base.Bounds() }

func (page *optionalPage) Size() int64 { return int64(len(page.definitionLevels)) + page.base.Size() }

func (page *optionalPage) RepetitionLevels() []byte { return nil }

func (page *optionalPage) DefinitionLevels() []byte { return page.definitionLevels }

func (page *optionalPage) Data() encoding.Values { return page.base.Data() }

func (page *optionalPage) Values() ValueReader {
	return &optionalPageValues{
		page:   page,
		values: page.base.Values(),
	}
}

func (page *optionalPage) Slice(i, j int64) Page {
	maxDefinitionLevel := page.maxDefinitionLevel
	definitionLevels := page.definitionLevels
	numNulls1 := int64(countLevelsNotEqual(definitionLevels[:i], maxDefinitionLevel))
	numNulls2 := int64(countLevelsNotEqual(definitionLevels[i:j], maxDefinitionLevel))
	return newOptionalPage(
		page.base.Slice(i-numNulls1, j-(numNulls1+numNulls2)),
		maxDefinitionLevel,
		definitionLevels[i:j:j],
	)
}

type optionalPageValues struct {
	page   *optionalPage
	values ValueReader
	offset int
}

func (r *optionalPageValues) ReadValues(values []Value) (n int, err error) {
	maxDefinitionLevel := r.page.maxDefinitionLevel
	definitionLevels := r.page.definitionLevels
	columnIndex := ^int16(r.page.Column())

	for n < len(values) && r.offset < len(definitionLevels) {
		for n < len(values) && r.offset < len(definitionLevels) && definitionLevels[r.offset] != maxDefinitionLevel {
			values[n] = Value{
				definitionLevel: definitionLevels[r.offset],
				columnIndex:     columnIndex,
			}
			r.offset++
			n++
		}

		i := n
		j := r.offset
		for i < len(values) && j < len(definitionLevels) && definitionLevels[j] == maxDefinitionLevel {
			i++
			j++
		}

		if n < i {
			for j, err = r.values.ReadValues(values[n:i]); j > 0; j-- {
				values[n].definitionLevel = maxDefinitionLevel
				r.offset++
				n++
			}
			// Do not return on an io.EOF here as we may still have null values to read.
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
