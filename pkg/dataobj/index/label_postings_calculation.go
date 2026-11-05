package index

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// created for and scoped to each logs section
type labelPostingsCalculation struct{}

func (c *labelPostingsCalculation) Name() string { return "label_postings" }

// ProcessBatchNeedsBuilderLock reports whether ProcessBatch mutates the shared
// builder. Label postings calls builder.ObserveLabelPosting per stream label,
// which writes into the shared postings builder, so it must run under the
// builder lock.
func (c *labelPostingsCalculation) ProcessBatchNeedsBuilderLock() bool { return true }

func (c *labelPostingsCalculation) Prepare(_ context.Context, _ *logsCalculationContext, _ *dataobj.Section, _ logs.Stats) error {
	return nil
}

// ProcessBatch observes the stream labels once per run of consecutive records
// of one stream, instead of once per record. A posting keeps only the minimum
// and maximum timestamp and the sum of sizes, so the result is the same.
func (c *labelPostingsCalculation) ProcessBatch(_ context.Context, calcCtx *logsCalculationContext, batch []logs.Record) error {
	for len(batch) > 0 {
		streamID := batch[0].StreamID
		run := streamRun{minTimestamp: batch[0].Timestamp, maxTimestamp: batch[0].Timestamp}
		n := 0
		for ; n < len(batch) && batch[n].StreamID == streamID; n++ {
			run.add(batch[n])
		}
		batch = batch[n:]

		if err := observeStreamRun(calcCtx, streamID, run); err != nil {
			return err
		}
	}
	return nil
}

// streamRun aggregates consecutive records of one stream.
type streamRun struct {
	minTimestamp     time.Time
	maxTimestamp     time.Time
	uncompressedSize int64
}

func (r *streamRun) add(record logs.Record) {
	if record.Timestamp.Before(r.minTimestamp) {
		r.minTimestamp = record.Timestamp
	}
	if record.Timestamp.After(r.maxTimestamp) {
		r.maxTimestamp = record.Timestamp
	}
	r.uncompressedSize += record.UncompressedSize()
}

// observeStreamRun observes every label of the stream for run. It observes
// the minimum timestamp with the full size, then the maximum timestamp with
// no size.
func observeStreamRun(calcCtx *logsCalculationContext, streamID int64, run streamRun) error {
	streamLbls, labelsOK := calcCtx.streamLabels[streamID]
	shardBucket, shardOK := calcCtx.streamShardBuckets[streamID]
	if !labelsOK || !shardOK {
		return fmt.Errorf("unknown stream ID %d in log record", streamID)
	}

	streamLbls.Range(func(lbl labels.Label) {
		obs := postings.LabelObservation{
			ObjectPath:       calcCtx.objectPath,
			ShardBuckets:     int64(calcCtx.shardLayout.Buckets()),
			SectionIndex:     calcCtx.sectionIdx,
			ColumnName:       lbl.Name,
			LabelValue:       lbl.Value,
			StreamID:         streamID,
			Timestamp:        run.minTimestamp,
			UncompressedSize: run.uncompressedSize,
			ShardBucket:      shardBucket,
		}
		calcCtx.builder.ObserveLabelPosting(obs)

		// Observe the end timestamp too, so the posting covers the full time
		// range of the run. Use a size of 0 because the first observation
		// already counted the size of the run.
		if !run.maxTimestamp.Equal(run.minTimestamp) {
			obs.Timestamp = run.maxTimestamp
			obs.UncompressedSize = 0
			calcCtx.builder.ObserveLabelPosting(obs)
		}
	})
	return nil
}

func (c *labelPostingsCalculation) Flush(_ context.Context, _ *logsCalculationContext, _ *indexobj.Builder) error {
	return nil
}
