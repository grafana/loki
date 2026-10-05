package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kerr"

	"github.com/grafana/loki/v3/pkg/kafkav2"
)

type downscalePermittedFunc func(context.Context) (bool, error)

// newOffsetCommittedDownscaleFunc returns a downscalePermittedFunc that
// permits a downscale when the consumer has no records left to consume in the
// partition. A record is left to consume when it is after the last committed
// offset and retention has not deleted it.
func newOffsetCommittedDownscaleFunc(offsetReader *kafkav2.OffsetReader, partitionID int32, logger log.Logger) downscalePermittedFunc {
	return func(ctx context.Context) (bool, error) {
		// Read the start offset before the end offset. Both offsets only grow,
		// so the start offset we read is never more than the end offset.
		startOffset, err := offsetReader.StartOffset(ctx, partitionID)
		if errors.Is(err, kerr.UnknownTopicOrPartition) {
			// A missing partition has no records to consume. Kafka can add
			// partitions but cannot remove them, so the partition never had
			// records. This happens when there are more replicas than
			// partitions.
			level.Debug(logger).Log("msg", "partition does not exist, nothing to consume", "err", err)
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to get start offset: %w", err)
		}
		// The end offset is the offset of the next record to be produced.
		endOffset, err := offsetReader.EndOffset(ctx, partitionID)
		if err != nil {
			return false, fmt.Errorf("failed to get end offset: %w", err)
		}
		// The last committed offset is negative if the group never committed.
		lastCommittedOffset, err := offsetReader.LastCommittedOffset(ctx, partitionID)
		if err != nil {
			return false, fmt.Errorf("failed to get last committed offset: %w", err)
		}
		// The consumer commits the offset of the last record it processed, so
		// it resumes at the next offset. If retention deleted that record, or
		// the group never committed, it resumes at the start offset instead.
		nextOffset := max(lastCommittedOffset+1, startOffset)
		isDownscalePermitted := nextOffset >= endOffset
		msg := "no records left to consume"
		if !isDownscalePermitted {
			msg = "there are records left to consume"
		}
		level.Debug(logger).Log(
			"msg", msg,
			"start_offset", startOffset,
			"end_offset", endOffset,
			"last_committed_offset", lastCommittedOffset,
			"remaining", max(endOffset-nextOffset, 0),
		)
		return isDownscalePermitted, nil
	}
}
