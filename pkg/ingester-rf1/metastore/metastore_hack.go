package metastore

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/raft"
	"github.com/oklog/ulid"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/raftlogpb"
	"github.com/grafana/loki/v3/pkg/storage/wal"
)

// periodicMaintenance runs the periodic maintenance for the metastore.
func (m *Metastore) periodicMaintenance() {
	t := time.NewTicker(m.config.MaintenanceInterval)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			if m.raft.State() != raft.Leader {
				continue
			}
			level.Debug(m.logger).Log("msg", "running maintenance")
			m.doMaintenance()
		}
	}
}

// doMaintenance runs the maintenance for the metastore, and is expected to
// be scheduled at a regular interval. The leader in the raft quorum
// coordinates all maintenance tasks.
func (m *Metastore) doMaintenance() {
	state := m.raft.State()
	if state == raft.Leader {
		if err := m.doMark(); err != nil {
			level.Error(m.logger).Log("msg", "failed to do mark step", "err", err)
		}
		if err := m.doSweep(); err != nil {
			level.Error(m.logger).Log("msg", "failed to do sweep step", "err", err)
		}
	}
}

// doMark marks all segments older than the retention period as deleted,
// and replicates it across the FSM.
func (m *Metastore) doMark() error {
	olderThan := m.clock.Now().Add(-m.config.RetentionPeriod)
	req := &raftlogpb.MarkCommand{Timestamp: uint64(olderThan.UnixMilli())}
	_, _, err := m.applyFSM(m.raft, req, m.config.Raft.ApplyTimeout)
	return err
}

// doSweep deletes all marked segments from object storage, and then replicates
// it across the FSM.
func (m *Metastore) doSweep() error {
	olderThan := m.clock.Now().Add(-m.config.RetentionPeriod - m.config.RetentionGracePeriod)
	OlderThanMilli := uint64(olderThan.UnixMilli())
	// Delete marked segments that have exceeded the grace period.
	for _, v := range m.state.marked {
		if ulid.MustParse(v.Id).Time() < OlderThanMilli {
			if err := m.deleteObject(v); err != nil {
				return err
			}
		}
	}
	// Remove the deleted segments from memory and the on-disk database, and
	// replicate it across the FSM so followers do the same.
	req := &raftlogpb.SweepCommand{Timestamp: OlderThanMilli}
	_, _, err := m.applyFSM(m.raft, req, m.config.Raft.ApplyTimeout)
	return err
}

func (m *Metastore) deleteObject(v *metastorepb.BlockMeta) error {
	ctx, cancelFunc := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancelFunc()
	_ = level.Debug(m.logger).Log("msg", "deleting segment from object storage", "id", v.Id)
	err := m.storage.DeleteObject(ctx, path.Join(wal.Dir, v.Id))
	if err != nil && !m.storage.IsObjectNotFoundErr(err) {
		_ = level.Error(m.logger).Log("msg", "failed to delete segment from object storage", "id", v.Id, "err", err)
		return fmt.Errorf("failed to delete segment from object store: %w", err)
	}
	_ = level.Debug(m.logger).Log("msg", "deleted segment from object storage", "id", v.Id)
	return nil
}

func (m *metastoreState) applyMark(request *raftlogpb.MarkCommand) (*anypb.Any, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	tx, err := m.db.boltdb.Begin(true)
	if err != nil {
		_ = level.Error(m.logger).Log("msg", "failed to begin tx", "err", err)
		return nil, err
	}
	var marked int
	defer func() {
		if err = tx.Commit(); err != nil {
			_ = level.Error(m.logger).Log("msg", "failed to commit tx", "err", err)
			return
		}
		_ = level.Info(m.logger).Log("msg", "marked segments for deletion", "n", marked)
	}()
	b, err := getBlockMetadataBuckets(tx)
	if err != nil {
		_ = level.Error(m.logger).Log("msg", "failed to get metadata bucket", "err", err)
		return nil, err
	}
	for k, v := range m.active {
		if ulid.MustParse(v.Id).Time() < request.Timestamp {
			if err = m.markSegment(b, v); err != nil {
				_ = level.Error(m.logger).Log("msg", "failed to mark segment", "id", v.Id, "err", err)
				continue
			}
			delete(m.active, k)
			m.marked[k] = v
			marked++
		}
	}
	return &anypb.Any{}, nil
}

func (m *metastoreState) applySweep(request *raftlogpb.SweepCommand) (*anypb.Any, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	tx, err := m.db.boltdb.Begin(true)
	if err != nil {
		_ = level.Error(m.logger).Log("msg", "failed to begin tx", "err", err)
		return nil, err
	}
	var sweeped int
	defer func() {
		if err = tx.Commit(); err != nil {
			_ = level.Error(m.logger).Log("msg", "failed to commit tx", "err", err)
			return
		}
		_ = level.Info(m.logger).Log("msg", "sweeped segments", "n", sweeped)
	}()
	b, err := getBlockMetadataBuckets(tx)
	if err != nil {
		_ = level.Error(m.logger).Log("msg", "failed to get metadata bucket", "err", err)
		return nil, err
	}
	for k, v := range m.marked {
		if ulid.MustParse(v.Id).Time() < request.Timestamp {
			if err = b.marked.Delete([]byte(v.Id)); err != nil {
				_ = level.Error(m.logger).Log("msg", "failed to delete segment from boltdb", "err", err)
			} else {
				delete(m.marked, k)
				sweeped++
			}
		}
	}
	return &anypb.Any{}, nil
}

func (m *metastoreState) markSegment(buckets *metadataBuckets, segment *metastorepb.BlockMeta) error {
	if err := buckets.active.Delete([]byte(segment.Id)); err != nil {
		return fmt.Errorf("failed to delete segment from boltdb: %w", err)
	}
	v, err := proto.Marshal(segment)
	if err != nil {
		return fmt.Errorf("failed to marshal segment: %w", err)
	}
	if err = buckets.marked.Put([]byte(segment.Id), v); err != nil {
		return fmt.Errorf("failed to mark segment: %w", err)
	}
	return nil
}
