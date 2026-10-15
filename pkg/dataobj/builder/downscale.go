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
// permits a downscale when the partition has no records left to consume.
func newOffsetCommittedDownscaleFunc(offsetReader *kafkav2.OffsetReader, partitionID int32, logger log.Logger) downscalePermittedFunc {
	return func(ctx context.Context) (bool, error) {
		startOffset, err := offsetReader.StartOffset(ctx, partitionID)
		if errors.Is(err, kerr.UnknownTopicOrPartition) {
			level.Debug(logger).Log("msg", "partition does not exist, no records to consume", "err", err)
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to get start offset: %w", err)
		}
		endOffset, err := offsetReader.EndOffset(ctx, partitionID)
		if err != nil {
			return false, fmt.Errorf("failed to get end offset: %w", err)
		}
		resumeOffset, err := offsetReader.ResumeOffset(ctx, partitionID)
		if err != nil {
			return false, fmt.Errorf("failed to get resume offset: %w", err)
		}
		recordsLeft := recordsLeftToConsume(startOffset, endOffset, resumeOffset)
		level.Debug(logger).Log(
			"msg", "checked records left to consume",
			"start_offset", startOffset,
			"end_offset", endOffset,
			"resume_offset", resumeOffset,
			"records_left_to_consume", recordsLeft,
		)
		return recordsLeft == 0, nil
	}
}

// recordsLeftToConsume returns the number of records of a partition that its
// consumer group did not consume yet. resumeOffset is the next offset to
// consume, or kafkav2.OffsetStart (-2) if the group never committed.
//
// The group consumes from the start offset if retention deleted the records
// after its committed offset.
func recordsLeftToConsume(startOffset, endOffset, resumeOffset int64) int64 {
	nextOffsetToConsume := max(resumeOffset, startOffset)
	return max(endOffset-nextOffsetToConsume, 0)
}
