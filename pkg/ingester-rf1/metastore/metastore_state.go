package metastore

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"go.etcd.io/bbolt"

	ingesterrf1 "github.com/grafana/loki/v3/pkg/ingester-rf1"
	metastorepb "github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
)

type metastoreState struct {
	logger log.Logger

	active map[string]*metastorepb.BlockMeta
	marked map[string]*metastorepb.BlockMeta
	mtx    sync.Mutex

	db    *boltdb
	store ingesterrf1.Storage
}

func newMetastoreState(logger log.Logger, db *boltdb, store ingesterrf1.Storage) *metastoreState {
	return &metastoreState{
		logger: logger,
		active: make(map[string]*metastorepb.BlockMeta),
		marked: make(map[string]*metastorepb.BlockMeta),
		db:     db,
	}
}

func (m *metastoreState) reset() {
	m.mtx.Lock()
	clear(m.active)
	m.mtx.Unlock()
}

func (m *metastoreState) restore(db *boltdb) error {
	m.reset()
	return db.boltdb.View(func(tx *bbolt.Tx) error {
		if err := m.restoreMetadata(tx); err != nil {
			return fmt.Errorf("failed to restore metadata entries: %w", err)
		}
		return nil
	})
}

func (m *metastoreState) restoreMetadata(tx *bbolt.Tx) error {
	b, err := getBlockMetadataBuckets(tx)
	if err != nil {
		if errors.Is(err, bbolt.ErrBucketNotFound) {
			return nil
		}
		return err
	}
	return m.loadMetadata(b)
}

func (m *metastoreState) loadMetadata(b *metadataBuckets) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	c := b.active.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var md metastorepb.BlockMeta
		if err := proto.Unmarshal(v, &md); err != nil {
			return fmt.Errorf("failed to block %q: %w", string(k), err)
		}
		m.active[md.Id] = &md
	}

	c = b.marked.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var md metastorepb.BlockMeta
		if err := proto.Unmarshal(v, &md); err != nil {
			return fmt.Errorf("failed to block %q: %w", string(k), err)
		}
		m.marked[md.Id] = &md
	}

	return nil
}

func (m *metastoreState) putSegment(segment *metastorepb.BlockMeta) {
	m.mtx.Lock()
	m.active[segment.Id] = segment
	m.mtx.Unlock()
}
