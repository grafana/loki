package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Merge is a pipeline that takes N inputs and sequentially consumes them.
// When bufferBatchCount > 0 it fills one batch from the current input; when that input
// returns EOF before the batch is full it advances to the next input and keeps filling
// the same batch (batch across inputs). Empty records are skipped.
type Merge struct {
	inputs           []Pipeline
	currInput        int // index of the currently processed input
	region           *xcap.Region
	bufferBatchCount int // when > 0, buffer up to this many records (from one or more inputs) then return
}

var _ Pipeline = (*Merge)(nil)

// newMergePipeline creates a new merge pipeline that merges N inputs into a single output.
// It reads directly from inputs (no prefetch). When bufferBatchCount > 0, the Merge fills
// one batch by reading one input at a time; if the batch becomes full it is emitted and
// reading continues from the same input; if the current input returns EOF before the
// batch is full, the Merge advances to the next input and keeps filling the same batch.
// Empty records (NumRows() == 0) are skipped and never returned.
func newMergePipeline(inputs []Pipeline, bufferBatchCount int, region *xcap.Region) (*Merge, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("merge pipeline: no inputs provided")
	}
	return &Merge{
		inputs:           inputs,
		region:           region,
		bufferBatchCount: bufferBatchCount,
	}, nil
}

// Read reads the next batch from the pipeline.
// It returns an error if reading fails or when the pipeline is exhausted.
func (m *Merge) Read(ctx context.Context) (arrow.RecordBatch, error) {
	return m.read(ctx)
}

func (m *Merge) read(ctx context.Context) (arrow.RecordBatch, error) {
	if m.bufferBatchCount <= 0 {
		return m.readOneAtATime(ctx)
	}
	return m.readBufferedAcrossInputs(ctx)
}

// readOneAtATime returns one record at a time from the current input until EOF, then advances.
// Empty records are skipped.
func (m *Merge) readOneAtATime(ctx context.Context) (arrow.RecordBatch, error) {
	for m.currInput < len(m.inputs) {
		input := m.inputs[m.currInput]
		rec, err := input.Read(ctx)
		if err != nil {
			if errors.Is(err, EOF) {
				input.Close()
				m.currInput++
				continue
			}
			return nil, err
		}
		if rec.NumRows() == 0 {
			rec.Release()
			continue
		}
		if m.region != nil {
			m.region.Record(xcap.TaskMergeRecordsConsumed.Observe(1))
			m.region.Record(xcap.TaskMergeRowsConsumed.Observe(rec.NumRows()))
			m.region.Record(xcap.TaskMergeBatchesProduced.Observe(1))
		}
		return rec, nil
	}
	return nil, EOF
}

// readBufferedAcrossInputs fills a buffer from the current input; when full returns that batch
// (stays on same input). When current input returns EOF before buffer is full, advances to
// next input and keeps filling. When all inputs exhausted, returns partial batch or EOF.
// Empty records are skipped.
func (m *Merge) readBufferedAcrossInputs(ctx context.Context) (arrow.RecordBatch, error) {
	var records []arrow.RecordBatch
	for m.currInput < len(m.inputs) {
		input := m.inputs[m.currInput]
		rec, err := input.Read(ctx)
		if err != nil {
			if errors.Is(err, EOF) {
				input.Close()
				m.currInput++
				continue
			}
			for _, r := range records {
				r.Release()
			}
			return nil, err
		}
		if rec.NumRows() == 0 {
			rec.Release()
			continue
		}
		if m.region != nil {
			m.region.Record(xcap.TaskMergeRecordsConsumed.Observe(1))
			m.region.Record(xcap.TaskMergeRowsConsumed.Observe(rec.NumRows()))
		}
		records = append(records, rec)
		if len(records) >= m.bufferBatchCount {
			if m.region != nil {
				m.region.Record(xcap.TaskMergeBatchesProduced.Observe(1))
			}

			if len(records) == 1 {
				return records[0], nil
			}

			batch, err := concatenateRecords(records)
			for _, r := range records {
				r.Release()
			}
			if err != nil {
				return nil, err
			}

			return batch, nil
		}
	}

	// All inputs exhausted
	if len(records) == 0 {
		return nil, EOF
	}

	if m.region != nil {
		m.region.Record(xcap.TaskMergeBatchesProduced.Observe(1))
	}

	if len(records) == 1 {
		return records[0], nil
	}

	batch, err := concatenateRecords(records)
	for _, r := range records {
		r.Release()
	}
	if err != nil {
		return nil, err
	}

	return batch, nil
}

// concatenateRecords concatenates the given records into a single RecordBatch.
// The schema of the first record is used. Caller must release the input records.
func concatenateRecords(records []arrow.RecordBatch) (arrow.RecordBatch, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("need at least one record")
	}
	schema := records[0].Schema()
	numCols := schema.NumFields()
	concatenatedCols := make([]arrow.Array, numCols)
	for col := 0; col < numCols; col++ {
		colArrays := make([]arrow.Array, len(records))
		for i, r := range records {
			colArrays[i] = r.Column(col)
		}
		concat, err := array.Concatenate(colArrays, memory.DefaultAllocator)
		if err != nil {
			for j := 0; j < col; j++ {
				concatenatedCols[j].Release()
			}
			return nil, err
		}
		concatenatedCols[col] = concat
	}
	var totalRows int64
	for _, r := range records {
		totalRows += r.NumRows()
	}
	return array.NewRecordBatch(schema, concatenatedCols, totalRows), nil
}

// Close implements Pipeline.
func (m *Merge) Close() {
	if m.region != nil {
		m.region.End()
	}
	for _, input := range m.inputs[m.currInput:] {
		input.Close()
	}
}

// Region implements RegionProvider.
func (m *Merge) Region() *xcap.Region {
	return m.region
}
