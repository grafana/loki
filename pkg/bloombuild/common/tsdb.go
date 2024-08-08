package common

import (
	"context"
	"fmt"
	"io"
	"math"
	"path"
	"strings"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	baseStore "github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

const (
	gzipExtension = ".gz"
)

type ClosableForSeries interface {
	sharding.ForSeries
	Close() error
}

type TSDBStore interface {
	UsersForPeriod(ctx context.Context, table config.DayTable) ([]string, error)
	ResolveTSDBs(ctx context.Context, table config.DayTable, tenant string) ([]tsdb.SingleTenantTSDBIdentifier, error)
	LoadTSDB(
		ctx context.Context,
		table config.DayTable,
		tenant string,
		id tsdb.Identifier,
	) (ClosableForSeries, error)
}

// BloomTSDBStore is a wrapper around the storage.Client interface which
// implements the TSDBStore interface for this pkg.
type BloomTSDBStore struct {
	storage storage.Client
	logger  log.Logger
}

func NewBloomTSDBStore(storage storage.Client, logger log.Logger) *BloomTSDBStore {
	return &BloomTSDBStore{
		storage: storage,
		logger:  logger,
	}
}

func (b *BloomTSDBStore) UsersForPeriod(ctx context.Context, table config.DayTable) ([]string, error) {
	_, users, err := b.storage.ListFiles(ctx, table.Addr(), true) // bypass cache for ease of testing
	return users, err
}

func (b *BloomTSDBStore) ResolveTSDBs(ctx context.Context, table config.DayTable, tenant string) ([]tsdb.SingleTenantTSDBIdentifier, error) {
	indices, err := b.storage.ListUserFiles(ctx, table.Addr(), tenant, true) // bypass cache for ease of testing
	if err != nil {
		return nil, errors.Wrap(err, "failed to list user files")
	}

	ids := make([]tsdb.SingleTenantTSDBIdentifier, 0, len(indices))
	for _, index := range indices {
		key := index.Name
		if decompress := storage.IsCompressedFile(index.Name); decompress {
			key = strings.TrimSuffix(key, gzipExtension)
		}

		id, ok := tsdb.ParseSingleTenantTSDBPath(path.Base(key))
		if !ok {
			return nil, errors.Errorf("failed to parse single tenant tsdb path: %s", key)
		}

		ids = append(ids, id)

	}
	return ids, nil
}

func (b *BloomTSDBStore) LoadTSDB(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	id tsdb.Identifier,
) (ClosableForSeries, error) {
	withCompression := id.Name() + gzipExtension

	data, err := b.storage.GetUserFile(ctx, table.Addr(), tenant, withCompression)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get file")
	}
	defer data.Close()

	decompressorPool := chunkenc.GetReaderPool(chunkenc.EncGZIP)
	decompressor, err := decompressorPool.GetReader(data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get decompressor")
	}
	defer decompressorPool.PutReader(decompressor)

	buf, err := io.ReadAll(decompressor)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read file")
	}

	reader, err := index.NewReader(index.RealByteSlice(buf))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create index reader")
	}

	idx := tsdb.NewTSDBIndex(reader)

	return idx, nil
}

func NewTSDBSeriesIter(ctx context.Context, user string, f sharding.ForSeries, bounds v1.FingerprintBounds) (iter.Iterator[*v1.Series], error) {
	// TODO(salvacorts): Create a pool
	series := make([]*v1.Series, 0, 100)

	if err := f.ForSeries(
		ctx,
		user,
		bounds,
		0, math.MaxInt64,
		func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
			select {
			case <-ctx.Done():
				return true
			default:
				res := &v1.Series{
					Fingerprint: fp,
					Chunks:      make(v1.ChunkRefs, 0, len(chks)),
				}
				for _, chk := range chks {
					res.Chunks = append(res.Chunks, v1.ChunkRef{
						From:     model.Time(chk.MinTime),
						Through:  model.Time(chk.MaxTime),
						Checksum: chk.Checksum,
					})
				}

				series = append(series, res)
				return false
			}
		},
		labels.MustNewMatcher(labels.MatchEqual, "", ""),
	); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return iter.NewEmptyIter[*v1.Series](), ctx.Err()
	default:
		return iter.NewCancelableIter[*v1.Series](ctx, iter.NewSliceIter[*v1.Series](series)), nil
	}
}

type TSDBStores struct {
	schemaCfg config.SchemaConfig
	stores    []TSDBStore
}

func NewTSDBStores(
	schemaCfg config.SchemaConfig,
	storeCfg baseStore.Config,
	clientMetrics baseStore.ClientMetrics,
	logger log.Logger,
) (*TSDBStores, error) {
	res := &TSDBStores{
		schemaCfg: schemaCfg,
		stores:    make([]TSDBStore, len(schemaCfg.Configs)),
	}

	for i, cfg := range schemaCfg.Configs {
		if cfg.IndexType == types.TSDBType {

			c, err := baseStore.NewObjectClient(cfg.ObjectType, storeCfg, clientMetrics)
			if err != nil {
				return nil, errors.Wrap(err, "failed to create object client")
			}
			res.stores[i] = NewBloomTSDBStore(storage.NewIndexStorageClient(c, cfg.IndexTables.PathPrefix), logger)
		}
	}

	return res, nil
}

func (s *TSDBStores) storeForPeriod(table config.DayTime) (TSDBStore, error) {
	for i := len(s.schemaCfg.Configs) - 1; i >= 0; i-- {
		period := s.schemaCfg.Configs[i]

		if !table.Before(period.From) {
			// we have the desired period config

			if s.stores[i] != nil {
				// valid: it's of tsdb type
				return s.stores[i], nil
			}

			// invalid
			return nil, errors.Errorf(
				"store for period is not of TSDB type (%s) while looking up store for (%v)",
				period.IndexType,
				table,
			)
		}

	}

	return nil, fmt.Errorf(
		"there is no store matching no matching period found for table (%v) -- too early",
		table,
	)
}

func (s *TSDBStores) UsersForPeriod(ctx context.Context, table config.DayTable) ([]string, error) {
	store, err := s.storeForPeriod(table.DayTime)
	if err != nil {
		return nil, err
	}

	return store.UsersForPeriod(ctx, table)
}

func (s *TSDBStores) ResolveTSDBs(ctx context.Context, table config.DayTable, tenant string) ([]tsdb.SingleTenantTSDBIdentifier, error) {
	store, err := s.storeForPeriod(table.DayTime)
	if err != nil {
		return nil, err
	}

	return store.ResolveTSDBs(ctx, table, tenant)
}

func (s *TSDBStores) LoadTSDB(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	id tsdb.Identifier,
) (ClosableForSeries, error) {
	store, err := s.storeForPeriod(table.DayTime)
	if err != nil {
		return nil, err
	}

	return store.LoadTSDB(ctx, table, tenant, id)
}
