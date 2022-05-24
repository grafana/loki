package tsdb

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	shipper_index "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

// nolint:revive
// TSDBManager wraps the index shipper and writes/manages
// TSDB files on  disk
type TSDBManager interface {
	Start() error
	Index
	// Builds a new TSDB file from a set of WALs
	BuildFromWALs(time.Time, []WALIdentifier) error
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

	chunkFilter chunk.RequestChunkFilterer
	shipper     indexshipper.IndexShipper
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

func (m *tsdbManager) Start() (err error) {
	var (
		buckets, indices, loadingErrors int
	)

	defer func() {
		level.Info(m.log).Log(
			"msg", "loaded leftover local indices",
			"err", err,
			"successful", err == nil,
			"buckets", buckets,
			"indices", indices,
			"failures", loadingErrors,
		)
	}()

	// load list of multitenant tsdbs
	mulitenantDir := managerMultitenantDir(m.dir)
	files, err := ioutil.ReadDir(mulitenantDir)
	if err != nil {
		return err
	}

	for _, f := range files {
		if !f.IsDir() {
			continue
		}

		bucket, err := strconv.Atoi(f.Name())
		if err != nil {
			level.Warn(m.log).Log(
				"msg", "failed to parse bucket in multitenant dir ",
				"err", err.Error(),
			)
			continue
		}
		buckets++

		tsdbs, err := ioutil.ReadDir(filepath.Join(mulitenantDir, f.Name()))
		if err != nil {
			level.Warn(m.log).Log(
				"msg", "failed to open period bucket dir",
				"bucket", bucket,
				"err", err.Error(),
			)
			continue
		}

		for _, db := range tsdbs {
			id, ok := parseMultitenantTSDBPath(db.Name())
			if !ok {
				continue
			}
			indices++

			prefixed := newPrefixedIdentifier(id, filepath.Join(mulitenantDir, f.Name()), "")
			loaded, err := NewShippableTSDBFile(
				prefixed,
				false,
			)

			if err != nil {
				level.Warn(m.log).Log(
					"msg", "",
					"tsdbPath", prefixed.Path(),
					"err", err.Error(),
				)
				loadingErrors++
			}

			if err := m.shipper.AddIndex(f.Name(), "", loaded); err != nil {
				loadingErrors++
				return err
			}
		}

	}

	return nil
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

		// Embed the tenant label into TSDB
		lb := labels.NewBuilder(ls)
		lb.Set(TenantLabel, user)
		withTenant := lb.Labels()

		// Add the chunks to all relevant builders
		for pd, matchingChks := range pds {
			b, ok := periods[pd]
			if !ok {
				b = NewBuilder()
				periods[pd] = b
			}

			b.AddSeries(
				withTenant,
				// use the fingerprint without the added tenant label
				// so queries route to the chunks which actually exist.
				model.Fingerprint(ls.Hash()),
				matchingChks,
			)
		}

	}); err != nil {
		level.Error(m.log).Log("err", err.Error(), "msg", "building TSDB from WALs")
		return err
	}

	for p, b := range periods {

		dstDir := filepath.Join(managerMultitenantDir(m.dir), fmt.Sprint(p))
		dst := newPrefixedIdentifier(
			MultitenantTSDBIdentifier{
				nodeName: m.nodeName,
				ts:       t,
			},
			dstDir,
			"",
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

		loaded, err := NewShippableTSDBFile(dst, false)
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

func (m *tsdbManager) indices(ctx context.Context, from, through model.Time, user string) (Index, error) {
	var indices []Index

	// Ensure we query both per tenant and multitenant TSDBs

	for _, bkt := range indexBuckets(m.indexPeriod, from, through) {
		if err := m.shipper.ForEach(ctx, fmt.Sprintf("%d", bkt), user, func(idx shipper_index.Index) error {
			_, multitenant := parseMultitenantTSDBName(idx.Name())
			impl, ok := idx.(Index)
			if !ok {
				return fmt.Errorf("unexpected shipper index type: %T", idx)
			}
			if multitenant {
				indices = append(indices, NewMultiTenantIndex(impl))
			} else {
				indices = append(indices, impl)
			}
			return nil
		}); err != nil {
			return nil, err
		}

	}

	if len(indices) == 0 {
		return NoopIndex{}, nil
	}
	idx, err := NewMultiIndex(indices...)
	if err != nil {
		return nil, err
	}

	if m.chunkFilter != nil {
		idx.SetChunkFilterer(m.chunkFilter)
	}
	return idx, nil
}

// TODO(owen-d): how to better implement this?
// setting 0->maxint will force the tsdbmanager to always query
// underlying tsdbs, which is safe, but can we optimize this?
func (m *tsdbManager) Bounds() (model.Time, model.Time) {
	return 0, math.MaxInt64
}

func (m *tsdbManager) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	m.chunkFilter = chunkFilter
}

// Close implements Index.Close, but we offload this responsibility
// to the index shipper
func (m *tsdbManager) Close() error {
	return nil
}

func (m *tsdbManager) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	idx, err := m.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.GetChunkRefs(ctx, userID, from, through, res, shard, matchers...)
}

func (m *tsdbManager) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	idx, err := m.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.Series(ctx, userID, from, through, res, shard, matchers...)
}

func (m *tsdbManager) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := m.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.LabelNames(ctx, userID, from, through, matchers...)
}

func (m *tsdbManager) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := m.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.LabelValues(ctx, userID, from, through, name, matchers...)
}
