package tsdb

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// nolint:revive
// TSDBManager wraps the index shipper and writes/manages
// TSDB files on  disk
type TSDBManager interface {
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
	Index       // placeholder until I implement
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
	log log.Logger,
	metrics *Metrics,
) TSDBManager {
	return &tsdbManager{
		indexPeriod: indexPeriod,
		nodeName:    nodeName,
		log:         log,
		dir:         dir,
		metrics:     metrics,
		shipper:     shipper,
	}
}

func indexBuckets(indexPeriod time.Duration, from, through model.Time) []int {
	start := from.Time().UnixNano() / int64(indexPeriod)
	end := through.Time().UnixNano() / int64(indexPeriod)
	if start == end {
		return []int{int(start)}
	}
	return []int{int(start), int(end)}
}

func (m *tsdbManager) BuildFromWALs(t time.Time, ids []WALIdentifier) (err error) {
	// get relevant wals
	// iterate them, build tsdb in scratch dir
	defer func() {
		m.metrics.tsdbCreationsTotal.Inc()
		if err != nil {
			m.metrics.tsdbCreationFailures.Inc()
		}
	}()

	tmp := newTenantHeads(t, defaultHeadManagerStripeSize, m.metrics, m.log)
	if err = recoverHead(m.dir, tmp, ids, true); err != nil {
		return errors.Wrap(err, "building TSDB from WALs")
	}

	periods := make(map[int]*index.Builder)

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
				b = index.NewBuilder()
				periods[pd] = b
			}

			b.AddSeries(
				append(ls, labels.Label{
					Name:  TenantLabel,
					Value: user,
				}),
				matchingChks,
			)
		}

	}); err != nil {
		level.Error(m.log).Log("err", err.Error(), "msg", "building TSDB from WALs")
		return err
	}

	for p, b := range periods {
		desired := MultitenantTSDBIdentifier{
			nodeName: m.nodeName,
			ts:       t,
		}

		dstFile := filepath.Join(managerBuiltDir(m.dir), desired.Name())

		// build/move tsdb to multitenant/built dir
		_, err = b.Build(
			context.TODO(),
			managerScratchDir(m.dir),
			func(from, through model.Time, checksum uint32) (index.Identifier, string) {

				// We don't use the resulting ID b/c this isn't compaction.
				// Instead we'll discard this and use our own override.
				return index.Identifier{}, dstFile
			},
		)
		if err != nil {
			return err
		}

		loaded, err := NewShippableTSDBFile(dstFile)
		if err != nil {
			return err
		}

		if err := m.shipper.AddIndex(fmt.Sprintf("%d-%s.tsdb", p, m.nodeName), "", loaded); err != nil {
			return err
		}
	}

	return nil
}
