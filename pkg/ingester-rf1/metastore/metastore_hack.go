package metastore

import (
	"time"

	"github.com/go-kit/log/level"
	"github.com/hashicorp/raft"
	"github.com/oklog/ulid"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/raftlogpb"
)

// FIXME(kolesnikovae):
//   Remove once compaction is implemented.
//   Or use index instead of the timestamp.

func (m *Metastore) cleanupLoop() {
	t := time.NewTicker(10 * time.Minute)
	defer func() {
		t.Stop()
		m.wg.Done()
	}()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			if m.raft.State() != raft.Leader {
				continue
			}
			timestamp := uint64(m.clock.Now().Add(-1 * time.Hour).UnixMilli())
			req := &raftlogpb.TruncateCommand{Timestamp: timestamp}
			_, _, err := applyCommand[*raftlogpb.TruncateCommand, *anypb.Any](m.raft, req, m.config.Raft.ApplyTimeout)
			if err != nil {
				_ = level.Error(m.logger).Log("msg", "failed to apply truncate command", "err", err)
			}
		}
	}
}

func (m *metastoreState) applyTruncate(request *raftlogpb.TruncateCommand) (*anypb.Any, error) {
	m.segmentsMutex.Lock()
	defer m.segmentsMutex.Unlock()
	tx, err := m.db.boltdb.Begin(true)
	if err != nil {
		_ = level.Error(m.logger).Log("msg", "failed to start transaction", "err", err)
		return nil, err
	}
	var truncated int
	defer func() {
		if err = tx.Commit(); err != nil {
			_ = level.Error(m.logger).Log("msg", "failed to commit transaction", "err", err)
			return
		}
		_ = level.Info(m.logger).Log("msg", "stale segments truncated", "segments", truncated)
	}()
	bucket, err := getBlockMetadataBucket(tx)
	if err != nil {
		_ = level.Error(m.logger).Log("msg", "failed to get metadata bucket", "err", err)
		return nil, err
	}
	for k, segment := range m.segments {
		if ulid.MustParse(segment.Id).Time() < request.Timestamp {
			if err = bucket.Delete([]byte(segment.Id)); err != nil {
				_ = level.Error(m.logger).Log("msg", "failed to delete stale segments", "err", err)
				continue
			}
			delete(m.segments, k)
			truncated++
		}
	}
	return &anypb.Any{}, nil
}
