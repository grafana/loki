package bloomshipper

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
)

type Shipper struct {
	client          Client
	config          config.Config
	logger          log.Logger
	blockDownloader *blockDownloader
}

type Limits interface {
	BloomGatewayBlocksDownloadingParallelism(tenantID string) int
}

func NewShipper(client Client, config config.Config, limits Limits, logger log.Logger, reg prometheus.Registerer) (*Shipper, error) {
	logger = log.With(logger, "component", "bloom-shipper")
	downloader := newBlockDownloader(config, client, limits, logger, reg)
	return &Shipper{
		client:          client,
		config:          config,
		logger:          logger,
		blockDownloader: downloader,
	}, nil
}

func (s *Shipper) ForEachBlock(
	ctx context.Context,
	tenantID string,
	from, through time.Time,
	fingerprints []uint64,
	callback ForEachBlockCallback) error {

	level.Debug(s.logger).Log("msg", "ForEachBlock", "tenant", tenantID, "from", from, "through", through, "fingerprints", len(fingerprints))

	blockRefs, err := s.getActiveBlockRefs(ctx, tenantID, from.UnixNano(), through.UnixNano(), fingerprints)
	if err != nil {
		return fmt.Errorf("error fetching active block references : %w", err)
	}

	cancelContext, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	blocksChannel, errorsChannel := s.blockDownloader.downloadBlocks(cancelContext, tenantID, blockRefs)
	for {
		select {
		case result, ok := <-blocksChannel:
			if !ok {
				return nil
			}
			err = callback(result.BlockQuerier)
			if err != nil {
				return fmt.Errorf("error running callback function for block %s err: %w", result.BlockPath, err)
			}
		case err := <-errorsChannel:
			if err != nil {
				return fmt.Errorf("error downloading blocks : %w", err)
			}
		}
	}
}

func (s *Shipper) Stop() {
	s.client.Stop()
	s.blockDownloader.stop()
}

// getFromThrough returns the first and list item of a fingerprint slice
// It assumes an ascending sorted list of fingerprints.
func getFromThrough(fingerprints []uint64) (uint64, uint64) {
	if len(fingerprints) == 0 {
		return 0, 0
	}
	return fingerprints[0], fingerprints[len(fingerprints)-1]
}

func (s *Shipper) getActiveBlockRefs(
	ctx context.Context,
	tenantID string,
	from, through int64,
	fingerprints []uint64) ([]BlockRef, error) {

	minFingerprint, maxFingerprint := getFromThrough(fingerprints)
	metas, err := s.client.GetMetas(ctx, MetaSearchParams{
		TenantID:       tenantID,
		MinFingerprint: minFingerprint,
		MaxFingerprint: maxFingerprint,
		StartTimestamp: from,
		EndTimestamp:   through,
	})
	if err != nil {
		return []BlockRef{}, fmt.Errorf("error fetching meta.json files: %w", err)
	}
	activeBlocks := s.findBlocks(metas, from, through, fingerprints)
	slices.SortStableFunc(activeBlocks, func(a, b BlockRef) int {
		if a.MinFingerprint < b.MinFingerprint {
			return -1
		}
		if a.MinFingerprint > b.MinFingerprint {
			return 1
		}

		return 0
	})
	return activeBlocks, nil
}

func (s *Shipper) findBlocks(metas []Meta, startTimestamp, endTimestamp int64, fingerprints []uint64) []BlockRef {
	outdatedBlocks := make(map[string]interface{})
	for _, meta := range metas {
		for _, tombstone := range meta.Tombstones {
			outdatedBlocks[tombstone.BlockPath] = nil
		}
	}
	blocksSet := make(map[string]BlockRef)
	for _, meta := range metas {
		for _, block := range meta.Blocks {
			if _, contains := outdatedBlocks[block.BlockPath]; contains {
				continue
			}
			if isOutsideRange(&block, startTimestamp, endTimestamp, fingerprints) {
				continue
			}
			blocksSet[block.BlockPath] = block
		}
	}
	blockRefs := make([]BlockRef, 0, len(blocksSet))
	for _, ref := range blocksSet {
		blockRefs = append(blockRefs, ref)
	}
	return blockRefs
}

// getPosition returns the smallest index of element v in slice s where v > s[i]
// TODO(chaudum): Use binary search to find index instead of iteration.
func getPosition[S ~[]E, E cmp.Ordered](s S, v E) int {
	for i := range s {
		if v > s[i] {
			continue
		}
		return i
	}
	return len(s)
}

func isOutsideRange(b *BlockRef, startTimestamp, endTimestamp int64, fingerprints []uint64) bool {
	// First, check time range
	if b.EndTimestamp < startTimestamp || b.StartTimestamp > endTimestamp {
		return true
	}

	// Then, check if outside of min/max of fingerprint slice
	minFp, maxFp := getFromThrough(fingerprints)
	if b.MaxFingerprint < minFp || b.MinFingerprint > maxFp {
		return true
	}

	// Check if the block range is inside a "gap" in the fingerprint slice
	// e.g.
	// fingerprints = [1, 2,          6, 7, 8]
	// block =              [3, 4, 5]
	idx := getPosition[[]uint64](fingerprints, b.MinFingerprint)
	// in case b.MinFingerprint is outside of the fingerprints range, return true
	// this is already covered in the range check above, but I keep it as a second gate
	if idx > len(fingerprints)-1 {
		return true
	}
	return b.MaxFingerprint < fingerprints[idx]
}
