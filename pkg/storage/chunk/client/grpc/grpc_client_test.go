package grpc

import (
	"context"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

// This includes test for all RPCs in
// tableClient, indexClient, storageClient
func TestGrpcStore(t *testing.T) {
	var err error
	cleanup, storeAddress := createTestGrpcServer(t)
	defer cleanup()
	cfg := Config{Address: storeAddress}
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{
		{
			From:       config.DayTime{Time: 1564358400000},
			IndexType:  "grpc-store",
			ObjectType: "grpc-store",
			Schema:     "v10",
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: 604800000000000,
					Tags:   nil,
				}},
			RowShards: 16,
		},
	}}

	// rpc calls specific to tableClient
	tableClient, _ := NewTestTableClient(cfg)
	tableDesc := config.TableDesc{
		Name:              "chunk_2607",
		UseOnDemandIOMode: false,
		ProvisionedRead:   300,
		ProvisionedWrite:  1,
		Tags:              nil,
	}
	err = tableClient.CreateTable(context.Background(), tableDesc)
	require.NoError(t, err)

	_, err = tableClient.ListTables(context.Background())
	require.NoError(t, err)

	_, _, err = tableClient.DescribeTable(context.Background(), "chunk_2591")
	require.NoError(t, err)

	currentTable := config.TableDesc{
		Name:              "chunk_2591",
		UseOnDemandIOMode: false,
		ProvisionedRead:   0,
		ProvisionedWrite:  0,
		Tags:              nil,
	}
	expectedTable := config.TableDesc{
		Name:              "chunk_2591",
		UseOnDemandIOMode: false,
		ProvisionedRead:   300,
		ProvisionedWrite:  1,
		Tags:              nil,
	}

	err = tableClient.UpdateTable(context.Background(), currentTable, expectedTable)
	require.NoError(t, err)

	err = tableClient.DeleteTable(context.Background(), "chunk_2591")
	require.NoError(t, err)

	// rpc calls for storageClient
	storageClient, _ := NewTestStorageClient(cfg, schemaCfg)
	newChunkData := func() chunk.Data {
		return chunkenc.NewFacade(
			chunkenc.NewMemChunk(
				chunkenc.ChunkFormatV3, compression.None, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 0,
			), 0, 0)
	}

	putChunksTestData := []chunk.Chunk{
		{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(15993187966453505842),
				UserID:      "fake",
				From:        1587997054298,
				Through:     1587997054298,
				Checksum:    3651208117,
			},
			Metric: labels.Labels{
				{
					Name:  "_name_",
					Value: "prometheus_sd_file_scan_duration_seconds_sum",
				},
				{
					Name:  "instance",
					Value: "localhost:9090",
				},
				{
					Name:  "job",
					Value: "prometheus",
				},
			},
			Encoding: chunkenc.LogChunk,
			Data:     newChunkData(),
		},
	}
	err = storageClient.PutChunks(context.Background(), putChunksTestData)
	require.NoError(t, err)

	getChunksTestData := []chunk.Chunk{
		{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(15993187966453505842),
				UserID:      "fake",
				From:        1587997054298,
				Through:     1587997054298,
				Checksum:    3651208117,
			},
			Metric: labels.Labels{
				{
					Name:  "_name_",
					Value: "prometheus_sd_file_scan_duration_seconds_sum",
				},
				{
					Name:  "instance",
					Value: "localhost:9090",
				},
				{
					Name:  "job",
					Value: "prometheus",
				},
			},
			Encoding: chunkenc.LogChunk,
			Data:     newChunkData(),
		},
	}
	_, err = storageClient.GetChunks(context.Background(), getChunksTestData)
	require.NoError(t, err)

	err = storageClient.DeleteChunk(context.Background(), "", "")
	require.NoError(t, err)

	// rpc calls specific to indexClient
	writeBatchTestData := writeBatchTestData()
	err = storageClient.BatchWrite(context.Background(), writeBatchTestData)
	require.NoError(t, err)

	queries := []index.Query{
		{TableName: "table", HashValue: "foo"},
	}
	results := 0
	err = storageClient.QueryPages(context.Background(), queries, func(_ index.Query, batch index.ReadBatchResult) bool {
		iter := batch.Iterator()
		for iter.Next() {
			results++
		}
		return true
	})
	require.NoError(t, err)
}

func writeBatchTestData() index.WriteBatch {
	t := &WriteBatch{
		Writes: []*IndexEntry{
			{
				TableName:  "index_2625",
				HashValue:  "fake:d18381:5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4",
				RangeValue: []byte("JSI0YbyRLVmLKkLBiAKf5ctf8mWtn9U6CXCzuYmWkMk 5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4  8"),
				Value:      []byte("localhost:9090"),
			},
		},
		Deletes: []*IndexEntry{
			{
				TableName:  "index_2625",
				HashValue:  "fake:d18381:5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4",
				RangeValue: []byte("JSI0YbyRLVmLKkLBiAKf5ctf8mWtn9U6CXCzuYmWkMk 5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4  8"),
				Value:      nil,
			},
		},
	}
	return t
}
