package metastore

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/raft"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/raftlogpb"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/wal"
)

type mockFSM struct {
	msgs []proto.Message
}

func (r *mockFSM) Apply(_ *raft.Raft, m proto.Message, _ time.Duration) (raft.ApplyFuture, proto.Message, error) {
	r.msgs = append(r.msgs, m)
	return nil, nil, nil
}

func TestMetastore_applyMark(t *testing.T) {
	m, _, err := newMetastoreForTests(t, Config{})
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	m.clock = clock
	// Put two segments in the metastore, 1 minute apart.
	ulid1 := ulid.MustNew(ulid.Timestamp(clock.Now()), rand.Reader).String()
	ulid2 := ulid.MustNew(ulid.Timestamp(clock.Now().Add(time.Minute)), rand.Reader).String()
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.NoError(t, b.active.Put([]byte(ulid1), []byte{}))
		require.NoError(t, b.active.Put([]byte(ulid2), []byte{}))
	})
	m.state.active[ulid1] = &metastorepb.BlockMeta{Id: ulid1}
	m.state.active[ulid2] = &metastorepb.BlockMeta{Id: ulid2}
	// Should be a no-op, such that no metadata is deleted.
	_, err = m.state.applyMark(&raftlogpb.MarkCommand{
		Timestamp: uint64(clock.Now().UnixMilli()),
	})
	require.NoError(t, err)
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.NotNil(t, b.active.Get([]byte(ulid1)))
		require.Nil(t, b.marked.Get([]byte(ulid1)))
		require.NotNil(t, b.active.Get([]byte(ulid2)))
		require.Nil(t, b.marked.Get([]byte(ulid2)))
	})
	// Should mark ulid1.
	_, err = m.state.applyMark(&raftlogpb.MarkCommand{
		Timestamp: uint64(clock.Now().Add(time.Second).UnixMilli()),
	})
	require.NoError(t, err)
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.Nil(t, b.active.Get([]byte(ulid1)))
		require.NotNil(t, b.marked.Get([]byte(ulid1)))
		require.NotNil(t, b.active.Get([]byte(ulid2)))
		require.Nil(t, b.marked.Get([]byte(ulid2)))
	})
	// Should move ulid1 from active to marked.
	_, ok := m.state.active[ulid1]
	require.False(t, ok)
	_, ok = m.state.marked[ulid1]
	require.True(t, ok)
	// But not affect ulid2.
	_, ok = m.state.active[ulid2]
	require.True(t, ok)
	// Should mark ulid2.
	_, err = m.state.applyMark(&raftlogpb.MarkCommand{
		Timestamp: uint64(clock.Now().Add(time.Minute + time.Second).UnixMilli()),
	})
	require.NoError(t, err)
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.Nil(t, b.active.Get([]byte(ulid1)))
		require.NotNil(t, b.marked.Get([]byte(ulid1)))
		require.Nil(t, b.active.Get([]byte(ulid2)))
		require.NotNil(t, b.marked.Get([]byte(ulid2)))
	})
	// Should move ulid2 from active to marked.
	_, ok = m.state.active[ulid2]
	require.False(t, ok)
	_, ok = m.state.marked[ulid2]
	require.True(t, ok)
	// But not affect ulid1.
	_, ok = m.state.marked[ulid1]
	require.True(t, ok)
}

func TestMetastore_doMark(t *testing.T) {
	c := Config{RetentionPeriod: time.Minute}
	m, r, err := newMetastoreForTests(t, c)
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	m.clock = clock
	// Should apply a MarkCommand to the FSM.
	clock.Advance(time.Second)
	require.NoError(t, m.doMark())
	require.Len(t, r.msgs, 1)
	cmd, ok := r.msgs[0].(*raftlogpb.MarkCommand)
	require.True(t, ok)
	require.Equal(t, cmd.Timestamp, uint64(clock.Now().Add(-c.RetentionPeriod).UnixMilli()))
}

func TestMetastore_applySweep(t *testing.T) {
	m, _, err := newMetastoreForTests(t, Config{})
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	m.clock = clock
	// Put two segments in the metastore, 1 minute apart.
	ulid1 := ulid.MustNew(ulid.Timestamp(clock.Now()), rand.Reader).String()
	ulid2 := ulid.MustNew(ulid.Timestamp(clock.Now().Add(time.Minute)), rand.Reader).String()
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.NoError(t, b.marked.Put([]byte(ulid1), []byte{}))
		require.NoError(t, b.marked.Put([]byte(ulid2), []byte{}))
	})
	m.state.marked[ulid1] = &metastorepb.BlockMeta{Id: ulid1}
	m.state.marked[ulid2] = &metastorepb.BlockMeta{Id: ulid2}
	// Should be a no-op, such that no metadata is deleted.
	_, err = m.state.applySweep(&raftlogpb.SweepCommand{
		Timestamp: uint64(clock.Now().UnixMilli()),
	})
	require.NoError(t, err)
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.NotNil(t, b.marked.Get([]byte(ulid1)))
		require.NotNil(t, b.marked.Get([]byte(ulid2)))
	})
	// Should delete ulid1 from the metastore.
	_, err = m.state.applySweep(&raftlogpb.SweepCommand{
		Timestamp: uint64(clock.Now().Add(time.Second).UnixMilli()),
	})
	require.NoError(t, err)
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.Nil(t, b.marked.Get([]byte(ulid1)))
		require.NotNil(t, b.marked.Get([]byte(ulid2)))
	})
	// Should delete ulid1.
	_, ok := m.state.marked[ulid1]
	require.False(t, ok)
	// But not affect ulid2.
	_, ok = m.state.marked[ulid2]
	require.True(t, ok)
	// Should delete ulid2 from the metastore.
	_, err = m.state.applySweep(&raftlogpb.SweepCommand{
		Timestamp: uint64(clock.Now().Add(time.Minute + time.Second).UnixMilli()),
	})
	require.NoError(t, err)
	withBoltTx(t, m.db, func(t *testing.T, b *metadataBuckets) {
		require.Nil(t, b.marked.Get([]byte(ulid1)))
		require.Nil(t, b.marked.Get([]byte(ulid2)))
	})
	// Should delete ulid2.
	_, ok = m.state.marked[ulid2]
	require.False(t, ok)
	// ulid1 should still be deleted.
	_, ok = m.state.marked[ulid1]
	require.False(t, ok)
}

func TestMetastore_doSweep(t *testing.T) {
	t.Run("command is applied to FSM", func(t *testing.T) {
		c := Config{RetentionPeriod: time.Minute, RetentionGracePeriod: time.Minute}
		m, r, err := newMetastoreForTests(t, c)
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		m.clock = clock
		// Should apply a MarkCommand to the FSM.
		clock.Advance(time.Second)
		require.NoError(t, m.doSweep())
		require.Len(t, r.msgs, 1)
		cmd, ok := r.msgs[0].(*raftlogpb.SweepCommand)
		require.True(t, ok)
		require.Equal(t, cmd.Timestamp, uint64(clock.Now().Add(-c.RetentionPeriod-c.RetentionGracePeriod).UnixMilli()))
	})

	t.Run("objects older than grace period are deleted from storage", func(t *testing.T) {
		c := Config{RetentionPeriod: time.Minute, RetentionGracePeriod: time.Minute}
		m, _, err := newMetastoreForTests(t, c)
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		m.clock = clock
		// Add two segments to the mock storage and metastore, 1 minute apart.
		ulid1 := ulid.MustNew(ulid.Timestamp(clock.Now()), rand.Reader).String()
		ulid2 := ulid.MustNew(ulid.Timestamp(clock.Now().Add(time.Minute)), rand.Reader).String()
		s := m.storage.(*testutils.MockStorage)
		// Put fake objects into the mock storage.
		require.NoError(t, s.PutObject(context.TODO(), path.Join(wal.Dir, ulid1), bytes.NewReader([]byte{})))
		require.NoError(t, s.PutObject(context.TODO(), path.Join(wal.Dir, ulid2), bytes.NewReader([]byte{})))
		require.Len(t, s.Internals(), 2)
		// Put their respective metadata into the metastore as marked.
		m.state.marked[ulid1] = &metastorepb.BlockMeta{Id: ulid1}
		m.state.marked[ulid2] = &metastorepb.BlockMeta{Id: ulid2}
		// Should delete ulid1 from the mock storage.
		clock.Advance(c.RetentionPeriod + c.RetentionGracePeriod + time.Second)
		require.NoError(t, m.doSweep())
		ok1, err := s.ObjectExists(context.TODO(), path.Join(wal.Dir, ulid1))
		require.NoError(t, err)
		require.False(t, ok1)
		ok2, err := s.ObjectExists(context.TODO(), path.Join(wal.Dir, ulid2))
		require.NoError(t, err)
		require.True(t, ok2)
		// Should delete ulid2 from mock storage.
		clock.Advance(c.RetentionPeriod + c.RetentionGracePeriod + time.Second)
		require.NoError(t, m.doSweep())
		ok2, err = s.ObjectExists(context.TODO(), path.Join(wal.Dir, ulid2))
		require.NoError(t, err)
		require.False(t, ok2)
	})
}

func newMetastoreForTests(t *testing.T, config Config) (*Metastore, *mockFSM, error) {
	if config.DataDir == "" {
		config.DataDir = os.TempDir()
	}
	db := newDB(config, log.NewNopLogger())
	if err := db.open(false); err != nil {
		return nil, nil, err
	}
	t.Cleanup(db.shutdown)
	cluster := raft.MakeCluster(1, t, nil)
	t.Cleanup(cluster.Close)
	fsm := &mockFSM{}
	return &Metastore{
		config:   config,
		logger:   log.NewNopLogger(),
		reg:      prometheus.NewRegistry(),
		db:       db,
		done:     make(chan struct{}),
		raft:     cluster.Leader(),
		state:    newMetastoreState(log.NewNopLogger(), db),
		storage:  testutils.NewMockStorage(),
		applyFSM: fsm.Apply,
	}, fsm, nil
}

func withBoltTx(t *testing.T, db *boltdb, fn func(t *testing.T, b *metadataBuckets)) {
	tx, err := db.boltdb.Begin(true)
	require.NoError(t, err)
	b, err := getBlockMetadataBuckets(tx)
	require.NoError(t, err)
	fn(t, b)
	require.NoError(t, tx.Commit())
}
