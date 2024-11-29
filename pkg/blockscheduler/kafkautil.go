// SPDX-License-Identifier: AGPL-3.0-only

package blockscheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
)

// GetGroupLag is similar to `kadm.Client.Lag` but works when the group doesn't have live participants.
// Similar to `kadm.CalculateGroupLagWithStartOffsets`, it takes into account that the group may not have any commits.
//
// The lag is the difference between the last produced offset (high watermark) and an offset in the "past".
// If the block builder committed an offset for a given partition to the consumer group at least once, then
// the lag is the difference between the last produced offset and the offset committed in the consumer group.
// Otherwise, if the block builder didn't commit an offset for a given partition yet (e.g. block builder is
// running for the first time), then the lag is the difference between the last produced offset and fallbackOffsetMillis.
func GetGroupLag(ctx context.Context, admClient *kadm.Client, topic, group string, fallbackOffsetMillis int64) (kadm.GroupLag, error) {
	offsets, err := admClient.FetchOffsets(ctx, group)
	if err != nil {
		if !errors.Is(err, kerr.GroupIDNotFound) {
			return nil, fmt.Errorf("fetch offsets: %w", err)
		}
	}
	if err := offsets.Error(); err != nil {
		return nil, fmt.Errorf("fetch offsets got error in response: %w", err)
	}

	startOffsets, err := admClient.ListStartOffsets(ctx, topic)
	if err != nil {
		return nil, err
	}
	endOffsets, err := admClient.ListEndOffsets(ctx, topic)
	if err != nil {
		return nil, err
	}

	resolveFallbackOffsets := sync.OnceValues(func() (kadm.ListedOffsets, error) {
		if fallbackOffsetMillis < 0 {
			return nil, fmt.Errorf("cannot resolve fallback offset for value %v", fallbackOffsetMillis)
		}
		return admClient.ListOffsetsAfterMilli(ctx, fallbackOffsetMillis, topic)
	})
	// If the group-partition in offsets doesn't have a commit, fall back depending on where fallbackOffsetMillis points at.
	for topic, pt := range startOffsets.Offsets() {
		for partition, startOffset := range pt {
			if _, ok := offsets.Lookup(topic, partition); ok {
				continue
			}
			fallbackOffsets, err := resolveFallbackOffsets()
			if err != nil {
				return nil, fmt.Errorf("resolve fallback offsets: %w", err)
			}
			o, ok := fallbackOffsets.Lookup(topic, partition)
			if !ok {
				return nil, fmt.Errorf("partition %d not found in fallback offsets for topic %s", partition, topic)
			}
			if o.Offset < startOffset.At {
				// Skip the resolved fallback offset if it's before the partition's start offset (i.e. before the earliest offset of the partition).
				// This should not happen in Kafka, but can happen in Kafka-compatible systems, e.g. Warpstream.
				continue
			}
			offsets.Add(kadm.OffsetResponse{Offset: kadm.Offset{
				Topic:       o.Topic,
				Partition:   o.Partition,
				At:          o.Offset,
				LeaderEpoch: o.LeaderEpoch,
			}})
		}
	}

	descrGroup := kadm.DescribedGroup{
		// "Empty" is the state that indicates that the group doesn't have active consumer members; this is always the case for block-builder,
		// because we don't use group consumption.
		State: "Empty",
	}
	return kadm.CalculateGroupLagWithStartOffsets(descrGroup, offsets, startOffsets, endOffsets), nil
}

const (
	kafkaCommitMetaV1 = 1
)

// commitRecTs: timestamp of the record which was committed (and not the commit time).
func marshallCommitMeta(commitRecTs, lastRecOffset, blockEndTs int64) string {
	return fmt.Sprintf("%d,%d", kafkaCommitMetaV1, commitRecTs)
}

// commitRecTs: timestamp of the record which was committed (and not the commit time).
func unmarshallCommitMeta(s string) (commitRecTs int64, err error) {
	if s == "" {
		return
	}
	var (
		version int
		metaStr string
	)
	_, err = fmt.Sscanf(s, "%d,%s", &version, &metaStr)
	if err != nil {
		return 0, fmt.Errorf("invalid commit metadata format: parse meta version: %w", err)
	}

	if version != kafkaCommitMetaV1 {
		return 0, fmt.Errorf("unsupported commit meta version %d", version)
	}
	_, err = fmt.Sscanf(metaStr, "%d", &commitRecTs)
	if err != nil {
		return 0, fmt.Errorf("invalid commit metadata format: %w", err)
	}
	return
}
