package deletion

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"

	"github.com/grafana/loki/pkg/push"
)

type mockChunkClient struct {
	client.Client
	chunks map[string]chunk.Chunk
}

func (m *mockChunkClient) GetChunks(_ context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	var result []chunk.Chunk
	for _, chk := range chunks {
		if storedChk, ok := m.chunks[chunkKey(chk)]; ok {
			result = append(result, storedChk)
		}
	}
	return result, nil
}

func (m *mockChunkClient) PutChunks(_ context.Context, chunks []chunk.Chunk) error {
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
func chunkKey(c chunk.Chunk) string {
	return fmt.Sprintf("%s/%x/%x:%x:%x", c.UserID, c.Fingerprint, int64(c.From), int64(c.Through), c.Checksum)
}

// Helper to create a test chunk with data
func createTestChunk(t *testing.T, userID string, lbs labels.Labels, from, through model.Time, logLine string, structuredMetadata push.LabelsAdapter, logInterval time.Duration) chunk.Chunk {
	t.Helper()
	const (
		targetSize = 1500 * 1024
		blockSize  = 256 * 1024
	)
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(labels.MetricName, "logs")
	metric := labelsBuilder.Labels()
	fp := model.Fingerprint(lbs.Hash())
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
	c := chunk.NewChunk(userID, fp, metric, chunkenc.NewFacade(chunkEnc, blockSize, targetSize), from, through)
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
				ChunksToIndex: []Chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: lblFoo.Hash(),
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
				ChunksToIndex: []Chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: lblFoo.Hash(),
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
				ChunksToIndex: []Chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(2 * time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: lblFoo.Hash(),
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
				ChunksToIndex: []Chunk{
					{
						From:        yesterdaysTableInterval.Start.Add(3 * time.Hour).Add(time.Minute),
						Through:     yesterdaysTableInterval.Start.Add(6 * time.Hour),
						Fingerprint: lblFoo.Hash(),
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
				chunks: map[string]chunk.Chunk{
					chunkKey(chk): chk,
				},
			}

			// Ensure Metrics is set for each DeleteRequest
			for i := range tc.deleteRequests {
				tc.deleteRequests[i].Metrics = newDeleteRequestsManagerMetrics(nil)
			}

			// Create job runner
			runner := NewJobRunner(func(_ context.Context, _ string) (client.Client, error) {
				return mockClient, nil
			})

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
			resultJSON, err := runner.Run(context.Background(), job)

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

func mustMarshal(t *testing.T, v interface{}) []byte {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
