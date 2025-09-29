package deletion

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
)

// tableUpdatesRecorder collects all the storage updates we get post processing of a manifest
type tableUpdatesRecorder struct {
	updates map[string]map[string]map[string]deletionproto.StorageUpdates
}

func (t *tableUpdatesRecorder) addStorageUpdates(tableName, userID, labels string, rebuiltChunks map[string]Chunk, chunksToDeIndex []string) error {
	if _, ok := t.updates[tableName]; !ok {
		t.updates[tableName] = map[string]map[string]deletionproto.StorageUpdates{
			userID: {
				labels: {
					RebuiltChunks: map[string]*deletionproto.Chunk{},
				},
			},
		}
	}
	if _, ok := t.updates[tableName][userID]; !ok {
		t.updates[tableName][userID] = map[string]deletionproto.StorageUpdates{
			labels: {
				RebuiltChunks: map[string]*deletionproto.Chunk{},
			},
		}
	}

	updates := t.updates[tableName][userID][labels]

	for chunkID, newChunk := range rebuiltChunks {
		if newChunk == nil {
			updates.RebuiltChunks[chunkID] = nil
		} else {
			updates.RebuiltChunks[chunkID] = newChunk.(*deletionproto.Chunk)
		}
	}
	updates.ChunksToDeIndex = append(updates.ChunksToDeIndex, chunksToDeIndex...)
	t.updates[tableName][userID][labels] = updates

	return nil
}

func TestJobBuilder_buildJobs(t *testing.T) {
	now := model.Now()

	for _, tc := range []struct {
		name                 string
		setupManifest        func(client client.ObjectClient) []deletionproto.DeleteRequest
		expectedJobs         []grpc.Job
		expectedTableUpdates map[string]map[string]map[string]deletionproto.StorageUpdates
		wholeChunksDeleted   bool
	}{
		{
			name: "no manifests in storage",
			setupManifest: func(_ client.ObjectClient) []deletionproto.DeleteRequest {
				return []deletionproto.DeleteRequest{}
			},
			expectedTableUpdates: map[string]map[string]map[string]deletionproto.StorageUpdates{},
		},
		{
			name: "one manifest in storage with less than maxChunksPerJob",
			setupManifest: func(client client.ObjectClient) []deletionproto.DeleteRequest {
				metrics := newDeleteRequestsManagerMetrics(nil)
				deleteRequestBatch := newDeleteRequestBatch(metrics)
				requestsToAdd := []deletionproto.DeleteRequest{
					{
						RequestID: req1,
						UserID:    user1,
						Query:     lblFooBar,
						StartTime: 0,
						EndTime:   now,
					},
				}

				for i := range requestsToAdd {
					req, err := newDeleteRequest(requestsToAdd[i], metrics.deletedLinesTotal)
					require.NoError(t, err)
					deleteRequestBatch.addDeleteRequest(req)
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, deleteRequestBatch)
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(0, maxChunksPerJob-1)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
			expectedTableUpdates: map[string]map[string]map[string]deletionproto.StorageUpdates{
				table1: {
					user1: {
						lblFooBar: buildStorageUpdates(0, 1, false),
					},
				},
			},
		},
		{
			name: "one manifest in storage with more than maxChunksPerJob",
			setupManifest: func(client client.ObjectClient) []deletionproto.DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(newDeleteRequestsManagerMetrics(nil))
				requestsToAdd := []deletionproto.DeleteRequest{
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
					deleteRequestBatch.addDeleteRequest(&deleteRequest{DeleteRequest: req})
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, deleteRequestBatch)
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(0, maxChunksPerJob)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(maxChunksPerJob, 1)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
			expectedTableUpdates: map[string]map[string]map[string]deletionproto.StorageUpdates{
				table1: {
					user1: {
						lblFooBar: buildStorageUpdates(0, 2, false),
					},
				},
			},
		},
		{
			name: "one manifest in storage with multiple groups",
			setupManifest: func(client client.ObjectClient) []deletionproto.DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(newDeleteRequestsManagerMetrics(nil))
				requestsToAdd := []deletionproto.DeleteRequest{
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
					deleteRequestBatch.addDeleteRequest(&deleteRequest{DeleteRequest: req})
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, deleteRequestBatch)
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(25, 25)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(50, 25)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
			expectedTableUpdates: map[string]map[string]map[string]deletionproto.StorageUpdates{
				table1: {
					user1: {
						lblFizzBuzzAndFooBar: buildStorageUpdates(0, 2, false),
					},
				},
			},
		},
		{
			name: "one manifest in storage with multiple segments due to multiple tables",
			setupManifest: func(client client.ObjectClient) []deletionproto.DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(newDeleteRequestsManagerMetrics(nil))
				requestsToAdd := []deletionproto.DeleteRequest{
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
					deleteRequestBatch.addDeleteRequest(&deleteRequest{DeleteRequest: req})
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, deleteRequestBatch)
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(0, 100)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table2,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(100, 100)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
			expectedTableUpdates: map[string]map[string]map[string]deletionproto.StorageUpdates{
				table1: {
					user1: {
						lblFooBar: buildStorageUpdates(0, 1, false),
					},
				},
				table2: {
					user1: {
						lblFooBar: buildStorageUpdates(1, 1, false),
					},
				},
			},
		},
		{
			name:               "whole chunks deleted",
			wholeChunksDeleted: true,
			setupManifest: func(client client.ObjectClient) []deletionproto.DeleteRequest {
				deleteRequestBatch := newDeleteRequestBatch(newDeleteRequestsManagerMetrics(nil))
				requestsToAdd := []deletionproto.DeleteRequest{
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
					deleteRequestBatch.addDeleteRequest(&deleteRequest{DeleteRequest: req})
				}
				manifestBuilder, err := newDeletionManifestBuilder(client, deleteRequestBatch)
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table1,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(0, 100)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
					Type: grpc.JOB_TYPE_DELETION,
					Payload: mustMarshalPayload(&deletionproto.DeletionJob{
						TableName: table2,
						UserID:    user1,
						ChunkIDs:  getChunkIDsFromRetentionChunks(buildRetentionChunks(100, 100)),
						DeleteRequests: []deletionproto.DeleteRequest{
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
			expectedTableUpdates: map[string]map[string]map[string]deletionproto.StorageUpdates{
				table1: {
					user1: {
						lblFooBar: buildStorageUpdates(0, 1, true),
					},
				},
				table2: {
					user1: {
						lblFooBar: buildStorageUpdates(1, 1, true),
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
			requestsMarkedAsProcessed := []deletionproto.DeleteRequest{}
			tableUpdatesRecorder := &tableUpdatesRecorder{
				updates: map[string]map[string]map[string]deletionproto.StorageUpdates{},
			}

			builder := NewJobBuilder(objectClient, func(_ context.Context, iterator StorageUpdatesIterator) error {
				for iterator.Next() {
					if err := iterator.ForEachSeries(func(labels string, rebuiltChunks map[string]Chunk, chunksToDeIndex []string) error {
						return tableUpdatesRecorder.addStorageUpdates(iterator.TableName(), iterator.UserID(), labels, rebuiltChunks, chunksToDeIndex)
					}); err != nil {
						return err
					}
				}

				return iterator.Err()
			}, func(requests []deletionproto.DeleteRequest) {
				requestsMarkedAsProcessed = requests
			}, nil)
			jobsChan := make(chan *grpc.Job)

			var jobsBuilt []grpc.Job
			seenJobIDs := map[string]struct{}{}
			go func() {
				cnt := 0
				for job := range jobsChan {
					// ensure that we get unique job IDs
					jobID := job.Id
					if _, ok := seenJobIDs[jobID]; ok {
						t.Errorf("job id %s already seen", jobID)
						return
					}
					seenJobIDs[jobID] = struct{}{}

					// while comparing jobs built vs expected, do not compare the job IDs
					job.Id = ""
					jobsBuilt = append(jobsBuilt, *job)
					storageUpdates := buildStorageUpdates(cnt, 1, tc.wholeChunksDeleted)
					err := builder.OnJobResponse(&grpc.JobResult{
						JobId:   jobID,
						JobType: job.Type,
						Result:  mustMarshal(t, &storageUpdates),
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
			slices.SortFunc(requestsMarkedAsProcessed, func(a, b deletionproto.DeleteRequest) int {
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
			jobResult: []byte(``),
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
			}, func(_ []deletionproto.DeleteRequest) {}, nil)

			// Create a test manifest
			manifest := &deletionproto.DeletionManifest{
				SegmentsCount: 1,
			}
			manifestData, err := proto.Marshal(manifest)
			require.NoError(t, err)
			err = objectClient.PutObject(context.Background(), "test-manifest/manifest.proto", bytes.NewReader(manifestData))
			require.NoError(t, err)

			// Create a test segment
			segment := &deletionproto.Segment{
				UserID:    "user1",
				TableName: "table1",
				ChunksGroups: []deletionproto.ChunksGroup{
					{
						Chunks: map[string]deletionproto.ChunkIDs{"": {IDs: []string{"chunk1", "chunk2"}}},
						Requests: []deletionproto.DeleteRequest{
							{Query: "{job=\"test\"}"},
						},
					},
				},
			}
			segmentData, err := proto.Marshal(segment)
			require.NoError(t, err)
			err = objectClient.PutObject(context.Background(), "test-manifest/0.proto", bytes.NewReader(segmentData))
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

func mustMarshalPayload(job *deletionproto.DeletionJob) []byte {
	payload, err := proto.Marshal(job)
	if err != nil {
		panic(err)
	}

	return payload
}

func buildStorageUpdates(jobNumStart, numJobs int, wholeChunksDeleted bool) deletionproto.StorageUpdates {
	s := deletionproto.StorageUpdates{
		RebuiltChunks: map[string]*deletionproto.Chunk{},
	}
	for i := 0; i < numJobs; i++ {
		jobNum := jobNumStart + i
		chunkID := fmt.Sprintf("%d-d", jobNum)
		if wholeChunksDeleted {
			s.RebuiltChunks[chunkID] = nil
		} else {
			s.RebuiltChunks[chunkID] = &deletionproto.Chunk{
				From:        model.Time(jobNum),
				Through:     model.Time(jobNum),
				Fingerprint: uint64(jobNum),
				Checksum:    uint32(jobNum),
				KB:          uint32(jobNum),
				Entries:     uint32(jobNum),
			}
		}
		s.ChunksToDeIndex = append(s.ChunksToDeIndex, fmt.Sprintf("%d-i", jobNum))
	}

	return s
}
