package tsdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// nolint:revive
// TSDBManager wraps the index shipper and writes/manages
// TSDB files on  disk
type TSDBManager interface {
	Start() error
	// Builds a new TSDB file from a set of WALs
	BuildFromWALs(time.Time, []WALIdentifier) error
	// Builds a new TSDB file from tenantHeads
	BuildFromHead(*tenantHeads) error
}

/*
tsdbManager is used for managing active index and is responsible for:
  - Turning WALs into optimized multi-tenant TSDBs when requested
  - Serving reads from these TSDBs
  - Shipping them to remote storage
  - Keeping them available for querying
  - Removing old TSDBs which are no longer needed
*/
type tsdbManager struct {
	nodeName    string // node name
	log         log.Logger
	dir         string
	metrics     *Metrics
	tableRanges config.TableRanges

	sync.RWMutex

	shipper indexshipper.IndexShipper
}

func NewTSDBManager(
	nodeName,
	dir string,
	shipper indexshipper.IndexShipper,
	tableRanges config.TableRanges,
	logger log.Logger,
	metrics *Metrics,
) TSDBManager {
	return &tsdbManager{
		nodeName:    nodeName,
		log:         log.With(logger, "component", "tsdb-manager"),
		dir:         dir,
		metrics:     metrics,
		tableRanges: tableRanges,
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

	// regexp for finding the trailing index bucket number at the end of table name
	extractBucketNumberRegex, err := regexp.Compile(`[0-9]+$`)
	if err != nil {
		return err
	}

	// load list of multitenant tsdbs
	mulitenantDir := managerMultitenantDir(m.dir)
	files, err := os.ReadDir(mulitenantDir)
	if err != nil {
		return err
	}

	for _, f := range files {
		if !f.IsDir() {
			continue
		}

		bucket := f.Name()
		if !extractBucketNumberRegex.MatchString(f.Name()) {
			level.Warn(m.log).Log(
				"msg", "directory name does not match expected bucket name pattern",
				"name", bucket,
				"err", err.Error(),
			)
			continue
		}
		buckets++

		tsdbs, err := os.ReadDir(filepath.Join(mulitenantDir, bucket))
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

			prefixed := newPrefixedIdentifier(id, filepath.Join(mulitenantDir, bucket), "")
			loaded, err := NewShippableTSDBFile(prefixed)

			if err != nil {
				level.Warn(m.log).Log(
					"msg", "",
					"tsdbPath", prefixed.Path(),
					"err", err.Error(),
				)
				loadingErrors++
			}

			if err := m.shipper.AddIndex(bucket, "", loaded); err != nil {
				loadingErrors++
				return err
			}
		}

	}

	return nil
}

func (m *tsdbManager) buildFromHead(heads *tenantHeads) (err error) {
	periods := make(map[string]*Builder)

	if err := heads.forAll(func(user string, ls labels.Labels, fp uint64, chks index.ChunkMetas) error {

		// chunks may overlap index period bounds, in which case they're written to multiple
		pds := make(map[string]index.ChunkMetas)
		for _, chk := range chks {
			idxBuckets := indexBuckets(chk.From(), chk.Through(), m.tableRanges)

			for _, bucket := range idxBuckets {
				pds[bucket] = append(pds[bucket], chk)
			}
		}

		// Embed the tenant label into TSDB
		lb := labels.NewBuilder(ls)
		lb.Set(TenantLabel, user)
		withTenant := lb.Labels(nil)

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
				model.Fingerprint(fp),
				matchingChks,
			)
		}

		return nil
	}); err != nil {
		level.Error(m.log).Log("err", err.Error(), "msg", "building TSDB")
		return err
	}

	for p, b := range periods {
		dstDir := filepath.Join(managerMultitenantDir(m.dir), fmt.Sprint(p))
		dst := newPrefixedIdentifier(
			MultitenantTSDBIdentifier{
				nodeName: m.nodeName,
				ts:       heads.start,
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

		loaded, err := NewShippableTSDBFile(dst)
		if err != nil {
			return err
		}

		if err := m.shipper.AddIndex(p, "", loaded); err != nil {
			return err
		}
	}

	m.metrics.tsdbBuildLastSuccess.SetToCurrentTime()
	return nil
}

func (m *tsdbManager) BuildFromHead(heads *tenantHeads) (err error) {
	level.Debug(m.log).Log("msg", "building heads")
	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}

		m.metrics.tsdbBuilds.WithLabelValues(status, "head").Inc()
	}()

	return m.buildFromHead(heads)
}

func (m *tsdbManager) BuildFromWALs(t time.Time, ids []WALIdentifier) (err error) {
	level.Debug(m.log).Log("msg", "building WALs", "n", len(ids), "ts", t)
	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}

		m.metrics.tsdbBuilds.WithLabelValues(status, "wal").Inc()
	}()

	level.Debug(m.log).Log("msg", "recovering tenant heads")
	for _, id := range ids {
		tmp := newTenantHeads(id.ts, defaultHeadManagerStripeSize, m.metrics, m.log)
		if err = recoverHead(m.dir, tmp, []WALIdentifier{id}); err != nil {
			return errors.Wrap(err, "building TSDB from WALs")
		}

		err := m.buildFromHead(tmp)
		if err != nil {
			return err
		}
	}

	return nil
}

func indexBuckets(from, through model.Time, tableRanges config.TableRanges) (res []string) {
	start := from.Time().UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod)
	end := through.Time().UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod)
	for cur := start; cur <= end; cur++ {
		cfg := tableRanges.ConfigForTableNumber(cur)
		if cfg != nil {
			res = append(res, cfg.IndexTables.Prefix+strconv.Itoa(int(cur)))
		}
	}
	if len(res) == 0 {
		level.Warn(util_log.Logger).Log("err", "could not find config for table(s) from: %d, through %d", start, end)
	}
	return
}
