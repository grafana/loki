package querier

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/store/hintspb"
)

type BlocksConsistencyChecker struct {
	uploadGracePeriod   time.Duration
	deletionGracePeriod time.Duration
	logger              log.Logger

	checksTotal  prometheus.Counter
	checksFailed prometheus.Counter
}

func NewBlocksConsistencyChecker(uploadGracePeriod, deletionGracePeriod time.Duration, logger log.Logger, reg prometheus.Registerer) *BlocksConsistencyChecker {
	return &BlocksConsistencyChecker{
		uploadGracePeriod:   uploadGracePeriod,
		deletionGracePeriod: deletionGracePeriod,
		logger:              logger,
		checksTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_querier_blocks_consistency_checks_total",
			Help: "Total number of consistency checks run on queried blocks.",
		}),
		checksFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_querier_blocks_consistency_checks_failed_total",
			Help: "Total number of consistency checks failed on queried blocks.",
		}),
	}
}

func (c *BlocksConsistencyChecker) Check(expectedBlocks []*BlockMeta, knownDeletionMarks map[ulid.ULID]*metadata.DeletionMark, queriedBlocks map[string][]hintspb.Block) error {
	c.checksTotal.Inc()

	// Reverse the map of queried blocks, so that we can easily look for missing ones
	// while keeping the information about which store-gateways have already been queried
	// for that block.
	actualBlocks := map[string][]string{}
	for gatewayAddr, blocks := range queriedBlocks {
		for _, b := range blocks {
			actualBlocks[b.Id] = append(actualBlocks[b.Id], gatewayAddr)
		}
	}

	// Look for any missing block.
	missingBlocks := map[string][]string{}
	var missingBlockIDs []string

	for _, meta := range expectedBlocks {
		// Some recently uploaded blocks, already discovered by the querier, may not have been discovered
		// and loaded by the store-gateway yet. In order to avoid false positives, we grant some time
		// to the store-gateway to discover them. It's safe to exclude recently uploaded blocks because:
		// - Blocks uploaded by ingesters: we will continue querying them from ingesters for a while (depends
		//   on the configured retention period).
		// - Blocks uploaded by compactor: the source blocks are marked for deletion but will continue to be
		//   queried by queriers for a while (depends on the configured deletion marks delay).
		if c.uploadGracePeriod > 0 && time.Since(meta.UploadedAt) < c.uploadGracePeriod {
			level.Debug(c.logger).Log("msg", "block skipped from consistency check because it was uploaded recently", "block", meta.ULID.String(), "uploadedAt", meta.UploadedAt.String())
			continue
		}

		// The store-gateway may offload blocks before the querier. If that happens, the querier will run a consistency check
		// on blocks that can't be queried because they were offloaded. For this reason, we don't run the consistency check on any block
		// which has been marked for deletion more then "grace period" time ago. Basically, the grace period is the time
		// we still expect a block marked for deletion to be still queried.
		if mark := knownDeletionMarks[meta.ULID]; mark != nil {
			deletionTime := time.Unix(mark.DeletionTime, 0)

			if c.deletionGracePeriod > 0 && time.Since(deletionTime) > c.deletionGracePeriod {
				level.Debug(c.logger).Log("msg", "block skipped from consistency check because it is marked for deletion", "block", meta.ULID.String(), "deletionTime", deletionTime.String())
				continue
			}
		}

		id := meta.ULID.String()
		if gatewayAddrs, ok := actualBlocks[id]; !ok {
			missingBlocks[id] = gatewayAddrs
			missingBlockIDs = append(missingBlockIDs, id)
		}
	}

	if len(missingBlocks) == 0 {
		return nil
	}

	c.checksFailed.Inc()
	return fmt.Errorf("consistency check failed because some blocks were not queried: %s", strings.Join(missingBlockIDs, " "))
}
