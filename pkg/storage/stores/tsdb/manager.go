package tsdb

import (
	"context"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	shipper_index "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type VersionPrefix int

func (v VersionPrefix) String() string {
	return fmt.Sprintf("v%d", v)
}

func (v VersionPrefix) PathPrefix() string {
	return fmt.Sprintf("tsdb/%s", v.String())
}

const (
	_ VersionPrefix = iota
	V1
)

// nolint:revive
// TSDBManager wraps the index shipper and writes/manages
// TSDB files on  disk
type TSDBManager interface {
	Index
	// Builds a new TSDB file from a set of WALs
	BuildFromWALs(time.Time, []WALIdentifier) error
}

// Identifier can resolve an index to a name (in object storage)
// and a path (on disk)
type Identifier interface {
	Name() string
	Path() string
}

func newPrefixedIdentifier(id Identifier, path, name string) prefixedIdentifier {
	return prefixedIdentifier{
		Identifier: id,
		parentPath: path,
		parentName: name,
	}
}

// parentIdentifier wraps an Identifier and prepends to its methods
type prefixedIdentifier struct {
	parentPath, parentName string
	Identifier
}

func (p prefixedIdentifier) Path() string {
	return filepath.Join(p.parentPath, p.Identifier.Path())
}

func (p prefixedIdentifier) Name() string {
	return path.Join(p.parentName, p.Identifier.Name())
}

/*
tsdbManager is responsible for:
 * Turning WALs into optimized multi-tenant TSDBs when requested
 * Serving reads from these TSDBs
 * Shipping them to remote storage
 * Keeping them available for querying
 * Removing old TSDBs which are no longer needed
*/
type tsdbManager struct {
	indexPeriod time.Duration
	nodeName    string // node name
	log         log.Logger
	dir         string
	metrics     *Metrics

	sync.RWMutex

	shipper indexshipper.IndexShipper
}

func NewTSDBManager(
	nodeName,
	dir string,
	shipper indexshipper.IndexShipper,
	indexPeriod time.Duration,
	logger log.Logger,
	metrics *Metrics,
) TSDBManager {
	return &tsdbManager{
		indexPeriod: indexPeriod,
		nodeName:    nodeName,
		log:         log.With(logger, "component", "tsdb-manager"),
		dir:         dir,
		metrics:     metrics,
		shipper:     shipper,
	}
}

func (m *tsdbManager) BuildFromWALs(t time.Time, ids []WALIdentifier) (err error) {
	level.Debug(m.log).Log("msg", "building WALs", "n", len(ids), "ts", t)
	// get relevant wals
	// iterate them, build tsdb in scratch dir
	defer func() {
		m.metrics.tsdbCreationsTotal.Inc()
		if err != nil {
			m.metrics.tsdbCreationFailures.Inc()
		}
	}()

	level.Debug(m.log).Log("msg", "recovering tenant heads")
	tmp := newTenantHeads(t, defaultHeadManagerStripeSize, m.metrics, m.log)
	if err = recoverHead(m.dir, tmp, ids); err != nil {
		return errors.Wrap(err, "building TSDB from WALs")
	}

	periods := make(map[int]*Builder)

	if err := tmp.forAll(func(user string, ls labels.Labels, chks index.ChunkMetas) {

		// chunks may overlap index period bounds, in which case they're written to multiple
		pds := make(map[int]index.ChunkMetas)
		for _, chk := range chks {
			for _, bucket := range indexBuckets(m.indexPeriod, chk.From(), chk.Through()) {
				pds[bucket] = append(pds[bucket], chk)
			}
		}

		// Add the chunks to all relevant builders
		for pd, matchingChks := range pds {
			b, ok := periods[pd]
			if !ok {
				b = NewBuilder()
				periods[pd] = b
			}

			b.AddSeries(
				ls,
				matchingChks,
			)
		}

	}); err != nil {
		level.Error(m.log).Log("err", err.Error(), "msg", "building TSDB from WALs")
		return err
	}

	for p, b := range periods {

		dstDir := filepath.Join(managerMultitenantDir(m.dir), fmt.Sprint(p))
		dstName := filepath.Join(managerMultitenantDir(V1.PathPrefix()), fmt.Sprint(p))
		dst := newPrefixedIdentifier(
			MultitenantTSDBIdentifier{
				nodeName: m.nodeName,
				ts:       t,
			},
			dstDir,
			dstName,
		)

		level.Debug(m.log).Log("msg", "building tsdb for period", "pd", p, "dst", dst.Path())
		// build+move tsdb to multitenant dir
		start := time.Now()
		_, err = b.Build(
			context.Background(),
			managerScratchDir(m.dir),
			func(from, through model.Time, checksum uint32) Identifier {
				return dst
			},
		)
		if err != nil {
			return err
		}

		level.Debug(m.log).Log("msg", "finished building tsdb for period", "pd", p, "dst", dst.Path(), "duration", time.Since(start))

		loaded, err := NewShippableTSDBFile(dst)
		if err != nil {
			return err
		}

		if err := m.shipper.AddIndex(fmt.Sprintf("%d", p), "", loaded); err != nil {
			return err
		}
	}

	return nil
}

func indexBuckets(indexPeriod time.Duration, from, through model.Time) (res []int) {
	start := from.Time().UnixNano() / int64(indexPeriod)
	end := through.Time().UnixNano() / int64(indexPeriod)
	for cur := start; cur <= end; cur++ {
		res = append(res, int(cur))
	}
	return
}

func (m *tsdbManager) indices(ctx context.Context, from, through model.Time, userIDs ...string) ([]Index, error) {
	var indices []Index
	for _, user := range userIDs {
		for _, bkt := range indexBuckets(m.indexPeriod, from, through) {
			if err := m.shipper.ForEach(ctx, fmt.Sprintf("%d", bkt), user, func(idx shipper_index.Index) error {
				impl, ok := idx.(Index)
				if !ok {
					return fmt.Errorf("unexpected shipper index type: %T", idx)
				}
				indices = append(indices, impl)
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}
	return indices, nil
}

func (m *tsdbManager) indexForTenant(ctx context.Context, from, through model.Time, userID string) (Index, error) {
	// Get all the indices with an empty user id. They're multitenant indices.
	multitenants, err := m.indices(ctx, from, through, "")
	for i, idx := range multitenants {
		multitenants[i] = NewMultiTenantIndex(idx)
	}

	if err != nil {
		return nil, err
	}
	tenant, err := m.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}

	combined := append(multitenants, tenant...)
	if len(combined) == 0 {
		return NoopIndex{}, nil
	}
	return NewMultiIndex(combined...)
}

// TODO(owen-d): how to better implement this?
// setting 0->maxint will force the tsdbmanager to always query
// underlying tsdbs, which is safe, but can we optimize this?
func (m *tsdbManager) Bounds() (model.Time, model.Time) {
	return 0, math.MaxInt64
}

// Close implements Index.Close, but we offload this responsibility
// to the index shipper
func (m *tsdbManager) Close() error {
	return nil
}

func (m *tsdbManager) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	idx, err := m.indexForTenant(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.GetChunkRefs(ctx, userID, from, through, res, shard, matchers...)
}

func (m *tsdbManager) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	idx, err := m.indexForTenant(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.Series(ctx, userID, from, through, res, shard, matchers...)
}

func (m *tsdbManager) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := m.indexForTenant(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.LabelNames(ctx, userID, from, through, matchers...)
}

func (m *tsdbManager) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := m.indexForTenant(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.LabelValues(ctx, userID, from, through, name, matchers...)
}
