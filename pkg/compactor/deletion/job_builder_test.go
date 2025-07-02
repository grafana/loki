package deletion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
)

// tableUpdatesRecorder collects all the storage updates we get post processing of a manifest
type tableUpdatesRecorder struct {
	updates map[string]map[string]map[string]storageUpdates
}

func (t *tableUpdatesRecorder) addStorageUpdates(tableName, userID, labels string, chunksToDelete []string, chunksToDeIndex []string, chunksToIndex []Chunk) error {
	if _, ok := t.updates[tableName]; !ok {
		t.updates[tableName] = map[string]map[string]storageUpdates{
			userID: {
				labels: {},
			},
		}
	}
	if _, ok := t.updates[tableName][userID]; !ok {
		t.updates[tableName][userID] = map[string]storageUpdates{
			labels: {},
		}
	}

	updates := t.updates[tableName][userID][labels]

	updates.ChunksToDelete = append(updates.ChunksToDelete, chunksToDelete...)
	updates.ChunksToDeIndex = append(updates.ChunksToDeIndex, chunksToDeIndex...)
	updates.ChunksToIndex = append(updates.ChunksToIndex, chunksToIndex...)

	t.updates[tableName][userID][labels] = updates

	return nil
}

func TestJobBuilder_buildJobs(t *testing.T) {
	now := model.Now()

	for _, tc := range []struct {
		name                 string
		setupManifest        func(client client.ObjectClient) []DeleteRequest
		expectedJobs         []grpc.Job
		expectedTableUpdates map[string]map[string]map[string]storageUpdates
	}{
		{
			name: "no manifests in storage",
			setupManifest: func(_ client.ObjectClient) []DeleteRequest {
				return []DeleteRequest{}
			},
			expectedTableUpdates: map[string]map[string]map[string]storageUpdates{},
		},
		{
			name: "one manifest in storage with less than maxChunksPerJob",
			setupManifest: func(client client.ObjectClient) []DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				requestsToAdd := []DeleteRequest{
					{
						RequestID: req1,
						UserID:    user1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   now,
					},
				}

				for i := range requestsToAdd {
					req := requestsToAdd[i]
					deleteRequestBatch.addDeleteRequest(&req)
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, *deleteRequestBatch)
				require.NoError(t, err)

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table1, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFooBar),
					chunks: buildRetentionChunks(0, maxChunksPerJob-1),
				}))

				require.NoError(t, manifestBuilder.Finish(context.Background()))

				return requestsToAdd
			},
			expectedJobs: []grpc.Job{
				{
					Id:   "0_0",
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(0, maxChunksPerJob-1)),
						DeleteRequests: []DeleteRequest{
							{
								RequestID: req1,
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
			},
			expectedTableUpdates: map[string]map[string]map[string]storageUpdates{
				table1: {
					user1: {
						lblFooBar: buildStorageUpdates(0, 1),
					},
				},
			},
		},
		{
			name: "one manifest in storage with more than maxChunksPerJob",
			setupManifest: func(client client.ObjectClient) []DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				requestsToAdd := []DeleteRequest{
					{
						RequestID: req1,
						UserID:    user1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   now,
					},
				}
				for i := range requestsToAdd {
					req := requestsToAdd[i]
					deleteRequestBatch.addDeleteRequest(&req)
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, *deleteRequestBatch)
				require.NoError(t, err)

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table1, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFooBar),
					chunks: buildRetentionChunks(0, maxChunksPerJob+1),
				}))

				require.NoError(t, manifestBuilder.Finish(context.Background()))

				return requestsToAdd
			},
			expectedJobs: []grpc.Job{
				{
					Id:   "0_0",
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(0, maxChunksPerJob)),
						DeleteRequests: []DeleteRequest{
							{
								RequestID: req1,
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
				{
					Id:   "0_1",
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(maxChunksPerJob, 1)),
						DeleteRequests: []DeleteRequest{
							{
								RequestID: req1,
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
			},
			expectedTableUpdates: map[string]map[string]map[string]storageUpdates{
				table1: {
					user1: {
						lblFooBar: buildStorageUpdates(0, 2),
					},
				},
			},
		},
		{
			name: "one manifest in storage with multiple groups",
			setupManifest: func(client client.ObjectClient) []DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				requestsToAdd := []DeleteRequest{
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
				}
				for i := range requestsToAdd {
					req := requestsToAdd[i]
					deleteRequestBatch.addDeleteRequest(&req)
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, *deleteRequestBatch)
				require.NoError(t, err)

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table1, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFizzBuzzAndFooBar),
					chunks: buildRetentionChunks(25, 50),
				}))

				require.NoError(t, manifestBuilder.Finish(context.Background()))

				return requestsToAdd
			},
			expectedJobs: []grpc.Job{
				{
					Id:   "0_0",
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(25, 25)),
						DeleteRequests: []DeleteRequest{
							{
								UserID:    user1,
								RequestID: req1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   100,
							},
						},
					}),
				},
				{
					Id:   "1_0",
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(50, 25)),
						DeleteRequests: []DeleteRequest{
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
					}),
				},
			},
			expectedTableUpdates: map[string]map[string]map[string]storageUpdates{
				table1: {
					user1: {
						lblFizzBuzzAndFooBar: buildStorageUpdates(0, 2),
					},
				},
			},
		},
		{
			name: "one manifest in storage with multiple segments due to multiple tables",
			setupManifest: func(client client.ObjectClient) []DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				requestsToAdd := []DeleteRequest{
					{
						RequestID: req1,
						UserID:    user1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   now,
					},
				}
				for i := range requestsToAdd {
					req := requestsToAdd[i]
					deleteRequestBatch.addDeleteRequest(&req)
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, *deleteRequestBatch)
				require.NoError(t, err)

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table1, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFooBar),
					chunks: buildRetentionChunks(0, 100),
				}))

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table2, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFooBar),
					chunks: buildRetentionChunks(100, 100),
				}))

				require.NoError(t, manifestBuilder.Finish(context.Background()))
				return requestsToAdd
			},
			expectedJobs: []grpc.Job{
				{
					Id:   "0_0",
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(0, 100)),
						DeleteRequests: []DeleteRequest{
							{
								RequestID: req1,
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
				{
					Id:   "0_0",
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionJob{
						TableName: table2,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(100, 100)),
						DeleteRequests: []DeleteRequest{
							{
								RequestID: req1,
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
			},
			expectedTableUpdates: map[string]map[string]map[string]storageUpdates{
				table1: {
					user1: {
						lblFooBar: buildStorageUpdates(0, 1),
					},
				},
				table2: {
					user1: {
						lblFooBar: buildStorageUpdates(1, 1),
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objectClient, err := local.NewFSObjectClient(local.FSConfig{
				Directory: t.TempDir(),
			})
			require.NoError(t, err)
			addedRequests := tc.setupManifest(objectClient)
			requestsMarkedAsProcessed := []DeleteRequest{}
			tableUpdatesRecorder := &tableUpdatesRecorder{
				updates: map[string]map[string]map[string]storageUpdates{},
			}

			builder := NewJobBuilder(objectClient, func(_ context.Context, iterator StorageUpdatesIterator) error {
				for iterator.Next() {
					if err := iterator.ForEachSeries(func(labels string, chunksToDelete []string, chunksToDeIndex []string, chunksToIndex []Chunk) error {
						return tableUpdatesRecorder.addStorageUpdates(iterator.TableName(), iterator.UserID(), labels, chunksToDelete, chunksToDeIndex, chunksToIndex)
					}); err != nil {
						return err
					}
				}

				return iterator.Err()
			}, func(requests []DeleteRequest) error {
				requestsMarkedAsProcessed = requests
				return nil
			})
			jobsChan := make(chan *grpc.Job)

			var jobsBuilt []grpc.Job
			go func() {
				cnt := 0
				for job := range jobsChan {
					jobsBuilt = append(jobsBuilt, *job)
					err := builder.OnJobResponse(&grpc.JobResult{
						JobId:   job.Id,
						JobType: job.Type,
						Result:  mustMarshal(t, buildStorageUpdates(cnt, 1)),
					})
					require.NoError(t, err)
					cnt++
				}
			}()

			err = builder.buildJobs(context.Background(), jobsChan)
			require.NoError(t, err)

			require.Equal(t, len(tc.expectedJobs), len(jobsBuilt))
			require.Equal(t, tc.expectedJobs, jobsBuilt)

			// verify operations and data post-processing of the manifests
			require.Equal(t, tc.expectedTableUpdates, tableUpdatesRecorder.updates)
			slices.SortFunc(requestsMarkedAsProcessed, func(a, b DeleteRequest) int {
				if len(a.RequestID) < len(b.RequestID) {
					return -1
				} else if len(a.RequestID) > len(b.RequestID) {
					return 1
				}

				return 0
			})
			require.Equal(t, addedRequests, requestsMarkedAsProcessed)

			// we should have cleaned up all the manifests from storage
			_, commonPrefixes, err := objectClient.List(context.Background(), "", "/")
			require.NoError(t, err)
			require.Equal(t, 0, len(commonPrefixes))
		})
	}
}

func TestJobBuilder_ProcessManifest(t *testing.T) {
	for _, tc := range []struct {
		name               string
		jobResult          []byte
		jobProcessingError string
	}{
		{
			name:      "all jobs succeeded",
			jobResult: []byte(`{}`),
		}, {
			name:               "job failure should fail the manifest processing",
			jobProcessingError: "job processing failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objectClient, err := local.NewFSObjectClient(local.FSConfig{
				Directory: t.TempDir(),
			})
			require.NoError(t, err)

			builder := NewJobBuilder(objectClient, func(_ context.Context, _ StorageUpdatesIterator) error {
				return nil
			}, func(_ []DeleteRequest) error {
				return nil
			})

			// Create a test manifest
			manifest := &manifest{
				SegmentsCount: 1,
			}
			manifestData, err := json.Marshal(manifest)
			require.NoError(t, err)
			err = objectClient.PutObject(context.Background(), "test-manifest/manifest.json", bytes.NewReader(manifestData))
			require.NoError(t, err)

			// Create a test segment
			segment := &segment{
				UserID:    "user1",
				TableName: "table1",
				ChunksGroups: []ChunksGroup{
					{
						Chunks: map[string][]string{"": {"chunk1", "chunk2"}},
						Requests: []DeleteRequest{
							{Query: "{job=\"test\"}"},
						},
					},
				},
			}
			segmentData, err := json.Marshal(segment)
			require.NoError(t, err)
			err = objectClient.PutObject(context.Background(), "test-manifest/0.json", bytes.NewReader(segmentData))
			require.NoError(t, err)

			jobsChan := make(chan *grpc.Job)
			go func() {
				for job := range jobsChan {
					err := builder.OnJobResponse(&grpc.JobResult{
						JobId:   job.Id,
						JobType: job.Type,
						Result:  tc.jobResult,
						Error:   tc.jobProcessingError,
					})
					require.NoError(t, err)
				}
			}()

			err = builder.processManifest(context.Background(), manifest, "test-manifest", jobsChan)
			if tc.jobProcessingError != "" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func mustMarshalPayload(job *deletionJob) []byte {
	payload, err := json.Marshal(job)
	if err != nil {
		panic(err)
	}

	return payload
}

func buildStorageUpdates(jobNumStart, numJobs int) storageUpdates {
	s := storageUpdates{}
	for i := 0; i < numJobs; i++ {
		jobNum := jobNumStart + i
		s.ChunksToDelete = append(s.ChunksToDelete, fmt.Sprintf("%d-d", jobNum))
		s.ChunksToDeIndex = append(s.ChunksToDeIndex, fmt.Sprintf("%d-i", jobNum))
		s.ChunksToIndex = append(s.ChunksToIndex, Chunk{
			From:        model.Time(jobNum),
			Through:     model.Time(jobNum),
			Fingerprint: uint64(jobNum),
			Checksum:    uint32(jobNum),
			KB:          uint32(jobNum),
			Entries:     uint32(jobNum),
		})
	}

	return s
}
