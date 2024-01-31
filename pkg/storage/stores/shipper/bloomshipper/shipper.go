package bloomshipper

import (
	"context"
	"fmt"
	"math"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
)

type Interval struct {
	Start, End model.Time
}

func (i Interval) String() string {
	return fmt.Sprintf("[%s, %s)", i.Start.Time(), i.End.Time())
}

func (i Interval) Cmp(other model.Time) v1.BoundsCheck {
	if other.Before(i.Start) {
		return v1.Before
	} else if other.After(i.End) || other.Equal(i.End) {
		return v1.After
	}
	return v1.Overlap
}

type Keyspace struct {
	Min, Max model.Fingerprint
}

func (r Keyspace) Cmp(other model.Fingerprint) v1.BoundsCheck {
	if other < r.Min {
		return v1.Before
	} else if other > r.Max {
		return v1.After
	}
	return v1.Overlap
}

type BlockQuerierWithFingerprintRange struct {
	*v1.BlockQuerier
	MinFp, MaxFp model.Fingerprint
}

type ForEachBlockCallback func(bq *v1.BlockQuerier, minFp, maxFp uint64) error

type Interface interface {
	GetBlockRefs(ctx context.Context, tenant string, interval Interval) ([]BlockRef, error)
	Fetch(ctx context.Context, tenant string, blocks []BlockRef, callback ForEachBlockCallback) error
	Stop()
}

type Shipper struct {
	store           Store
	config          config.Config
	logger          log.Logger
	blockDownloader *blockDownloader
}

type Limits interface {
	BloomGatewayBlocksDownloadingParallelism(tenantID string) int
}

// TODO(chaudum): resolve and rip out
type StoreAndClient interface {
	Store
	Client
}

func NewShipper(client StoreAndClient, config config.Config, limits Limits, logger log.Logger, reg prometheus.Registerer) (*Shipper, error) {
	logger = log.With(logger, "component", "bloom-shipper")
	downloader, err := newBlockDownloader(config, client, limits, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("error creating block downloader: %w", err)
	}
	return &Shipper{
		store:           client,
		config:          config,
		logger:          logger,
		blockDownloader: downloader,
	}, nil
}

func (s *Shipper) GetBlockRefs(ctx context.Context, tenantID string, interval Interval) ([]BlockRef, error) {
	level.Debug(s.logger).Log("msg", "GetBlockRefs", "tenant", tenantID, "[", interval.Start, "", interval.End)

	// TODO(chaudum): The bloom gateway should not fetch blocks for the complete key space
	keyspaces := []Keyspace{{0, math.MaxUint64}}
	blockRefs, err := s.getActiveBlockRefs(ctx, tenantID, interval, keyspaces)
	if err != nil {
		return nil, fmt.Errorf("error fetching active block references : %w", err)
	}
	return blockRefs, nil
}

func (s *Shipper) Fetch(ctx context.Context, tenantID string, blocks []BlockRef, callback ForEachBlockCallback) error {
	cancelContext, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	blocksChannel, errorsChannel := s.blockDownloader.downloadBlocks(cancelContext, tenantID, blocks)

	// track how many blocks are still remaning to be downloaded
	remaining := len(blocks)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to fetch blocks: %w", ctx.Err())
		case result, sentBeforeClosed := <-blocksChannel:
			if !sentBeforeClosed {
				return nil
			}
			err := runCallback(callback, result)
			if err != nil {
				return err
			}
			remaining--
			if remaining == 0 {
				return nil
			}
		case err := <-errorsChannel:
			return fmt.Errorf("error downloading blocks : %w", err)
		}
	}
}

func runCallback(callback ForEachBlockCallback, block blockWithQuerier) error {
	defer func(b blockWithQuerier) {
		_ = b.Close()
	}(block)

	err := callback(block.closableBlockQuerier.BlockQuerier, block.MinFingerprint, block.MaxFingerprint)
	if err != nil {
		return fmt.Errorf("error running callback function for block %s err: %w", block.BlockPath, err)
	}
	return nil
}

func (s *Shipper) Stop() {
	s.store.Stop()
	s.blockDownloader.stop()
}

// getFirstLast returns the first and last item of a fingerprint slice
// It assumes an ascending sorted list of fingerprints.
func getFirstLast[T any](s []T) (T, T) {
	var zero T
	if len(s) == 0 {
		return zero, zero
	}
	return s[0], s[len(s)-1]
}

func (s *Shipper) getActiveBlockRefs(ctx context.Context, tenantID string, interval Interval, keyspaces []Keyspace) ([]BlockRef, error) {
	minFpRange, maxFpRange := getFirstLast(keyspaces)
	metas, err := s.store.FetchMetas(ctx, MetaSearchParams{
		TenantID: tenantID,
		Keyspace: Keyspace{Min: minFpRange.Min, Max: maxFpRange.Max},
		Interval: interval,
	})
	if err != nil {
		return []BlockRef{}, fmt.Errorf("error fetching meta.json files: %w", err)
	}
	level.Debug(s.logger).Log("msg", "dowloaded metas", "count", len(metas))

	return BlocksForMetas(metas, interval, keyspaces), nil
}

func BlocksForMetas(metas []Meta, interval Interval, keyspaces []Keyspace) []BlockRef {
	tombstones := make(map[string]interface{})
	for _, meta := range metas {
		for _, tombstone := range meta.Tombstones {
			tombstones[tombstone.BlockPath] = nil
		}
	}
	blocksSet := make(map[string]BlockRef)
	for _, meta := range metas {
		for _, block := range meta.Blocks {
			if _, contains := tombstones[block.BlockPath]; contains {
				// skip tombstoned blocks
				continue
			}
			if isOutsideRange(block, interval, keyspaces) {
				// skip block that are outside of interval or keyspaces
				continue
			}
			blocksSet[block.BlockPath] = block
		}
	}
	blockRefs := make([]BlockRef, 0, len(blocksSet))
	for _, ref := range blocksSet {
		blockRefs = append(blockRefs, ref)
	}

	slices.SortStableFunc(blockRefs, func(a, b BlockRef) int {
		if a.MinFingerprint < b.MinFingerprint {
			return -1
		}
		if a.MinFingerprint > b.MinFingerprint {
			return 1
		}

		return 0
	})

	return blockRefs
}

// isOutsideRange tests if a given BlockRef b is outside of search boundaries
// defined by min/max timestamp and min/max fingerprint.
// Fingerprint ranges must be sorted in ascending order.
func isOutsideRange(b BlockRef, interval Interval, keyspaces []Keyspace) bool {
	// check time interval
	if interval.Cmp(b.EndTimestamp) == v1.Before || interval.Cmp(b.StartTimestamp) == v1.After {
		return true
	}

	// check fingerprint ranges
	for _, keyspace := range keyspaces {
		if keyspace.Cmp(model.Fingerprint(b.MinFingerprint)) == v1.Before && keyspace.Cmp(model.Fingerprint(b.MaxFingerprint)) == v1.After {
			return false
		}
		if keyspace.Cmp(model.Fingerprint(b.MinFingerprint)) == v1.Overlap || keyspace.Cmp(model.Fingerprint(b.MaxFingerprint)) == v1.Overlap {
			return false
		}
	}

	return true
}
