package tsdb

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

// nolint:revive
// TSDBManager wraps the index shipper and writes/manages
// TSDB files on  disk
type TSDBManager interface {
	Start() error
	// Builds a new TSDB file from a set of WALs
	BuildFromWALs(time.Time, []WALIdentifier) error
}

// it is mandatory to have 24h index period for tsdb index store
const indexPeriod = 24 * time.Hour

/*
tsdbManager is used for managing active index and is responsible for:
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
			for _, bucket := range indexBuckets(chk.From(), chk.Through()) {
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

func indexBuckets(from, through model.Time) (res []int) {
	start := from.Time().UnixNano() / int64(indexPeriod)
	end := through.Time().UnixNano() / int64(indexPeriod)
	for cur := start; cur <= end; cur++ {
		res = append(res, int(cur))
	}
	return
}
