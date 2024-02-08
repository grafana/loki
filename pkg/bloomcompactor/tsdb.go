package bloomcompactor

import (
	"context"
	"io"
	"math"
	"path"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const (
	gzipExtension = ".gz"
)

type TSDBStore interface {
	UsersForPeriod(ctx context.Context, table string) ([]string, error)
	ResolveTSDBs(ctx context.Context, table, tenant string) ([]tsdb.SingleTenantTSDBIdentifier, error)
	LoadTSDB(
		ctx context.Context,
		table,
		tenant string,
		id tsdb.Identifier,
		bounds v1.FingerprintBounds,
	) (v1.CloseableIterator[*v1.Series], error)
}

// BloomTSDBStore is a wrapper around the storage.Client interface which
// implements the TSDBStore interface for this pkg.
type BloomTSDBStore struct {
	storage storage.Client
}

func NewBloomTSDBStore(storage storage.Client) *BloomTSDBStore {
	return &BloomTSDBStore{
		storage: storage,
	}
}

func (b *BloomTSDBStore) UsersForPeriod(ctx context.Context, table string) ([]string, error) {
	_, users, err := b.storage.ListFiles(ctx, table, false)
	return users, err
}

func (b *BloomTSDBStore) ResolveTSDBs(ctx context.Context, table, tenant string) ([]tsdb.SingleTenantTSDBIdentifier, error) {
	indices, err := b.storage.ListUserFiles(ctx, table, tenant, false)
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
	table,
	tenant string,
	id tsdb.Identifier,
	bounds v1.FingerprintBounds,
) (v1.CloseableIterator[*v1.Series], error) {
	data, err := b.storage.GetUserFile(ctx, table, tenant, id.Name())
	if err != nil {
		return nil, errors.Wrap(err, "failed to get file")
	}

	buf, err := io.ReadAll(data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read file")
	}
	_ = data.Close()

	reader, err := index.NewReader(index.RealByteSlice(buf))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create index reader")
	}

	idx := tsdb.NewTSDBIndex(reader)

	return NewTSDBSeriesIter(ctx, idx, bounds), nil
}

// TSDBStore is an interface for interacting with the TSDB,
// modeled off a relevant subset of the `tsdb.TSDBIndex` struct
type forSeries interface {
	ForSeries(
		ctx context.Context,
		fpFilter index.FingerprintFilter,
		from model.Time,
		through model.Time,
		fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta),
		matchers ...*labels.Matcher,
	) error
	Close() error
}

type TSDBSeriesIter struct {
	f      forSeries
	bounds v1.FingerprintBounds
	ctx    context.Context

	ch          chan *v1.Series
	initialized bool
	next        *v1.Series
	err         error
}

func NewTSDBSeriesIter(ctx context.Context, f forSeries, bounds v1.FingerprintBounds) *TSDBSeriesIter {
	return &TSDBSeriesIter{
		f:      f,
		bounds: bounds,
		ctx:    ctx,
		ch:     make(chan *v1.Series),
	}
}

func (t *TSDBSeriesIter) Next() bool {
	if !t.initialized {
		t.initialized = true
		t.background()
	}

	select {
	case <-t.ctx.Done():
		return false
	case next, ok := <-t.ch:
		t.next = next
		return ok
	}
}

func (t *TSDBSeriesIter) At() *v1.Series {
	return t.next
}

func (t *TSDBSeriesIter) Err() error {
	if t.err != nil {
		return t.err
	}

	return t.ctx.Err()
}

func (t *TSDBSeriesIter) Close() error {
	return t.f.Close()
}

// background iterates over the tsdb file, populating the next
// value via a channel to handle backpressure
func (t *TSDBSeriesIter) background() {
	go func() {
		t.err = t.f.ForSeries(
			t.ctx,
			t.bounds,
			0, math.MaxInt64,
			func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {

				res := &v1.Series{
					Fingerprint: fp,
					Chunks:      make(v1.ChunkRefs, 0, len(chks)),
				}
				for _, chk := range chks {
					res.Chunks = append(res.Chunks, v1.ChunkRef{
						Start:    model.Time(chk.MinTime),
						End:      model.Time(chk.MaxTime),
						Checksum: chk.Checksum,
					})
				}

				select {
				case <-t.ctx.Done():
					return
				case t.ch <- res:
				}
			},
			labels.MustNewMatcher(labels.MatchEqual, "", ""),
		)
		close(t.ch)
	}()
}
