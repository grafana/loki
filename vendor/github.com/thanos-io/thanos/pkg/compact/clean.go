// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package compact

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/objstore"
)

const (
	// PartialUploadThresholdAge is a time after partial block is assumed aborted and ready to be cleaned.
	// Keep it long as it is based on block creation time not upload start time.
	PartialUploadThresholdAge = 2 * 24 * time.Hour
)

func BestEffortCleanAbortedPartialUploads(
	ctx context.Context,
	logger log.Logger,
	partial map[ulid.ULID]error,
	bkt objstore.Bucket,
	deleteAttempts prometheus.Counter,
	blocksMarkedForDeletion prometheus.Counter,
) {
	level.Info(logger).Log("msg", "started cleaning of aborted partial uploads")

	// Delete partial blocks that are older than partialUploadThresholdAge.
	// TODO(bwplotka): This is can cause data loss if blocks are:
	// * being uploaded longer than partialUploadThresholdAge
	// * being uploaded and started after their partialUploadThresholdAge
	// can be assumed in this case. Keep partialUploadThresholdAge long for now.
	// Mitigate this by adding ModifiedTime to bkt and check that instead of ULID (block creation time).
	for id := range partial {
		if ulid.Now()-id.Time() <= uint64(PartialUploadThresholdAge/time.Millisecond) {
			// Minimum delay has not expired, ignore for now.
			continue
		}

		deleteAttempts.Inc()
		level.Info(logger).Log("msg", "found partially uploaded block; marking for deletion", "block", id)
		if err := block.MarkForDeletion(ctx, logger, bkt, id, blocksMarkedForDeletion); err != nil {
			level.Warn(logger).Log("msg", "failed to delete aborted partial upload; skipping", "block", id, "thresholdAge", PartialUploadThresholdAge, "err", err)
			return
		}
		level.Info(logger).Log("msg", "deleted aborted partial upload", "block", id, "thresholdAge", PartialUploadThresholdAge)
	}
	level.Info(logger).Log("msg", "cleaning of aborted partial uploads done")
}
