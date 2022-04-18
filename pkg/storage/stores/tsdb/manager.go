package tsdb

import (
	"context"
	"fmt"
	"io/ioutil"
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

	Index

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

	return m.updateIndices()
}

// updateIndices replaces the *tsdbManager's list of indices
// with those on disk.
func (m *tsdbManager) updateIndices() (err error) {
	defer func() {
		m.metrics.tsdbManagerUpdatesTotal.Inc()
		if err != nil {
			m.metrics.tsdbManagerUpdatesFailedTotal.Inc()
		}
	}()
	var indices []Index

	// lock mtx, load file into list, unlock, start ship process
	built, err := m.listMultiTenantTSDBs(managerBuiltDir(m.dir))
	if err != nil {
		return err
	}
	for _, x := range built {
		idx, err := NewShippableTSDBFile(filepath.Join(managerBuiltDir(m.dir), x.Name()))
		if err != nil {
			return err
		}
		indices = append(indices, idx)
	}

	shipped, err := m.listMultiTenantTSDBs(managerShippedDir(m.dir))
	if err != nil {
		return err
	}
	for _, x := range shipped {
		idx, err := NewShippableTSDBFile(filepath.Join(managerShippedDir(m.dir), x.Name()))
		if err != nil {
			return err
		}
		indices = append(indices, idx)
	}

	var newIdx Index
	if len(indices) == 0 {
		newIdx = NoopIndex{}
	} else {
		newIdx, err = NewMultiIndex(indices...)
		if err != nil {
			return err
		}
	}

	m.Lock()
	defer m.Unlock()
	if err := m.Index.Close(); err != nil {
		return err
	}

	m.Index = NewLockedMutex(&m.RWMutex, newIdx)
	return nil
}

func (m *tsdbManager) listMultiTenantTSDBs(dir string) (res []MultitenantTSDBIdentifier, err error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		if id, ok := parseMultitenantTSDBPath(filepath.Base(f.Name())); ok {
			res = append(res, id)
		}
	}

	return
}

func (m *tsdbManager) Start() {
	go m.loop()
}

func (m *tsdbManager) loop() {
	// continually ship built indices to storage then move them to the shipped directory
	// continually remove shipped tsdbs over 1 period old
}
