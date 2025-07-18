package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	storage_chunk "github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"

	"github.com/grafana/loki/pkg/push"
)

type mockChunkClient struct {
	client.Client
	chunks map[string]storage_chunk.Chunk
	mtx    sync.RWMutex
}

func (m *mockChunkClient) GetChunks(_ context.Context, chunks []storage_chunk.Chunk) ([]storage_chunk.Chunk, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	var result []storage_chunk.Chunk
	for _, chk := range chunks {
		if storedChk, ok := m.chunks[chunkKey(chk)]; ok {
			result = append(result, storedChk)
		}
	}
	return result, nil
}

func (m *mockChunkClient) PutChunks(_ context.Context, chunks []storage_chunk.Chunk) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	for _, chk := range chunks {
		m.chunks[chunkKey(chk)] = chk
	}
	return nil
}

// Helper to convert prometheus labels.Labels to push.LabelsAdapter
func toLabelsAdapter(lbls labels.Labels) push.LabelsAdapter {
	out := make(push.LabelsAdapter, 0, len(lbls))
	for _, l := range lbls {
		out = append(out, push.LabelAdapter{Name: l.Name, Value: l.Value})
	}
	return out
}

// Helper to generate a unique key for a chunk (for test map)
func chunkKey(c storage_chunk.Chunk) string {
	return fmt.Sprintf("%s/%x/%x:%x:%x", c.UserID, c.Fingerprint, int64(c.From), int64(c.Through), c.Checksum)
}

// Helper to create a test chunk with data
func createTestChunk(t *testing.T, userID string, lbs labels.Labels, from, through model.Time, logLine string, structuredMetadata push.LabelsAdapter, logInterval time.Duration) storage_chunk.Chunk {
	t.Helper()
	const (
		targetSize = 1500 * 1024
		blockSize  = 256 * 1024
	)
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(labels.MetricName, "logs")
	metric := labelsBuilder.Labels()
	fp := model.Fingerprint(labels.StableHash(lbs))
	chunkEnc := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.None, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, targetSize)

	for ts := from; !ts.After(through); ts = ts.Add(logInterval) {
		dup, err := chunkEnc.Append(&logproto.Entry{
			Timestamp:          ts.Time(),
			Line:               logLine,
			StructuredMetadata: structuredMetadata,
		})
		require.False(t, dup)
		require.NoError(t, err)
	}

	require.NoError(t, chunkEnc.Close())
	c := storage_chunk.NewChunk(userID, fp, metric, chunkenc.NewFacade(chunkEnc, blockSize, targetSize), from, through)
	require.NoError(t, c.Encode())
	return c
}

func TestJobRunner_Run(t *testing.T) {
	now := model.Now()
	userID := "test-user"
	yesterdaysTableNumber := (now.Unix() / 86400) - 1
	tableName := fmt.Sprintf("table_%d", yesterdaysTableNumber)
	yesterdaysTableInterval := retention.ExtractIntervalFromTableName(tableName)

	// Create test labels
	lblFoo, err := syntax.ParseLabels(`{foo="bar"}`)
	require.NoError(t, err)

	chk := createTestChunk(t, userID, lblFoo, yesterdaysTableInterval.Start.Add(-6*time.Hour), yesterdaysTableInterval.Start.Add(6*time.Hour), "test data", toLabelsAdapter(labels.FromStrings("foo", "bar")), time.Minute)

	for _, tc := range []struct {
		name           string
		deleteRequests []DeleteRequest
		expectedResult *storageUpdates
		expectError    bool
	}{
		{
			name: "delete request without line filter should fail",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-5",
					Query:     lblFoo.String(),
					StartTime: yesterdaysTableInterval.Start.Add(-7 * time.Hour),
					EndTime:   yesterdaysTableInterval.Start.Add(time.Hour),
				},
			},
			expectError: true,
		},
		{
			name: "delete request not matching any data",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-1",
					Query:     `{foo="different"} |= "test"`,
					StartTime: yesterdaysTableInterval.Start.Add(-12 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResult: &storageUpdates{},
		},
		{
			name: "single delete request deleting some data",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-2",
					Query:     lblFoo.String() + ` |= "test"`,
					StartTime: yesterdaysTableInterval.Start.Add(-7 * time.Hour),
					EndTime:   yesterdaysTableInterval.Start.Add(time.Hour),
				},
			},
			expectedResult: &storageUpdates{
				ChunksToDelete: []string{chunkKey(chk)},
				ChunksToIndex: []chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: labels.StableHash(lblFoo),
					},
				},
			},
		},
		{
			name: "multiple delete requests deleting data",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-3",
					Query:     lblFoo.String() + ` |= "test"`,
					StartTime: yesterdaysTableInterval.Start.Add(-7 * time.Hour),
					EndTime:   yesterdaysTableInterval.Start,
				},
				{
					RequestID: "test-request-4",
					Query:     lblFoo.String() + ` |= "data"`,
					StartTime: yesterdaysTableInterval.Start,
					EndTime:   yesterdaysTableInterval.Start.Add(time.Hour),
				},
			},
			expectedResult: &storageUpdates{
				ChunksToDelete: []string{chunkKey(chk)},
				ChunksToIndex: []chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: labels.StableHash(lblFoo),
					},
				},
			},
		},
		{
			name: "delete request with time range outside chunk time range",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-6",
					Query:     lblFoo.String() + ` |= "test"`,
					StartTime: yesterdaysTableInterval.Start.Add(-24 * time.Hour),
					EndTime:   yesterdaysTableInterval.Start.Add(-23 * time.Hour),
				},
			},
			expectedResult: &storageUpdates{},
		},
		{
			name: "delete request with structured metadata filter",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-7",
					Query:     lblFoo.String() + ` | foo="bar"`,
					StartTime: yesterdaysTableInterval.Start.Add(-7 * time.Hour),
					EndTime:   yesterdaysTableInterval.Start.Add(2 * time.Hour),
				},
			},
			expectedResult: &storageUpdates{
				ChunksToDelete: []string{chunkKey(chk)},
				ChunksToIndex: []chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(2 * time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: labels.StableHash(lblFoo),
					},
				},
			},
		},
		{
			name: "delete request with line and structured metadata filters",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-8",
					Query:     lblFoo.String() + ` | foo="bar" |= "test"`,
					StartTime: yesterdaysTableInterval.Start.Add(-7 * time.Hour),
					EndTime:   yesterdaysTableInterval.Start.Add(3 * time.Hour),
				},
			},
			expectedResult: &storageUpdates{
				ChunksToDelete: []string{chunkKey(chk)},
				ChunksToIndex: []chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(3 * time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: labels.StableHash(lblFoo),
					},
				},
			},
		},
		{
			name: "delete request deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-8",
					Query:     lblFoo.String() + ` |= "test"`,
					StartTime: yesterdaysTableInterval.Start.Add(-7 * time.Hour),
					EndTime:   yesterdaysTableInterval.Start.Add(7 * time.Hour),
				},
			},
			expectedResult: &storageUpdates{
				ChunksToDelete: []string{chunkKey(chk)},
			},
		},
		{
			name: "old chunk to be de-indexed when new chunk wont belong to the current table",
			deleteRequests: []DeleteRequest{
				{
					RequestID: "test-request-8",
					Query:     lblFoo.String() + ` |= "test"`,
					StartTime: yesterdaysTableInterval.Start,
					EndTime:   yesterdaysTableInterval.Start.Add(7 * time.Hour),
				},
			},
			expectedResult: &storageUpdates{
				ChunksToDeIndex: []string{chunkKey(chk)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock chunk client
			mockClient := &mockChunkClient{
				chunks: map[string]storage_chunk.Chunk{
					chunkKey(chk): chk,
				},
			}

			// Ensure Metrics is set for each DeleteRequest
			for i := range tc.deleteRequests {
				tc.deleteRequests[i].Metrics = newDeleteRequestsManagerMetrics(nil)
			}

			// Create job runner
			runner := NewJobRunner(1, func(_ string) (client.Client, error) {
				return mockClient, nil
			}, nil)

			// Create job
			job := grpc.Job{
				Id: "test-job",
				Payload: mustMarshal(t, deletionJob{
					UserID:         userID,
					TableName:      tableName,
					ChunkIDs:       []string{chunkKey(chk)},
					DeleteRequests: tc.deleteRequests,
				}),
			}

			// Run job
			resultJSON, err := runner.Run(context.Background(), &job)

			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			result := &storageUpdates{}
			require.NoError(t, json.Unmarshal(resultJSON, result))

			// For test cases where we expect no changes
			if len(tc.expectedResult.ChunksToDelete) == 0 && len(tc.expectedResult.ChunksToIndex) == 0 {
				require.Equal(t, tc.expectedResult, result)
				return
			}

			// For test cases where we expect changes
			require.Equal(t, tc.expectedResult.ChunksToDelete, result.ChunksToDelete)
			require.Equal(t, len(tc.expectedResult.ChunksToIndex), len(result.ChunksToIndex))
			if len(result.ChunksToIndex) > 0 {
				require.Equal(t, tc.expectedResult.ChunksToIndex[0].From, result.ChunksToIndex[0].From)
				require.Equal(t, tc.expectedResult.ChunksToIndex[0].Through, result.ChunksToIndex[0].Through)
				require.Equal(t, tc.expectedResult.ChunksToIndex[0].Fingerprint, result.ChunksToIndex[0].Fingerprint)
			}
		})
	}
}

func TestJobRunner_Run_ConcurrentChunkProcessing(t *testing.T) {
	now := model.Now()
	userID := "test-user"
	yesterdaysTableNumber := (now.Unix() / 86400) - 1
	tableName := fmt.Sprintf("table_%d", yesterdaysTableNumber)
	yesterdaysTableInterval := retention.ExtractIntervalFromTableName(tableName)

	// Create test labels
	lblFoo, err := syntax.ParseLabels(`{foo="bar"}`)
	require.NoError(t, err)

	var chks []storage_chunk.Chunk

	for i := 0; i < 24; i++ {
		chkStart := yesterdaysTableInterval.Start.Add(time.Duration(i) * time.Hour)
		chkEnd := chkStart.Add(time.Hour)
		chk := createTestChunk(t, userID, lblFoo, chkStart, chkEnd, "test data", toLabelsAdapter(labels.FromStrings("foo", "bar")), time.Minute)
		chks = append(chks, chk)
	}

	overlappingChunk := createTestChunk(t, userID, lblFoo, yesterdaysTableInterval.End.Add(-30*time.Minute), yesterdaysTableInterval.End.Add(30*time.Minute), "test data overlapping multiple tables", toLabelsAdapter(labels.FromStrings("foo", "bar")), time.Minute)
	chks = append(chks, overlappingChunk)

	chunksMap := map[string]storage_chunk.Chunk{}
	var chunkIDs []string
	for _, chk := range chks {
		chunkID := chunkKey(chk)
		chunksMap[chunkID] = chk
		chunkIDs = append(chunkIDs, chunkID)
	}

	// Setup mock chunk client
	mockClient := &mockChunkClient{
		chunks: chunksMap,
	}

	deleteRequests := []DeleteRequest{
		{
			// partially delete the first chunk
			RequestID: "test-request-0",
			Query:     lblFoo.String() + ` |= "test"`,
			StartTime: yesterdaysTableInterval.Start,
			EndTime:   yesterdaysTableInterval.Start.Add(30 * time.Minute),
		},
		{
			// Delete 4 complete chunks in the middle.
			// Since the chunks overlap by a minute, it would also cause deletion of one log line from the adjoining chunk on both ends.
			RequestID: "test-request-1",
			Query:     lblFoo.String() + ` |= "test"`,
			StartTime: yesterdaysTableInterval.Start.Add(6 * time.Hour),
			EndTime:   yesterdaysTableInterval.Start.Add(10 * time.Hour),
		},
		{
			// Delete the whole part of chunk which overlaps the yesterdays table which would cause chunk to get de-indexed
			RequestID: "test-request-2",
			Query:     lblFoo.String() + ` |= "overlapping"`,
			StartTime: yesterdaysTableInterval.End.Add(-30 * time.Minute),
			EndTime:   yesterdaysTableInterval.End,
		},
	}

	expectedStorageUpdates := &storageUpdates{
		ChunksToDelete: []string{
			chunkKey(chks[0]), // first partially deleted chunk to get removed by test-request-0
			chunkKey(chks[5]), // adjoining chunk to a bunch of chunks selected for deletion by test-request-1\

			chunkKey(chks[6]), ///////////////////////////////////////////////////////
			chunkKey(chks[7]), //   chunks selected for deletion by test-request-1	//
			chunkKey(chks[8]), //													//
			chunkKey(chks[9]), ///////////////////////////////////////////////////////

			chunkKey(chks[10]), // adjoining chunk to a bunch of chunks selected for deletion by test-request-1
		},
		ChunksToIndex: []chunk{
			{
				// chunk recreated by test-request-0
				From:        yesterdaysTableInterval.Start.Add(31 * time.Minute),
				Through:     yesterdaysTableInterval.Start.Add(time.Hour),
				Fingerprint: labels.StableHash(lblFoo),
			},
			{
				// chunk recreated by test-request-1, removing just last line
				From:        chks[5].From,
				Through:     chks[5].Through.Add(-time.Minute),
				Fingerprint: labels.StableHash(lblFoo),
			},
			{
				// chunk recreated by test-request-1, removing just first line
				From:        chks[10].From.Add(time.Minute),
				Through:     chks[10].Through,
				Fingerprint: labels.StableHash(lblFoo),
			},
		},
		ChunksToDeIndex: []string{
			chunkKey(overlappingChunk), // chunk de-indexed by test-request-2
		},
	}

	// Ensure Metrics is set for each DeleteRequest
	for i := range deleteRequests {
		deleteRequests[i].Metrics = newDeleteRequestsManagerMetrics(nil)
	}

	// Create job runner with chunk processing concurrency of 2
	runner := NewJobRunner(2, func(_ string) (client.Client, error) {
		return mockClient, nil
	}, nil)

	// Create the job
	job := grpc.Job{
		Id: "test-job",
		Payload: mustMarshal(t, deletionJob{
			UserID:         userID,
			TableName:      tableName,
			ChunkIDs:       chunkIDs,
			DeleteRequests: deleteRequests,
		}),
	}

	// Run the job
	resultJSON, err := runner.Run(context.Background(), &job)
	require.NoError(t, err)

	require.NoError(t, err)
	result := &storageUpdates{}
	require.NoError(t, json.Unmarshal(resultJSON, result))

	// verify we got the expected storage updates
	require.Equal(t, len(expectedStorageUpdates.ChunksToIndex), len(result.ChunksToIndex))

	slices.SortFunc(result.ChunksToIndex, func(a, b chunk) int {
		if a.From < b.From {
			return -1
		} else if a.From > b.From {
			return 1
		}

		return 0
	})
	for i := range expectedStorageUpdates.ChunksToIndex {
		require.Equal(t, expectedStorageUpdates.ChunksToIndex[i].From, result.ChunksToIndex[i].From)
		require.Equal(t, expectedStorageUpdates.ChunksToIndex[i].Through, result.ChunksToIndex[i].Through)
		require.Equal(t, expectedStorageUpdates.ChunksToIndex[i].Fingerprint, result.ChunksToIndex[i].Fingerprint)
	}
	slices.SortFunc(result.ChunksToDelete, func(a, b string) int {
		if a < b {
			return -1
		} else if a > b {
			return 1
		}

		return 0
	})
	require.Equal(t, expectedStorageUpdates.ChunksToDelete, result.ChunksToDelete)
	require.Equal(t, expectedStorageUpdates.ChunksToDeIndex, result.ChunksToDeIndex)
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
