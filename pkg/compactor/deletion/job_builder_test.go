package deletion

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
)

func TestJobBuilder_buildJobs(t *testing.T) {
	now := model.Now()

	for _, tc := range []struct {
		name          string
		setupManifest func(client client.ObjectClient)
		expectedJobs  []grpc.Job
	}{
		{
			name:          "no manifests in storage",
			setupManifest: func(_ client.ObjectClient) {},
		},
		{
			name: "one manifest in storage with less than maxChunksPerJob",
			setupManifest: func(client client.ObjectClient) {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				deleteRequestBatch.addDeleteRequest(&DeleteRequest{
					UserID:    user1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   now,
				})
				manifestBuilder, err := newDeletionManifestBuilder(client, *deleteRequestBatch)
				require.NoError(t, err)

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table1, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFooBar),
					chunks: buildRetentionChunks(0, maxChunksPerJob-1),
				}))

				require.NoError(t, manifestBuilder.Finish(context.Background()))
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
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
			},
		},
		{
			name: "one manifest in storage with more than maxChunksPerJob",
			setupManifest: func(client client.ObjectClient) {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				deleteRequestBatch.addDeleteRequest(&DeleteRequest{
					UserID:    user1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   now,
				})
				manifestBuilder, err := newDeletionManifestBuilder(client, *deleteRequestBatch)
				require.NoError(t, err)

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table1, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFooBar),
					chunks: buildRetentionChunks(0, maxChunksPerJob+1),
				}))

				require.NoError(t, manifestBuilder.Finish(context.Background()))
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
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
			},
		},
		{
			name: "one manifest in storage with multiple groups",
			setupManifest: func(client client.ObjectClient) {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				deleteRequestBatch.addDeleteRequest(&DeleteRequest{
					UserID:    user1,
					RequestID: req1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   100,
				})
				deleteRequestBatch.addDeleteRequest(&DeleteRequest{
					UserID:    user1,
					RequestID: req2,
					Query:     lblFizzBuzz,
					StartTime: 51,
					EndTime:   100,
				})
				manifestBuilder, err := newDeletionManifestBuilder(client, *deleteRequestBatch)
				require.NoError(t, err)

				require.NoError(t, manifestBuilder.AddSeries(context.Background(), table1, &mockSeries{
					userID: user1,
					labels: mustParseLabel(lblFooBarAndFizzBuzz),
					chunks: buildRetentionChunks(25, 50),
				}))

				require.NoError(t, manifestBuilder.Finish(context.Background()))
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
		},
		{
			name: "one manifest in storage with multiple segments due to multiple tables",
			setupManifest: func(client client.ObjectClient) {
				deleteRequestBatch := newDeleteRequestBatch(nil)
				deleteRequestBatch.addDeleteRequest(&DeleteRequest{
					UserID:    user1,
					Query:     lblFooBar,
					StartTime: 0,
					EndTime:   now,
				})
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
								UserID:    user1,
								Query:     lblFooBar,
								StartTime: 0,
								EndTime:   now,
							},
						},
					}),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objectClient, err := local.NewFSObjectClient(local.FSConfig{
				Directory: t.TempDir(),
			})
			require.NoError(t, err)
			tc.setupManifest(objectClient)

			builder := NewJobBuilder(objectClient)
			jobsChan := make(chan *grpc.Job)

			var jobsBuilt []grpc.Job
			go func() {
				for job := range jobsChan {
					jobsBuilt = append(jobsBuilt, *job)
					builder.OnJobResponse(&grpc.JobResult{
						JobId:   job.Id,
						JobType: job.Type,
					})
				}
			}()

			err = builder.buildJobs(context.Background(), jobsChan)
			require.NoError(t, err)

			require.Equal(t, len(tc.expectedJobs), len(jobsBuilt))
			require.Equal(t, tc.expectedJobs, jobsBuilt)
		})
	}
}

func TestJobBuilder_ProcessManifest(t *testing.T) {
	for _, tc := range []struct {
		name               string
		jobProcessingError string
	}{
		{
			name: "all jobs succeeded",
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

			builder := NewJobBuilder(objectClient)

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
						Chunks: []string{"chunk1", "chunk2"},
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
					builder.OnJobResponse(&grpc.JobResult{
						JobId:   job.Id,
						JobType: job.Type,
						Error:   tc.jobProcessingError,
					})
				}
			}()

			err = builder.processManifest(context.Background(), "test-manifest", jobsChan)
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
