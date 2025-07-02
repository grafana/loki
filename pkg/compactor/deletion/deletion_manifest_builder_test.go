package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
)

const (
	req1                 = "req1"
	req2                 = "req2"
	table1               = "table1"
	table2               = "table2"
	lblFizzBuzz          = `{fizz="buzz"}`
	lblFizzBuzzAndFooBar = `{fizz="buzz", foo="bar"}`
)

func buildRetentionChunks(start model.Time, count int) []retention.Chunk {
	chunks := make([]retention.Chunk, 0, count)
	chunks = append(chunks, retention.Chunk{
		ChunkID: fmt.Sprintf("%d", start),
		From:    start,
		Through: start + 1,
	})

	for i := 1; i < count; i++ {
		from := chunks[i-1].From + 1
		chunks = append(chunks, retention.Chunk{
			ChunkID: fmt.Sprintf("%d", from),
			From:    from,
			Through: from + 1,
		})
	}

	return chunks
}

func getChunkIDsFromRetentionChunks(chunks []retention.Chunk) []string {
	chunkIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunkIDs = append(chunkIDs, chunk.ChunkID)
	}

	return chunkIDs
}

type mockSeries struct {
	seriesID []byte
	userID   string
	labels   labels.Labels
	chunks   []retention.Chunk
}

func (m *mockSeries) SeriesID() []byte {
	return m.seriesID
}

func (m *mockSeries) Reset(seriesID, userID []byte, labels labels.Labels) {
	m.seriesID = seriesID
	m.userID = string(userID)
	m.labels = labels
	m.chunks = nil
}

func (m *mockSeries) AppendChunks(ref ...retention.Chunk) {
	m.chunks = append(m.chunks, ref...)
}

func (m *mockSeries) UserID() []byte {
	return []byte(m.userID)
}

func (m *mockSeries) Labels() labels.Labels {
	return m.labels
}

func (m *mockSeries) Chunks() []retention.Chunk {
	return m.chunks
}

func TestDeletionManifestBuilder(t *testing.T) {
	tests := []struct {
		name           string
		deleteRequests []DeleteRequest
		series         []struct {
			tableName string
			series    *mockSeries
		}
		expectedManifest manifest
		expectedSegments []segment
		validateFunc     func(t *testing.T, builder *deletionManifestBuilder)
	}{
		{
			name: "single user with single segment",
			deleteRequests: []DeleteRequest{
				{
					UserID:    user1,
					RequestID: req1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   100,
				},
			},
			series: []struct {
				tableName string
				series    *mockSeries
			}{
				{
					tableName: table1,
					series: &mockSeries{
						userID: user1,
						labels: mustParseLabel(lblFooBar),
						chunks: buildRetentionChunks(10, 100),
					},
				},
			},
			expectedManifest: manifest{
				Requests: []DeleteRequest{
					{
						UserID:    user1,
						RequestID: req1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   100,
					},
				},
				SegmentsCount: 1,
				ChunksCount:   91,
			},
			expectedSegments: []segment{
				{
					UserID:    user1,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   100,
								},
							},
							Chunks: map[string][]string{
								lblFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(10, 91)),
							},
						},
					},
					ChunksCount: 91,
				},
			},
		},
		{
			name: "single user with multiple segments due to chunks count",
			deleteRequests: []DeleteRequest{
				{
					UserID:    user1,
					RequestID: req1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   maxChunksPerSegment + 1,
				},
			},
			series: []struct {
				tableName string
				series    *mockSeries
			}{
				{
					tableName: table1,
					series: &mockSeries{
						userID: user1,
						labels: mustParseLabel(lblFooBar),
						chunks: buildRetentionChunks(0, maxChunksPerSegment+1),
					},
				},
			},
			expectedManifest: manifest{
				Requests: []DeleteRequest{
					{
						UserID:    user1,
						RequestID: req1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   maxChunksPerSegment + 1,
					},
				},
				SegmentsCount: 2,
				ChunksCount:   maxChunksPerSegment + 1,
			},
			expectedSegments: []segment{
				{
					UserID:    user1,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   maxChunksPerSegment + 1,
								},
							},
							Chunks: map[string][]string{
								lblFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(0, maxChunksPerSegment)),
							},
						},
					},
					ChunksCount: maxChunksPerSegment,
				},
				{
					UserID:    user1,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   maxChunksPerSegment + 1,
								},
							},
							Chunks: map[string][]string{
								lblFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(maxChunksPerSegment, 1)),
							},
						},
					},
					ChunksCount: 1,
				},
			},
		},
		{
			name: "single user with multiple segments due to multiple tables having chunks to delete",
			deleteRequests: []DeleteRequest{
				{
					UserID:    user1,
					RequestID: req1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   100,
				},
			},
			series: []struct {
				tableName string
				series    *mockSeries
			}{
				{
					tableName: table1,
					series: &mockSeries{
						userID: user1,
						labels: mustParseLabel(lblFooBar),
						chunks: buildRetentionChunks(0, 50),
					},
				},
				{
					tableName: table2,
					series: &mockSeries{
						userID: user1,
						labels: mustParseLabel(lblFooBar),
						chunks: buildRetentionChunks(50, 50),
					},
				},
			},
			expectedManifest: manifest{
				Requests: []DeleteRequest{
					{
						UserID:    user1,
						RequestID: req1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   100,
					},
				},
				SegmentsCount: 2,
				ChunksCount:   100,
			},
			expectedSegments: []segment{
				{
					UserID:    user1,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   100,
								},
							},
							Chunks: map[string][]string{
								lblFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(0, 50)),
							},
						},
					},
					ChunksCount: 50,
				},
				{
					UserID:    user1,
					TableName: table2,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   100,
								},
							},
							Chunks: map[string][]string{
								lblFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(50, 50)),
							},
						},
					},
					ChunksCount: 50,
				},
			},
		},
		{
			name: "multiple users with multiple segments",
			deleteRequests: []DeleteRequest{
				{
					UserID:    user1,
					RequestID: req1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   maxChunksPerSegment + 1,
				},
				{
					UserID:    user2,
					RequestID: req2,
					Query:     lblFizzBuzz,
					StartTime: 10,
					EndTime:   10 + maxChunksPerSegment + 1,
				},
			},
			series: []struct {
				tableName string
				series    *mockSeries
			}{
				{
					tableName: table1,
					series: &mockSeries{
						userID: user1,
						labels: mustParseLabel(lblFooBar),
						chunks: buildRetentionChunks(0, maxChunksPerSegment+1),
					},
				},
				{
					tableName: table1,
					series: &mockSeries{
						userID: user2,
						labels: mustParseLabel(lblFizzBuzz),
						chunks: buildRetentionChunks(10, maxChunksPerSegment+1),
					},
				},
			},
			expectedManifest: manifest{
				Requests: []DeleteRequest{
					{
						UserID:    user1,
						RequestID: req1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   maxChunksPerSegment + 1,
					},
					{
						UserID:    user2,
						RequestID: req2,
						Query:     lblFizzBuzz,
						StartTime: 10,
						EndTime:   10 + maxChunksPerSegment + 1,
					},
				},
				SegmentsCount: 4,
				ChunksCount:   (maxChunksPerSegment + 1) * 2,
			},
			expectedSegments: []segment{
				{
					UserID:    user1,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   maxChunksPerSegment + 1,
								},
							},
							Chunks: map[string][]string{
								lblFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(0, maxChunksPerSegment)),
							},
						},
					},
					ChunksCount: maxChunksPerSegment,
				},
				{
					UserID:    user1,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   maxChunksPerSegment + 1,
								},
							},
							Chunks: map[string][]string{
								lblFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(maxChunksPerSegment, 1)),
							},
						},
					},
					ChunksCount: 1,
				},
				{
					UserID:    user2,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user2,
									RequestID: req2,
									Query:     lblFizzBuzz,
									StartTime: 10,
									EndTime:   10 + maxChunksPerSegment + 1,
								},
							},
							Chunks: map[string][]string{
								lblFizzBuzz: getChunkIDsFromRetentionChunks(buildRetentionChunks(10, maxChunksPerSegment)),
							},
						},
					},
					ChunksCount: maxChunksPerSegment,
				},
				{
					UserID:    user2,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user2,
									RequestID: req2,
									Query:     lblFizzBuzz,
									StartTime: 10,
									EndTime:   10 + maxChunksPerSegment + 1,
								},
							},
							Chunks: map[string][]string{
								lblFizzBuzz: getChunkIDsFromRetentionChunks(buildRetentionChunks(10+maxChunksPerSegment, 1)),
							},
						},
					},
					ChunksCount: 1,
				},
			},
		},
		{
			name: "multiple delete requests covering same chunks",
			deleteRequests: []DeleteRequest{
				{
					UserID:    user1,
					RequestID: req1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   100,
				},
				{
					UserID:    user1,
					RequestID: req2,
					Query:     lblFizzBuzz,
					StartTime: 51,
					EndTime:   100,
				},
			},
			series: []struct {
				tableName string
				series    *mockSeries
			}{
				{
					tableName: table1,
					series: &mockSeries{
						userID: user1,
						labels: mustParseLabel(lblFizzBuzzAndFooBar),
						chunks: buildRetentionChunks(25, 50),
					},
				},
			},
			expectedManifest: manifest{
				Requests: []DeleteRequest{
					{
						UserID:    user1,
						RequestID: req1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   100,
					},
					{
						UserID:    user1,
						RequestID: req2,
						Query:     lblFizzBuzz,
						StartTime: 51,
						EndTime:   100,
					},
				},
				SegmentsCount: 1,
				ChunksCount:   50,
			},
			expectedSegments: []segment{
				{
					UserID:    user1,
					TableName: table1,
					ChunksGroups: []ChunksGroup{
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   100,
								},
							},
							Chunks: map[string][]string{
								lblFizzBuzzAndFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(25, 25)),
							},
						},
						{
							Requests: []DeleteRequest{
								{
									UserID:    user1,
									RequestID: req1,
									Query:     lblFooBar,
									StartTime: 0,
									EndTime:   100,
								},
								{
									UserID:    user1,
									RequestID: req2,
									Query:     lblFizzBuzz,
									StartTime: 51,
									EndTime:   100,
								},
							},
							Chunks: map[string][]string{
								lblFizzBuzzAndFooBar: getChunkIDsFromRetentionChunks(buildRetentionChunks(50, 25)),
							},
						},
					},

					ChunksCount: 50,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			ctx := context.Background()
			objectClient, err := local.NewFSObjectClient(local.FSConfig{
				Directory: tempDir,
			})
			require.NoError(t, err)

			// Create delete request batch
			batch := newDeleteRequestBatch(nil)
			for _, req := range tc.deleteRequests {
				batch.addDeleteRequest(&req)
			}

			// Create builder
			builder, err := newDeletionManifestBuilder(objectClient, *batch)
			require.NoError(t, err)

			// Process series
			for _, s := range tc.series {
				err := builder.AddSeries(ctx, s.tableName, s.series)
				require.NoError(t, err)
			}

			// Finish and validate
			err = builder.Finish(ctx)
			require.NoError(t, err)

			require.Equal(t, tc.expectedManifest.SegmentsCount, builder.segmentsCount)
			require.Equal(t, tc.expectedManifest.ChunksCount, builder.overallChunksCount)

			reader, _, err := builder.deleteStoreClient.GetObject(context.Background(), builder.buildObjectKey(manifestFileName))
			require.NoError(t, err)

			manifestJSON, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())

			var manifest manifest
			require.NoError(t, json.Unmarshal(manifestJSON, &manifest))
			slices.SortFunc(manifest.Requests, func(a, b DeleteRequest) int {
				return strings.Compare(a.RequestID, b.RequestID)
			})

			require.Equal(t, tc.expectedManifest, manifest)

			for i := 0; i < tc.expectedManifest.SegmentsCount; i++ {
				reader, _, err := builder.deleteStoreClient.GetObject(context.Background(), builder.buildObjectKey(fmt.Sprintf("%d.json", i)))
				require.NoError(t, err)

				segmentJSON, err := io.ReadAll(reader)
				require.NoError(t, err)
				require.NoError(t, reader.Close())

				var segment segment
				require.NoError(t, json.Unmarshal(segmentJSON, &segment))

				slices.SortFunc(segment.ChunksGroups, func(a, b ChunksGroup) int {
					switch {
					case len(a.Requests) < len(b.Requests):
						return -1
					case len(a.Requests) > len(b.Requests):
						return 1
					default:
						return 0
					}
				})
				require.Equal(t, tc.expectedSegments[i], segment)
			}
		})
	}
}

func TestDeletionManifestBuilder_Errors(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()
	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: tempDir,
	})
	require.NoError(t, err)

	// Create delete request batch
	batch := newDeleteRequestBatch(nil)
	batch.addDeleteRequest(&DeleteRequest{
		UserID:    user1,
		RequestID: req1,
		Query:     lblFooBar,
		StartTime: 0,
		EndTime:   100,
	})

	// Create builder
	builder, err := newDeletionManifestBuilder(objectClient, *batch)
	require.NoError(t, err)

	err = builder.AddSeries(ctx, table1, &mockSeries{
		userID: user2,
		labels: mustParseLabel(lblFooBar),
		chunks: buildRetentionChunks(0, 25),
	})
	require.EqualError(t, err, fmt.Sprintf("no requests loaded for user: %s", user2))

	err = builder.Finish(ctx)
	require.EqualError(t, err, ErrNoChunksSelectedForDeletion.Error())
}
