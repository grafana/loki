package executor

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/arrowagg"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// batchingPipeline wraps a [Pipeline] and accumulates records from it into
// larger batches of at most batchSize rows, performing schema reconciliation
// across records with different schemas via [arrowagg.Records].
//
// When batchSize <= 0, records are passed through unchanged.
type batchingPipeline struct {
	inner     Pipeline
	batchSize int64
	agg       *arrowagg.Records

	// pending holds a record that was read from inner but would have caused the
	// current batch to exceed batchSize. It is carried over to the next Read call,
	// where it becomes the first record of the next batch.
	pending arrow.RecordBatch
	done    bool // inner pipeline is exhausted
}

var _ WrappedPipeline = (*batchingPipeline)(nil)

// NewBatchingPipeline wraps inner so that each Read call returns a single
// aggregated batch of up to batchSize rows. When batchSize <= 0, records are
// passed through unchanged.
func NewBatchingPipeline(inner Pipeline, batchSize int64) Pipeline {
	return &batchingPipeline{
		inner:     inner,
		batchSize: batchSize,
		agg:       arrowagg.NewRecords(memory.DefaultAllocator),
	}
}

// Open implements Pipeline.
func (p *batchingPipeline) Open(ctx context.Context) error {
	return p.inner.Open(ctx)
}

// Read implements Pipeline.
// It reads from the inner pipeline, accumulating records until batchSize rows
// have been collected or the inner pipeline is exhausted, then returns a single
// aggregated batch. A record that alone exceeds batchSize is still returned
// as its own batch.
func (p *batchingPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if p.batchSize <= 0 {
		return p.inner.Read(ctx)
	}

	if p.done {
		return nil, EOF
	}

	region := xcap.RegionFromContext(ctx)
	var currentCount int64

	// Include any record carried over from the previous Read.
	if p.pending != nil {
		p.agg.Append(p.pending)
		currentCount += p.pending.NumRows()
		p.pending = nil
	}

	for {
		rec, err := p.inner.Read(ctx)
		if errors.Is(err, EOF) {
			p.done = true
			break
		}
		if err != nil {
			return nil, err
		}

		region.Record(xcap.TaskBatchingRecordsReceived.Observe(1))
		region.Record(xcap.TaskBatchingRowsReceived.Observe(rec.NumRows()))

		if rec.NumRows() == 0 {
			continue
		}

		// If adding this record would overflow a non-empty batch, stash it for
		// the next Read and return the current batch now.
		if currentCount > 0 && currentCount+rec.NumRows() > p.batchSize {
			p.pending = rec
			break
		}

		p.agg.Append(rec)
		currentCount += rec.NumRows()

		if currentCount >= p.batchSize {
			break
		}
	}

	if currentCount == 0 {
		return nil, EOF
	}

	combined, err := p.agg.Aggregate()
	if err != nil {
		return nil, err
	}

	region.Record(xcap.TaskBatchingBatchesProduced.Observe(1))
	region.Record(xcap.TaskBatchingRowsWritten.Observe(combined.NumRows()))
	return combined, nil
}

// Close implements Pipeline.
func (p *batchingPipeline) Close() {
	p.inner.Close()
}

// Unwrap implements WrappedPipeline.
func (p *batchingPipeline) Unwrap() Pipeline {
	return p.inner
}
