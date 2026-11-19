package index

import (
	"context"
	"fmt"

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
// of one stream. A posting keeps only a time range, a sum of sizes and a set
// of streams, so one observation per run gives the same posting as one
// observation per record.
func (c *labelPostingsCalculation) ProcessBatch(_ context.Context, calcCtx *logsCalculationContext, batch []logs.Record) error {
	for len(batch) > 0 {
		run := streamRun{
			streamID: batch[0].StreamID,
			time:     postings.At(batch[0].Timestamp),
		}
		n := 0
		for ; n < len(batch) && batch[n].StreamID == run.streamID; n++ {
			run.add(batch[n])
		}
		batch = batch[n:]

		if err := c.observeRun(calcCtx, run); err != nil {
			return err
		}
	}
	return nil
}

// streamRun aggregates consecutive records of one stream.
type streamRun struct {
	streamID         int64
	time             postings.TimeRange
	uncompressedSize int64
}

func (r *streamRun) add(record logs.Record) {
	r.time = r.time.Extend(record.Timestamp)
	r.uncompressedSize += record.UncompressedSize()
}

// observeRun observes every label of the run's stream.
func (c *labelPostingsCalculation) observeRun(calcCtx *logsCalculationContext, run streamRun) error {
	streamLbls, labelsOK := calcCtx.streamLabels[run.streamID]
	shardBucket, shardOK := calcCtx.streamShardBuckets[run.streamID]
	if !labelsOK || !shardOK {
		return fmt.Errorf("unknown stream ID %d in log record", run.streamID)
	}

	streamLbls.Range(func(lbl labels.Label) {
		calcCtx.builder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       calcCtx.objectPath,
			ShardBuckets:     int64(calcCtx.shardLayout.Buckets()),
			SectionIndex:     calcCtx.sectionIdx,
			ColumnName:       lbl.Name,
			LabelValue:       lbl.Value,
			StreamID:         run.streamID,
			Time:             run.time,
			UncompressedSize: run.uncompressedSize,
			ShardBucket:      shardBucket,
		})
	})
	return nil
}

func (c *labelPostingsCalculation) Flush(_ context.Context, _ *logsCalculationContext, _ *indexobj.Builder) error {
	return nil
}
