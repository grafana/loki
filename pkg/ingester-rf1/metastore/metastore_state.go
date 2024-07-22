package metastore

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"go.etcd.io/bbolt"

	metastorepb "github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
)

type metastoreState struct {
	logger log.Logger

	segmentsMutex sync.Mutex
	segments      map[string]*metastorepb.BlockMeta

	db *boltdb
}

func newMetastoreState(logger log.Logger, db *boltdb) *metastoreState {
	return &metastoreState{
		logger:   logger,
		segments: make(map[string]*metastorepb.BlockMeta),
		db:       db,
	}
}

func (m *metastoreState) reset() {
	m.segmentsMutex.Lock()
	clear(m.segments)
	m.segmentsMutex.Unlock()
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
	mdb, err := getBlockMetadataBucket(tx)
	switch {
	case err == nil:
	case errors.Is(err, bbolt.ErrBucketNotFound):
		return nil
	default:
		return err
	}
	return m.loadSegments(mdb)
}

func (m *metastoreState) putSegment(segment *metastorepb.BlockMeta) {
	m.segmentsMutex.Lock()
	m.segments[segment.Id] = segment
	m.segmentsMutex.Unlock()
}

func (m *metastoreState) loadSegments(b *bbolt.Bucket) error {
	m.segmentsMutex.Lock()
	defer m.segmentsMutex.Unlock()
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var md metastorepb.BlockMeta
		if err := proto.Unmarshal(v, &md); err != nil {
			return fmt.Errorf("failed to block %q: %w", string(k), err)
		}
		m.segments[md.Id] = &md
	}
	return nil
}
