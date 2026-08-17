package index

import (
	"context"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
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

func (c *labelPostingsCalculation) ProcessBatch(_ context.Context, calcCtx *logsCalculationContext, batch []logs.Record) error {
	var batchErr error
	for _, log := range batch {
		if batchErr != nil {
			break
		}
		streamLbls := calcCtx.streamLabels[log.StreamID]
		// The uncompressed byte contract is line bytes plus structured metadata
		// value bytes, matching streams.Stream.UncompressedSize and the stats
		// calculation so every producer reports the same quantity.
		uncompressedSize := int64(len(log.Line))
		log.Metadata.Range(func(md labels.Label) {
			uncompressedSize += int64(len(md.Value))
		})
		streamLbls.Range(func(lbl labels.Label) {
			if batchErr != nil {
				return
			}
			calcCtx.builder.ObserveLabelPosting(calcCtx.tenantID, postings.LabelObservation{
				ObjectPath:       calcCtx.objectPath,
				SectionIndex:     calcCtx.sectionIdx,
				ColumnName:       lbl.Name,
				LabelValue:       lbl.Value,
				StreamID:         log.StreamID,
				Timestamp:        log.Timestamp,
				UncompressedSize: uncompressedSize,
			})
		})
	}
	return batchErr
}

func (c *labelPostingsCalculation) Flush(_ context.Context, _ *logsCalculationContext) error {
	return nil
}
