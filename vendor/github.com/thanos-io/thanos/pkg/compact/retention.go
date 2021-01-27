// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package compact

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// ApplyRetentionPolicyByResolution removes blocks depending on the specified retentionByResolution based on blocks MaxTime.
// A value of 0 disables the retention for its resolution.
func ApplyRetentionPolicyByResolution(
	ctx context.Context,
	logger log.Logger,
	bkt objstore.Bucket,
	metas map[ulid.ULID]*metadata.Meta,
	retentionByResolution map[ResolutionLevel]time.Duration,
	blocksMarkedForDeletion prometheus.Counter,
) error {
	level.Info(logger).Log("msg", "start optional retention")
	for id, m := range metas {
		retentionDuration := retentionByResolution[ResolutionLevel(m.Thanos.Downsample.Resolution)]
		if retentionDuration.Seconds() == 0 {
			continue
		}

		maxTime := time.Unix(m.MaxTime/1000, 0)
		if time.Now().After(maxTime.Add(retentionDuration)) {
			level.Info(logger).Log("msg", "applying retention: marking block for deletion", "id", id, "maxTime", maxTime.String())
			if err := block.MarkForDeletion(ctx, logger, bkt, id, fmt.Sprintf("block exceeding retention of %v", retentionDuration), blocksMarkedForDeletion); err != nil {
				return errors.Wrap(err, "delete block")
			}
		}
	}
	level.Info(logger).Log("msg", "optional retention apply done")
	return nil
}
