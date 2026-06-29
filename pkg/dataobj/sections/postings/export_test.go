package postings

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

// ResolveStreamsAndPointers resolves the streams and pointers for this single postings section.
func (r *Reader) ResolveStreamsAndPointers(ctx context.Context, matchers []*labels.Matcher, start, end time.Time) (*StreamScanResult, error) {
	if len(matchers) == 0 {
		return &StreamScanResult{}, nil
	}
	acc := NewStreamScan(matchers, start, end)
	if _, err := r.ScanLabelsInto(ctx, acc, streamScanBatchSize); err != nil {
		return nil, err
	}
	return acc.Finalize(ctx), nil
}
