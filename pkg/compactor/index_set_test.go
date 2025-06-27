package compactor

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

type indexUpdatesRecorder struct {
	CompactedIndex
	indexedChunks map[string][]Chunk
	removedChunks map[string][]string
}

func (i indexUpdatesRecorder) IndexChunk(chunkRef logproto.ChunkRef, lbls labels.Labels, sizeInKB uint32, logEntriesCount uint32) (bool, error) {
	lblsString := lbls.String()
	indexedChunks, ok := i.indexedChunks[lblsString]
	if !ok {
		i.indexedChunks[lblsString] = []Chunk{}
		indexedChunks = i.indexedChunks[lblsString]
	}
	indexedChunks = append(indexedChunks, dummyChunk{
		from:        chunkRef.From,
		through:     chunkRef.Through,
		fingerprint: chunkRef.Fingerprint,
		checksum:    chunkRef.Checksum,
		kb:          sizeInKB,
		entries:     logEntriesCount,
	})
	i.indexedChunks[lblsString] = indexedChunks

	return true, nil
}

func (i indexUpdatesRecorder) RemoveChunk(_, _ model.Time, _ []byte, lbls labels.Labels, chunkID string) error {
	lblsString := lbls.String()
	removedChunks, ok := i.removedChunks[lblsString]
	if !ok {
		i.removedChunks[lblsString] = []string{}
		removedChunks = i.removedChunks[lblsString]
	}
	removedChunks = append(removedChunks, chunkID)
	i.removedChunks[lblsString] = removedChunks

	return nil
}

type dummyChunk struct {
	from, through model.Time
	fingerprint   uint64
	checksum      uint32
	kb, entries   uint32
}

func (c dummyChunk) GetFrom() model.Time {
	return c.from
}

func (c dummyChunk) GetThrough() model.Time {
	return c.through
}

func (c dummyChunk) GetFingerprint() uint64 {
	return c.fingerprint
}

func (c dummyChunk) GetChecksum() uint32 {
	return c.checksum
}

func (c dummyChunk) GetSize() uint32 {
	return c.kb
}

func (c dummyChunk) GetEntriesCount() uint32 {
	return c.entries
}

func TestIndexSet_ApplyIndexUpdates(t *testing.T) {
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       dayFromTime(0),
				IndexType:  "tsdb",
				ObjectType: "filesystem",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
		},
	}

	userID := "u1"
	var chunksToDelete []string
	var expectedChunksToRemove []string
	for i := 0; i < 10; i++ {
		chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
			Fingerprint: uint64(i),
			UserID:      userID,
			From:        model.Time(i),
			Through:     model.Time(i + 1),
			Checksum:    uint32(i),
		})
		chunksToDelete = append(chunksToDelete, chunkID)
		expectedChunksToRemove = append(expectedChunksToRemove, chunkID)
	}

	var chunksToDeIndex []string
	for i := 10; i < 20; i++ {
		chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
			Fingerprint: uint64(i),
			UserID:      userID,
			From:        model.Time(i),
			Through:     model.Time(i + 1),
			Checksum:    uint32(i),
		})
		chunksToDeIndex = append(chunksToDeIndex, chunkID)
		expectedChunksToRemove = append(expectedChunksToRemove, chunkID)
	}

	var chunksToIndex []Chunk
	for i := 20; i < 30; i++ {
		chunksToIndex = append(chunksToIndex, dummyChunk{
			from:        model.Time(i),
			through:     model.Time(i + 1),
			fingerprint: uint64(i),
			checksum:    uint32(i),
			kb:          uint32(i),
			entries:     uint32(i),
		})
	}

	indexSet := &indexSet{
		userID: userID,
		compactedIndex: &indexUpdatesRecorder{
			indexedChunks: map[string][]Chunk{},
			removedChunks: map[string][]string{},
		},
	}

	lblFoo := labels.Labels{
		{
			Name:  "foo",
			Value: "bar",
		},
	}

	err := indexSet.applyUpdates(lblFoo.String(), chunksToDelete, chunksToDeIndex, chunksToIndex)
	require.NoError(t, err)

	require.Equal(t, map[string][]string{lblFoo.String(): expectedChunksToRemove}, indexSet.compactedIndex.(*indexUpdatesRecorder).removedChunks)
	require.Equal(t, map[string][]Chunk{lblFoo.String(): chunksToIndex}, indexSet.compactedIndex.(*indexUpdatesRecorder).indexedChunks)
}
