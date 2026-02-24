package physical

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestScanSet_Shards_DataObjBatching(t *testing.T) {
	// ScanSet with 5 DataObjScan targets and ShardBatchSize=2 should yield 3 batched ScanSets (2+2+1).
	targets := make([]*ScanTarget, 5)
	for i := range targets {
		targets[i] = &ScanTarget{
			Type: ScanTypeDataObject,
			DataObject: &DataObjScan{
				NodeID: ulid.Make(),
			},
		}
	}
	s := &ScanSet{
		NodeID:         ulid.Make(),
		Targets:        targets,
		ShardBatchSize: 2,
	}

	var shards []Node
	for shard := range s.Shards() {
		shards = append(shards, shard)
	}

	require.Len(t, shards, 3, "expected 3 batched shards (2+2+1)")
	for i, shard := range shards {
		ss, ok := shard.(*ScanSet)
		require.True(t, ok, "shard %d should be ScanSet", i)
		require.Equal(t, 0, ss.ShardBatchSize, "injected ScanSet should have ShardBatchSize 0")
	}
	require.Len(t, shards[0].(*ScanSet).Targets, 2)
	require.Len(t, shards[1].(*ScanSet).Targets, 2)
	require.Len(t, shards[2].(*ScanSet).Targets, 1)
}
