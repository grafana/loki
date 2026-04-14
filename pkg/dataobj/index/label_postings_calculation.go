package index

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/memory"
)

// created for and scoped to each logs section
type labelPostingsCalculation struct {
	postingsByKey map[postingKey]*labelPosting
	maxStreamID   int64
}

type postingKey struct {
	columnName string
	labelValue string
}

type labelPosting struct {
	streamIDBitmap   memory.Bitmap
	minTimestamp     time.Time
	maxTimestamp     time.Time
	uncompressedSize int64
}

func (c *labelPostingsCalculation) Name() string { return "label_postings" }

func (c *labelPostingsCalculation) Prepare(_ context.Context, _ *dataobj.Section, _ logs.Stats) error {
	c.postingsByKey = make(map[postingKey]*labelPosting)
	c.maxStreamID = 0
	return nil
}

func (c *labelPostingsCalculation) ProcessBatch(_ context.Context, calcCtx *logsCalculationContext, batch []logs.Record) error {
	for _, log := range batch {
		streamLbls := calcCtx.streamLabels[log.StreamID]

		if log.StreamID > c.maxStreamID {
			c.maxStreamID = log.StreamID
		}

		// Create postings for every stream label key/value pair
		streamLbls.Range(func(lbl labels.Label) {
			pk := postingKey{columnName: lbl.Name, labelValue: lbl.Value}

			posting, ok := c.postingsByKey[pk]
			if !ok {
				posting = &labelPosting{
					streamIDBitmap: memory.NewBitmap(nil, 0),
					minTimestamp:   log.Timestamp,
					maxTimestamp:   log.Timestamp,
				}
				c.postingsByKey[pk] = posting
			}

			// Grow the bitmap if needed and set the bit for this stream ID.
			if int(log.StreamID) >= posting.streamIDBitmap.Len() {
				posting.streamIDBitmap.Resize(int(log.StreamID) + 1)
			}
			posting.streamIDBitmap.Set(int(log.StreamID), true)

			if log.Timestamp.Before(posting.minTimestamp) {
				posting.minTimestamp = log.Timestamp
			}
			if log.Timestamp.After(posting.maxTimestamp) {
				posting.maxTimestamp = log.Timestamp
			}
			posting.uncompressedSize += int64(len(log.Line))
		})
	}
	return nil
}

func (c *labelPostingsCalculation) Flush(_ context.Context, calcCtx *logsCalculationContext) error {
	if len(c.postingsByKey) == 0 {
		return nil
	}

	// Normalize all bitmaps to the same size.
	targetSize := int(c.maxStreamID) + 1
	for _, posting := range c.postingsByKey {
		if posting.streamIDBitmap.Len() < targetSize {
			posting.streamIDBitmap.Resize(targetSize)
		}
	}

	// Sort postings by [columnName, labelValue] for deterministic output.
	keys := make([]postingKey, 0, len(c.postingsByKey))
	for k := range c.postingsByKey {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b postingKey) int {
		if c := cmp.Compare(a.columnName, b.columnName); c != 0 {
			return c
		}
		return cmp.Compare(a.labelValue, b.labelValue)
	})

	for _, key := range keys {
		posting := c.postingsByKey[key]
		bitmapData, _ := posting.streamIDBitmap.Bytes()

		err := calcCtx.builder.AppendLabelPosting(
			calcCtx.tenantID,
			calcCtx.objectPath,
			calcCtx.sectionIdx,
			key.columnName,
			key.labelValue,
			bitmapData,
			posting.uncompressedSize,
			posting.minTimestamp,
			posting.maxTimestamp,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
