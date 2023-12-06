package log

import (
	"context"

	"github.com/apache/arrow/go/v14/arrow"
	"github.com/apache/arrow/go/v14/arrow/array"
	"github.com/apache/arrow/go/v14/arrow/compute"
	memmem "github.com/jeschkies/go-memmem/pkg/search"
)

// Stage is a single step of a Pipeline.
// A Stage implementation should never mutate the line passed, but instead either
// return the line unchanged or allocate a new line.
type BatchStage interface {
	Process(batch arrow.Record, column int) arrow.Record
}

type containsFilterBatchStage struct {
	needle []byte
	fb     *array.BooleanBuilder
}

func (f *containsFilterBatchStage) Process(ctx context.Context, batch arrow.Record, column int) (arrow.Record, error) {
	// TODO: check ok
	lines, _ := batch.Column(1).(*array.String)

	mask := f.Filter(lines, f.needle)
	return compute.FilterRecordBatch(ctx, batch, mask, compute.DefaultFilterOptions())
}

func (f *containsFilterBatchStage) Filter(data *array.String, needle []byte) arrow.Array {

	for i := 0; i < data.Len(); i++ {
		beg := data.ValueOffset64(i)
		end := data.ValueOffset64(i + 1)
		offset := memmem.Index(data.ValueBytes()[beg:], []byte(needle))

		// Nothing was found
		if offset == -1 {
			// Fill rest with nulls
			f.fb.AppendNulls(data.Len() - i)
			break
		}

		pos := beg + offset
		// Append nulls until offset is found
		for pos >= end {
			f.fb.AppendNull()
			beg = end
			i++
			end = data.ValueOffset64(i + 1)
		}

		f.fb.Append(true)
	}

	return f.fb.NewArray()
}

type batchSampleExtractor struct {
	LineExtractor
	builder *LabelsBuilder
	stages  []BatchStage
}

func (l *batchSampleExtractor) Process(batch arrow.Record) (arrow.Record, bool) {
	return nil, false
}
