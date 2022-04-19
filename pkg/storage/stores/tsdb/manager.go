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
	period  period
	name    string // node name
	log     log.Logger
	dir     string
	metrics *Metrics

	sync.RWMutex

	shipper indexshipper.IndexShipper
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
	b := index.NewBuilder()

	if err := tmp.forAll(func(user string, ls labels.Labels, chks index.ChunkMetas) {
		b.AddSeries(
			append(ls, labels.Label{
				Name:  TenantLabel,
				Value: user,
			}),
			chks,
		)
	}); err != nil {
		level.Error(m.log).Log("err", err.Error(), "msg", "building TSDB from WALs")
		return err
	}

	desired := MultitenantTSDBIdentifier{
		nodeName: m.name,
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

	if err := m.shipper.AddIndex(fmt.Sprintf("%d", m.period.PeriodFor(t)), "", loaded); err != nil {
		return err
	}

	return nil
}
