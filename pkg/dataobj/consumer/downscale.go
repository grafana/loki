package consumer

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

type downscalePermittedFunc func(context.Context) (bool, error)

// newChainedDownscalePermittedFunc returns a chain of downscalePermittedFunc
// that must all return true for the func to return true.
func newChainedDownscalePermittedFunc(funcs ...downscalePermittedFunc) downscalePermittedFunc {
	return func(ctx context.Context) (bool, error) {
		for _, f := range funcs {
			if ok, err := f(ctx); err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
}

// newOffsetCommittedDownscaleFunc returns a downscalePermittedFunc that checks
// if the consumer has committed all records up to the end offset.
func newOffsetCommittedDownscaleFunc(offsetManager *partition.KafkaOffsetManager, partitionID int32, logger log.Logger) downscalePermittedFunc {
	return func(ctx context.Context) (bool, error) {
		endOffset, err := offsetManager.PartitionOffset(ctx, partitionID, partition.KafkaEndOffset)
		if err != nil {
			return false, fmt.Errorf("failed to get end offset: %w", err)
		}
		// The end offset is the offset of the next record to be produced.
		// That means if the end offset is zero no records have been produced
		// for this partition, which in turn means we can downscale.
		if endOffset == 0 {
			level.Debug(logger).Log("msg", "no records produced for partition")
			return true, nil
		}
		// If some records have been produced for this partition we need to
		// make sure the consumer has processed and committed all of them
		// otherwise we risk data loss.
		lastCommittedOffset, err := offsetManager.LastCommittedOffset(ctx, partitionID)
		if err != nil {
			return false, fmt.Errorf("failed to get last committed offset: %w", err)
		}
		// The end offset is the offset of the next record, so we need to
		// subtract one to get the offset of last record.
		isDownscalePermitted := lastCommittedOffset == endOffset-1
		if isDownscalePermitted {
			level.Debug(logger).Log(
				"msg",
				"all offsets have been committed",
				"last_committed_offset",
				lastCommittedOffset,
				"end_offset",
				endOffset,
			)
		} else {
			level.Debug(logger).Log(
				"msg",
				"there are uncommitted offsets",
				"last_committed_offset",
				lastCommittedOffset,
				"end_offset",
				endOffset,
				"delta",
				endOffset-lastCommittedOffset-1,
			)
		}
		return isDownscalePermitted, nil
	}
}
