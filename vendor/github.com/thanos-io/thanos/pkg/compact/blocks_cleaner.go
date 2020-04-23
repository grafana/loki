// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package compact

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// BlocksCleaner is a struct that deletes blocks from bucket which are marked for deletion.
type BlocksCleaner struct {
	logger                   log.Logger
	ignoreDeletionMarkFilter *block.IgnoreDeletionMarkFilter
	bkt                      objstore.Bucket
	deleteDelay              time.Duration
	blocksCleaned            prometheus.Counter
	blockCleanupFailures     prometheus.Counter
}

// NewBlocksCleaner creates a new BlocksCleaner.
func NewBlocksCleaner(logger log.Logger, bkt objstore.Bucket, ignoreDeletionMarkFilter *block.IgnoreDeletionMarkFilter, deleteDelay time.Duration, blocksCleaned prometheus.Counter, blockCleanupFailures prometheus.Counter) *BlocksCleaner {
	return &BlocksCleaner{
		logger:                   logger,
		ignoreDeletionMarkFilter: ignoreDeletionMarkFilter,
		bkt:                      bkt,
		deleteDelay:              deleteDelay,
		blocksCleaned:            blocksCleaned,
		blockCleanupFailures:     blockCleanupFailures,
	}
}

// DeleteMarkedBlocks uses ignoreDeletionMarkFilter to gather the blocks that are marked for deletion and deletes those
// if older than given deleteDelay.
func (s *BlocksCleaner) DeleteMarkedBlocks(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "started cleaning of blocks marked for deletion")

	deletionMarkMap := s.ignoreDeletionMarkFilter.DeletionMarkBlocks()
	for _, deletionMark := range deletionMarkMap {
		if time.Since(time.Unix(deletionMark.DeletionTime, 0)).Seconds() > s.deleteDelay.Seconds() {
			if err := block.Delete(ctx, s.logger, s.bkt, deletionMark.ID); err != nil {
				s.blockCleanupFailures.Inc()
				return errors.Wrap(err, "delete block")
			}
			s.blocksCleaned.Inc()
			level.Info(s.logger).Log("msg", "deleted block marked for deletion", "block", deletionMark.ID)
		}
	}

	level.Info(s.logger).Log("msg", "cleaning of blocks marked for deletion done")
	return nil
}
