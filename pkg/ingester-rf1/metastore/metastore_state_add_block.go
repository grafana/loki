package metastore

import (
	"context"

	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
)

func (m *Metastore) AddBlock(_ context.Context, req *metastorepb.AddBlockRequest) (*metastorepb.AddBlockResponse, error) {
	_, resp, err := applyCommand[*metastorepb.AddBlockRequest, *metastorepb.AddBlockResponse](m.raft, req, m.config.Raft.ApplyTimeout)
	return resp, err
}

func (m *metastoreState) applyAddBlock(request *metastorepb.AddBlockRequest) (*metastorepb.AddBlockResponse, error) {
	_ = level.Info(m.logger).Log("msg", "adding block", "block_id", request.Block.Id)
	if request.Block.CompactionLevel != 0 {
		_ = level.Error(m.logger).Log(
			"msg", "compaction not implemented, ignoring block with non-zero compaction level",
			"compaction_level", request.Block.CompactionLevel,
			"block", request.Block.Id,
		)
		return &metastorepb.AddBlockResponse{}, nil
	}
	value, err := proto.Marshal(request.Block)
	if err != nil {
		return nil, err
	}
	err = m.db.boltdb.Batch(func(tx *bbolt.Tx) error {
		return updateBlockMetadataBucket(tx, func(bucket *bbolt.Bucket) error {
			return bucket.Put([]byte(request.Block.Id), value)
		})
	})
	if err != nil {
		_ = level.Error(m.logger).Log(
			"msg", "failed to add block",
			"block", request.Block.Id,
			"err", err,
		)
		return nil, err
	}
	m.putSegment(request.Block)
	return &metastorepb.AddBlockResponse{}, nil
}
