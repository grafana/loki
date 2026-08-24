package querier

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/user"
	"go.uber.org/atomic"
	"google.golang.org/grpc/codes"
	grpc_metadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestIngesterQuerier_earlyExitOnQuorum(t *testing.T) {
	t.Parallel()

	ringIngesters := []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)}
	tests := map[string]struct {
		method string
		testFn func(*IngesterQuerier) error
		retVal interface{}
	}{
		"label": {
			method: "Label",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.Label(context.Background(), nil)
				return err
			},
			retVal: new(logproto.LabelResponse),
		},
		"series": {
			method: "Series",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.Series(context.Background(), nil)
				return err
			},
			retVal: new(logproto.SeriesResponse),
		},
		"tailers_count": {
			method: "TailersCount",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.TailersCount(context.Background())
				return err
			},
			retVal: new(logproto.TailersCountResponse),
		},
		"get_chunk_ids": {
			method: "GetChunkIDs",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.GetChunkIDs(context.Background(), model.Time(0), model.Time(0))
				return err
			},
			retVal: new(logproto.GetChunkIDsResponse),
		},
	}

	var cnt atomic.Int32

	for testName, testData := range tests {
		for _, retErr := range []bool{true, false} {
			if retErr {
				testName += " call should return early on breaching max errors"
			} else {
				testName += " call should return early on reaching quorum"
			}

			t.Run(testName, func(t *testing.T) {
				wg := sync.WaitGroup{}
				wait := make(chan struct{})
				cnt.Store(0)

				runFn := func(args mock.Arguments) {
					wg.Done()

					ctx := args[0].(context.Context)
					select {
					case <-ctx.Done():
						// ctx should be cancelled after the first two replicas return
						require.ErrorIs(t, ctx.Err(), context.Canceled)
					case <-wait:
						cnt.Add(1)
					case <-time.After(time.Second):
						t.Error("timed out waiting for ctx cancellation")
					}
				}

				ingesterClient := newQuerierClientMock()
				if retErr {
					ingesterClient.On(testData.method, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New(testData.method+" failed")).Run(runFn)
				} else {
					ingesterClient.On(testData.method, mock.Anything, mock.Anything, mock.Anything).Return(testData.retVal, nil).Run(runFn)
				}

				ingesterQuerier, err := newTestIngesterQuerier(newReadRingMock(ringIngesters, 1), ingesterClient)
				require.NoError(t, err)

				wg.Add(3)
				go func() {
					// wait for all 3 replicas to get called before returning response
					wg.Wait()

					// return response from 2 of the 3 replicas
					wait <- struct{}{}
					wait <- struct{}{}
				}()

				err = testData.testFn(ingesterQuerier)
				ingesterClient.AssertNumberOfCalls(t, testData.method, 3)
				require.Equal(t, int32(2), cnt.Load())
				if retErr {
					require.ErrorContains(t, err, testData.method+" failed")
				} else {
					require.NoError(t, err)
				}
			})
		}
	}

	tests = map[string]struct {
		method string
		testFn func(*IngesterQuerier) error
		retVal interface{}
	}{
		"select_logs": {
			method: "Query",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectLogs(context.Background(), logql.SelectLogParams{
					QueryRequest: new(logproto.QueryRequest),
				})
				return err
			},
			retVal: newQueryClientMock(),
		},
		"select_sample": {
			method: "QuerySample",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectSample(context.Background(), logql.SelectSampleParams{
					SampleQueryRequest: new(logproto.SampleQueryRequest),
				})
				return err
			},
			retVal: newQuerySampleClientMock(),
		},
		"tail": {
			method: "Tail",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.Tail(context.Background(), new(logproto.TailRequest))
				return err
			},
			retVal: &mockQuerierTailClient{},
		},
	}

	for testName, testData := range tests {
		for _, retErr := range []bool{true, false} {
			if retErr {
				testName += " call should not return early on breaching max errors"
			} else {
				testName += " call should not return early on reaching quorum"
			}

			t.Run(testName, func(t *testing.T) {
				cnt.Store(0)
				wg := sync.WaitGroup{}
				wait := make(chan struct{})

				runFn := func(args mock.Arguments) {
					wg.Done()
					ctx := args[0].(context.Context)

					select {
					case <-ctx.Done():
						// should not be cancelled by the tracker
						require.NoError(t, ctx.Err())
					case <-wait:
						cnt.Add(1)
					case <-time.After(time.Second):
					}
				}

				ingesterClient := newQuerierClientMock()
				if retErr {
					ingesterClient.On(testData.method, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New(testData.method+" failed")).Run(runFn)
				} else {
					ingesterClient.On(testData.method, mock.Anything, mock.Anything, mock.Anything).Return(testData.retVal, nil).Run(runFn)
				}
				ingesterQuerier, err := newTestIngesterQuerier(newReadRingMock(ringIngesters, 1), ingesterClient)
				require.NoError(t, err)

				wg.Add(3)
				go func() {
					// wait for all 3 replicas to get called before returning response
					wg.Wait()

					// return response from 2 out of the 3 replicas
					wait <- struct{}{}
					wait <- struct{}{}
				}()

				err = testData.testFn(ingesterQuerier)
				ingesterClient.AssertNumberOfCalls(t, testData.method, 3)
				require.Equal(t, int32(2), cnt.Load())
				if retErr {
					require.ErrorContains(t, err, testData.method+" failed")
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestIngesterQuerierFetchesResponsesFromPartitionIngesters(t *testing.T) {
	t.Parallel()
	ctx := user.InjectOrgID(context.Background(), "test-user")
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	ingesters := []ring.InstanceDesc{
		mockInstanceDescWithZone("1.1.1.1", ring.ACTIVE, "A"),
		mockInstanceDescWithZone("2.2.2.2", ring.ACTIVE, "B"),
		mockInstanceDescWithZone("3.3.3.3", ring.ACTIVE, "A"),
		mockInstanceDescWithZone("4.4.4.4", ring.ACTIVE, "B"),
		mockInstanceDescWithZone("5.5.5.5", ring.ACTIVE, "A"),
		mockInstanceDescWithZone("6.6.6.6", ring.ACTIVE, "B"),
	}

	tests := map[string]struct {
		method string
		testFn func(*IngesterQuerier) error
		retVal interface{}
		shards int
	}{
		"label": {
			method: "Label",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.Label(ctx, nil)
				return err
			},
			retVal: new(logproto.LabelResponse),
		},
		"series": {
			method: "Series",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.Series(ctx, nil)
				return err
			},
			retVal: new(logproto.SeriesResponse),
		},
		"get_chunk_ids": {
			method: "GetChunkIDs",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.GetChunkIDs(ctx, model.Time(0), model.Time(0))
				return err
			},
			retVal: new(logproto.GetChunkIDsResponse),
		},
		"select_logs": {
			method: "Query",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectLogs(ctx, logql.SelectLogParams{
					QueryRequest: new(logproto.QueryRequest),
				})
				return err
			},
			retVal: newQueryClientMock(),
		},
		"select_sample": {
			method: "QuerySample",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectSample(ctx, logql.SelectSampleParams{
					SampleQueryRequest: new(logproto.SampleQueryRequest),
				})
				return err
			},
			retVal: newQuerySampleClientMock(),
		},
		"select_logs_shuffle_sharded": {
			method: "Query",
			testFn: func(ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectLogs(ctx, logql.SelectLogParams{
					QueryRequest: new(logproto.QueryRequest),
				})
				return err
			},
			retVal: newQueryClientMock(),
			shards: 2, // Must be less than number of partitions
		},
	}

	for testName, testData := range tests {
		cnt := atomic.NewInt32(0)

		t.Run(testName, func(t *testing.T) {
			cnt.Store(0)
			runFn := func(args mock.Arguments) {
				ctx := args[0].(context.Context)

				select {
				case <-ctx.Done():
					// should not be cancelled by the tracker
					require.NoErrorf(t, ctx.Err(), "tracker should not cancel ctx: %v", context.Cause(ctx))
				default:
					cnt.Add(1)
				}
			}

			instanceRing := newReadRingMock(ingesters, 0)
			ingesterClient := newQuerierClientMock()
			ingesterClient.On(testData.method, mock.Anything, mock.Anything, mock.Anything).Return(testData.retVal, nil).Run(runFn)

			partitions := 3
			ingestersPerPartition := len(ingesters) / partitions
			assert.Greaterf(t, ingestersPerPartition, 1, "must have more than one ingester per partition")

			ingesterQuerier, err := newTestPartitionIngesterQuerier(newIngesterClientMockFactory(ingesterClient), instanceRing, newPartitionInstanceRingMock(instanceRing, ingesters, partitions, ingestersPerPartition), testData.shards)
			require.NoError(t, err)

			ingesterQuerier.querierConfig.QueryPartitionIngesters = true

			err = testData.testFn(ingesterQuerier)
			require.NoError(t, err)

			if testData.shards == 0 {
				testData.shards = partitions
			}
			expectedCalls := min(testData.shards, partitions)
			// Wait for responses: We expect one request per queried partition because we have request minimization enabled & ingesters are in multiple zones.
			// If shuffle sharding is enabled, we expect one query per shard as we write to a subset of partitions.
			require.Eventually(t, func() bool { return cnt.Load() >= int32(expectedCalls) }, time.Millisecond*100, time.Millisecond*1, "expected all ingesters to respond")
			ingesterClient.AssertNumberOfCalls(t, testData.method, expectedCalls)
		})
	}
}

func TestIngesterQuerier_QueriesSameIngestersWithPartitionContext(t *testing.T) {
	t.Parallel()
	userCtx := user.InjectOrgID(context.Background(), "test-user")
	testCtx, cancel := context.WithTimeout(userCtx, time.Second*10)
	defer cancel()

	ingesters := []ring.InstanceDesc{
		mockInstanceDescWithZone("1.1.1.1", ring.ACTIVE, "A"),
		mockInstanceDescWithZone("2.2.2.2", ring.ACTIVE, "B"),
		mockInstanceDescWithZone("3.3.3.3", ring.ACTIVE, "A"),
		mockInstanceDescWithZone("4.4.4.4", ring.ACTIVE, "B"),
		mockInstanceDescWithZone("5.5.5.5", ring.ACTIVE, "A"),
		mockInstanceDescWithZone("6.6.6.6", ring.ACTIVE, "B"),
	}

	tests := map[string]struct {
		method string
		testFn func(context.Context, *IngesterQuerier) error
		retVal interface{}
		shards int
	}{
		"select_logs": {
			method: "Query",
			testFn: func(ctx context.Context, ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectLogs(ctx, logql.SelectLogParams{
					QueryRequest: new(logproto.QueryRequest),
				})
				return err
			},
			retVal: newQueryClientMock(),
		},
		"select_sample": {
			method: "QuerySample",
			testFn: func(ctx context.Context, ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectSample(ctx, logql.SelectSampleParams{
					SampleQueryRequest: new(logproto.SampleQueryRequest),
				})
				return err
			},
			retVal: newQuerySampleClientMock(),
		},
		"select_logs_shuffle_sharded": {
			method: "Query",
			testFn: func(ctx context.Context, ingesterQuerier *IngesterQuerier) error {
				_, err := ingesterQuerier.SelectLogs(ctx, logql.SelectLogParams{
					QueryRequest: new(logproto.QueryRequest),
				})
				return err
			},
			retVal: newQueryClientMock(),
			shards: 2, // Must be less than number of partitions
		},
	}

	for testName, testData := range tests {
		cnt := atomic.NewInt32(0)
		ctx := NewPartitionContext(testCtx)

		t.Run(testName, func(t *testing.T) {
			cnt.Store(0)
			runFn := func(args mock.Arguments) {
				ctx := args[0].(context.Context)

				select {
				case <-ctx.Done():
					// should not be cancelled by the tracker
					require.NoErrorf(t, ctx.Err(), "tracker should not cancel ctx: %v", context.Cause(ctx))
				default:
					cnt.Add(1)
				}
			}

			instanceRing := newReadRingMock(ingesters, 0)
			ingesterClient := newQuerierClientMock()
			ingesterClient.On(testData.method, mock.Anything, mock.Anything, mock.Anything).Return(testData.retVal, nil).Run(runFn)
			ingesterClient.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything).Return(new(logproto.GetChunkIDsResponse), nil).Run(runFn)

			partitions := 3
			ingestersPerPartition := len(ingesters) / partitions
			assert.Greaterf(t, ingestersPerPartition, 1, "must have more than one ingester per partition")

			mockClientFactory := mockIngesterClientFactory{
				requestedClients: make(map[string]int),
			}

			ingesterQuerier, err := newTestPartitionIngesterQuerier(mockClientFactory.newIngesterClientMockFactory(ingesterClient), instanceRing, newPartitionInstanceRingMock(instanceRing, ingesters, partitions, ingestersPerPartition), testData.shards)
			require.NoError(t, err)

			ingesterQuerier.querierConfig.QueryPartitionIngesters = true

			err = testData.testFn(ctx, ingesterQuerier)
			require.NoError(t, err)

			if testData.shards == 0 {
				testData.shards = partitions
			}
			expectedCalls := min(testData.shards, partitions)
			expectedIngesterCalls := expectedCalls
			// Wait for responses: We expect one request per queried partition because we have request minimization enabled & ingesters are in multiple zones.
			// If shuffle sharding is enabled, we expect one query per shard as we write to a subset of partitions.
			require.Eventually(t, func() bool { return cnt.Load() >= int32(expectedCalls) }, time.Millisecond*100, time.Millisecond*1, "expected ingesters to respond")
			ingesterClient.AssertNumberOfCalls(t, testData.method, expectedCalls)

			partitionCtx := ExtractPartitionContext(ctx)
			require.Equal(t, expectedIngesterCalls, len(partitionCtx.ingestersUsed))
			require.Equal(t, expectedIngesterCalls, len(mockClientFactory.requestedClients))

			for _, ingester := range partitionCtx.ingestersUsed {
				count, ok := mockClientFactory.requestedClients[ingester.addr]
				require.True(t, ok)
				require.Equal(t, count, 1)
			}

			// Now call getChunkIDs to ensure we only call the same ingesters as before.
			_, err = ingesterQuerier.GetChunkIDs(ctx, model.Time(0), model.Time(1))
			require.NoError(t, err)

			require.Eventually(t, func() bool { return cnt.Load() >= int32(expectedCalls) }, time.Millisecond*100, time.Millisecond*1, "expected ingesters to respond")
			ingesterClient.AssertNumberOfCalls(t, "GetChunkIDs", expectedCalls)

			// Finally, confirm we called the same ingesters again and didn't ask for any new clients
			require.Equal(t, expectedIngesterCalls, len(mockClientFactory.requestedClients))
			for _, ingester := range partitionCtx.ingestersUsed {
				count, ok := mockClientFactory.requestedClients[ingester.addr]
				require.True(t, ok)
				require.Equal(t, count, 1)
			}
		})
	}
}

func TestQuerier_tailDisconnectedIngesters(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		connectedIngestersAddr []string
		ringIngesters          []ring.InstanceDesc
		expectedClientsAddr    []string
	}{
		"no connected ingesters and empty ring": {
			connectedIngestersAddr: []string{},
			ringIngesters:          []ring.InstanceDesc{},
			expectedClientsAddr:    []string{},
		},
		"no connected ingesters and ring containing new ingesters": {
			connectedIngestersAddr: []string{},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
			expectedClientsAddr:    []string{"1.1.1.1"},
		},
		"connected ingesters and ring contain the same ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("2.2.2.2", ring.ACTIVE), mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
			expectedClientsAddr:    []string{},
		},
		"ring contains new ingesters compared to the connected one": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)},
			expectedClientsAddr:    []string{"2.2.2.2", "3.3.3.3"},
		},
		"connected ingesters contain ingesters not in the ring anymore": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)},
			expectedClientsAddr:    []string{},
		},
		"connected ingesters contain ingesters not in the ring anymore and the ring contains new ingesters too": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE), mockInstanceDesc("4.4.4.4", ring.ACTIVE)},
			expectedClientsAddr:    []string{"4.4.4.4"},
		},
		"ring contains ingester in LEAVING state not listed in the connected ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.LEAVING)},
			expectedClientsAddr:    []string{},
		},
		"ring contains ingester in PENDING state not listed in the connected ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("2.2.2.2", ring.PENDING)},
			expectedClientsAddr:    []string{},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			req := logproto.TailRequest{
				Query:    "{type=\"test\"}",
				DelayFor: 0,
				Limit:    10,
				Start:    time.Now(),
			}

			// For this test's purpose, whenever a new ingester client needs to
			// be created, the factory will always return the same mock instance
			ingesterClient := newQuerierClientMock()
			ingesterClient.On("Tail", mock.Anything, &req, mock.Anything).Return(&mockQuerierTailClient{}, nil)

			ingesterQuerier, err := newTestIngesterQuerier(newReadRingMock(testData.ringIngesters, 0), ingesterClient)
			require.NoError(t, err)

			actualClients, err := ingesterQuerier.TailDisconnectedIngesters(context.Background(), &req, testData.connectedIngestersAddr)
			require.NoError(t, err)

			actualClientsAddr := make([]string, 0, len(actualClients))
			for addr, client := range actualClients {
				actualClientsAddr = append(actualClientsAddr, addr)

				// The returned map of clients should never contain nil values
				assert.NotNil(t, client)
			}

			assert.ElementsMatch(t, testData.expectedClientsAddr, actualClientsAddr)
		})
	}
}

func TestConvertMatchersToString(t *testing.T) {
	for _, tc := range []struct {
		name     string
		matchers []*labels.Matcher
		expected string
	}{
		{
			name:     "empty matchers",
			matchers: []*labels.Matcher{},
			expected: "{}",
		},
		{
			name: "with matchers",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "equal"),
				labels.MustNewMatcher(labels.MatchNotEqual, "bar", "not-equal"),
			},
			expected: "{foo=\"equal\",bar!=\"not-equal\"}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, convertMatchersToString(tc.matchers))
		})
	}
}

func TestIngesterQuerier_Volume(t *testing.T) {
	t.Run("it gets label volumes from all the ingesters", func(t *testing.T) {
		ret := &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{Name: `{foo="bar"}`, Volume: 38},
			},
			Limit: 10,
		}

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("GetVolume", mock.Anything, mock.Anything, mock.Anything).Return(ret, nil)

		ingesterQuerier, err := newTestIngesterQuerier(newReadRingMock([]ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)}, 0), ingesterClient)
		require.NoError(t, err)

		volumes, err := ingesterQuerier.Volume(context.Background(), "", 0, 1, 10, nil, "labels")
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 76},
		}, volumes.Volumes)
	})

	t.Run("it returns an empty result when an unimplemented error happens", func(t *testing.T) {
		ingesterClient := newQuerierClientMock()
		ingesterClient.On("GetVolume", mock.Anything, mock.Anything, mock.Anything).Return(nil, status.Error(codes.Unimplemented, "something bad"))

		ingesterQuerier, err := newTestIngesterQuerier(newReadRingMock([]ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)}, 0), ingesterClient)
		require.NoError(t, err)

		volumes, err := ingesterQuerier.Volume(context.Background(), "", 0, 1, 10, nil, "labels")
		require.NoError(t, err)

		require.Equal(t, []logproto.Volume(nil), volumes.Volumes)
	})
}

func TestIngesterQuerier_DetectedLabels(t *testing.T) {
	t.Run("it returns all unique detected labels from all ingesters", func(t *testing.T) {
		req := logproto.DetectedLabelsRequest{}

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything).Return(&logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
			"cluster": {Values: []string{"ingester"}},
			"foo":     {Values: []string{"abc", "abc", "ghi"}},
			"bar":     {Values: []string{"cgi", "def"}},
			"all-ids": {Values: []string{"1", "3", "3", "3"}},
		}}, nil)

		readRingMock := newReadRingMock([]ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE), mockInstanceDesc("3.3.3.3", ring.ACTIVE)}, 0)
		ingesterQuerier, err := newTestIngesterQuerier(readRingMock, ingesterClient)
		require.NoError(t, err)

		detectedLabels, err := ingesterQuerier.DetectedLabel(context.Background(), &req)
		require.NoError(t, err)

		require.Equal(t, &logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
			"all-ids": {Values: []string{"1", "3"}},
			"bar":     {Values: []string{"cgi", "def"}},
			"cluster": {Values: []string{"ingester"}},
			"foo":     {Values: []string{"abc", "ghi"}},
		}}, detectedLabels)
	})
}

func newTestIngesterQuerier(readRingMock *readRingMock, ingesterClient *querierClientMock) (*IngesterQuerier, error) {
	return newIngesterQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		readRingMock,
		nil,
		func(string) int { return 0 },
		newIngesterClientMockFactory(ingesterClient),
		constants.Loki,
		log.NewNopLogger(),
	)
}

func newTestIngesterQuerierWithConfig(cfg Config, readRingMock *readRingMock, clientFactory client.PoolFactory) (*IngesterQuerier, error) {
	return newIngesterQuerier(
		cfg,
		mockIngesterClientConfig(),
		readRingMock,
		nil,
		func(string) int { return 0 },
		clientFactory,
		constants.Loki,
		log.NewNopLogger(),
	)
}

func newTestPartitionIngesterQuerier(clientFactory client.PoolFactory, instanceRing *readRingMock, partitionRing *ring.PartitionInstanceRing, tenantShards int) (*IngesterQuerier, error) {
	return newIngesterQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		instanceRing,
		partitionRing,
		func(string) int { return tenantShards },
		clientFactory,
		constants.Loki,
		log.NewNopLogger(),
	)
}

var _ logproto.Querier_TailClient = &mockQuerierTailClient{}

// mockQuerierTailClient implements logproto.Querier_TailClient interface
type mockQuerierTailClient struct{}

func (c *mockQuerierTailClient) Recv() (*logproto.TailResponse, error) {
	return nil, nil
}

func (c *mockQuerierTailClient) Header() (grpc_metadata.MD, error) {
	return nil, nil
}

func (c *mockQuerierTailClient) Trailer() grpc_metadata.MD {
	return nil
}

func (c *mockQuerierTailClient) CloseSend() error {
	return nil
}

func (c *mockQuerierTailClient) Context() context.Context {
	return context.Background()
}

func (c *mockQuerierTailClient) SendMsg(_ interface{}) error {
	return nil
}

func (c *mockQuerierTailClient) RecvMsg(_ interface{}) error {
	return nil
}

func TestPreferredZoneSorter(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		preferred []string
		zones     []string
		// expectFirst is the set of zones expected to occupy the first
		// len(expectFirst) positions, in any order.
		expectFirst []string
	}{
		"no preferred zone keeps every zone": {
			preferred:   nil,
			zones:       []string{"A", "B", "C"},
			expectFirst: nil,
		},
		"single preferred zone is sorted first": {
			preferred:   []string{"A"},
			zones:       []string{"A", "B", "C"},
			expectFirst: []string{"A"},
		},
		"single preferred zone is sorted first regardless of input order": {
			preferred:   []string{"C"},
			zones:       []string{"A", "B", "C"},
			expectFirst: []string{"C"},
		},
		"all preferred zones are sorted first": {
			preferred:   []string{"A", "C"},
			zones:       []string{"A", "B", "C"},
			expectFirst: []string{"A", "C"},
		},
		"preferred zone missing from the ring is ignored": {
			preferred:   []string{"D"},
			zones:       []string{"A", "B", "C"},
			expectFirst: nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Run repeatedly: the sorter shuffles, so a single run can pass by chance.
			for i := 0; i < 50; i++ {
				sorted := preferredZoneSorter(tc.preferred)(slices.Clone(tc.zones))

				require.ElementsMatch(t, tc.zones, sorted, "every zone must be retained")
				if len(tc.expectFirst) > 0 {
					require.ElementsMatch(t, tc.expectFirst, sorted[:len(tc.expectFirst)])
				}
			}
		})
	}
}

// zonedIngesters returns two ingesters per zone, and a lookup from address to zone.
func zonedIngesters(zones ...string) ([]ring.InstanceDesc, map[string]string) {
	instances := make([]ring.InstanceDesc, 0, len(zones)*2)
	zoneByAddr := make(map[string]string, len(zones)*2)

	for i, zone := range zones {
		for j := 0; j < 2; j++ {
			addr := fmt.Sprintf("%d.%d.%d.%d", i+1, i+1, i+1, j+1)
			instances = append(instances, mockInstanceDescWithZone(addr, ring.ACTIVE, zone))
			zoneByAddr[addr] = zone
		}
	}

	return instances, zoneByAddr
}

// newPerAddrClientFactory returns a pool factory handing out one client per
// address, along with a func returning the addresses a client was requested for.
// The pool only creates a client for an ingester the querier actually queries, so
// that set is exactly the set of queried ingesters.
func newPerAddrClientFactory(setup func(addr string) *querierClientMock) (client.PoolFactory, func() []string) {
	var (
		mu      sync.Mutex
		created = map[string]*querierClientMock{}
	)

	factory := client.PoolAddrFunc(func(addr string) (client.PoolClient, error) {
		mu.Lock()
		defer mu.Unlock()

		if c, ok := created[addr]; ok {
			return c, nil
		}

		c := setup(addr)
		created[addr] = c
		return c, nil
	})

	queriedAddrs := func() []string {
		mu.Lock()
		defer mu.Unlock()

		addrs := make([]string, 0, len(created))
		for addr := range created {
			addrs = append(addrs, addr)
		}
		sort.Strings(addrs)
		return addrs
	}

	return factory, queriedAddrs
}

func distinctZones(addrs []string, zoneByAddr map[string]string) []string {
	zones := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if zone := zoneByAddr[addr]; !slices.Contains(zones, zone) {
			zones = append(zones, zone)
		}
	}
	sort.Strings(zones)
	return zones
}

func TestIngesterQuerier_zoneRestrictedReads(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		zones               []string
		replicationFactor   int
		maxUnavailableZones int
		zoneAware           bool
		preferredZones      []string
		ingesterQueryZones  int
		expectZoneCount     int
		expectZones         []string
	}{
		"unconfigured queries every zone": {
			zones:               []string{"A", "B", "C"},
			replicationFactor:   3,
			maxUnavailableZones: 1,
			zoneAware:           true,
			expectZoneCount:     3,
		},
		"preferred zone only queries the zones needed for ring quorum": {
			zones:               []string{"A", "B", "C"},
			replicationFactor:   3,
			maxUnavailableZones: 1,
			zoneAware:           true,
			preferredZones:      []string{"A"},
			expectZoneCount:     2,
			expectZones:         []string{"A"},
		},
		"one zone queries only the preferred zone": {
			zones:               []string{"A", "B", "C"},
			replicationFactor:   3,
			maxUnavailableZones: 1,
			zoneAware:           true,
			preferredZones:      []string{"A"},
			ingesterQueryZones:  1,
			expectZoneCount:     1,
			expectZones:         []string{"A"},
		},
		"two zones queries the preferred zone and one other": {
			zones:               []string{"A", "B", "C"},
			replicationFactor:   3,
			maxUnavailableZones: 1,
			zoneAware:           true,
			preferredZones:      []string{"A"},
			ingesterQueryZones:  2,
			expectZoneCount:     2,
			expectZones:         []string{"A"},
		},
		"more zones than replicas falls back to ring quorum": {
			// With 4 zones and RF 3 a zone does not hold a complete copy of the
			// data, so the restriction must be ignored.
			zones:               []string{"A", "B", "C", "D"},
			replicationFactor:   3,
			maxUnavailableZones: 1,
			zoneAware:           true,
			preferredZones:      []string{"A"},
			ingesterQueryZones:  1,
			expectZoneCount:     3,
			expectZones:         []string{"A"},
		},
		"zone awareness disabled queries every zone": {
			zones:              []string{"A", "B", "C"},
			replicationFactor:  3,
			zoneAware:          false,
			preferredZones:     []string{"A"},
			ingesterQueryZones: 1,
			expectZoneCount:    3,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ingesters, zoneByAddr := zonedIngesters(tc.zones...)

			var ringMock *readRingMock
			if tc.zoneAware {
				ringMock = newZoneAwareReadRingMock(ingesters, tc.maxUnavailableZones, tc.replicationFactor)
			} else {
				ringMock = newReadRingMock(ingesters, 0)
			}

			factory, queriedAddrs := newPerAddrClientFactory(func(_ string) *querierClientMock {
				c := newQuerierClientMock()
				c.On("Label", mock.Anything, mock.Anything, mock.Anything).Return(new(logproto.LabelResponse), nil)
				return c
			})

			cfg := mockQuerierConfig()
			cfg.PreferAvailabilityZones = tc.preferredZones
			cfg.IngesterQueryZones = tc.ingesterQueryZones

			ingesterQuerier, err := newTestIngesterQuerierWithConfig(cfg, ringMock, factory)
			require.NoError(t, err)

			_, err = ingesterQuerier.Label(context.Background(), nil)
			require.NoError(t, err)

			// Zones beyond the ones needed are only started on failure, so wait for
			// the expected count and then check no further zone gets queried.
			require.Eventually(t, func() bool {
				return len(distinctZones(queriedAddrs(), zoneByAddr)) >= tc.expectZoneCount
			}, time.Second, time.Millisecond, "expected %d zones to be queried, got %v", tc.expectZoneCount, distinctZones(queriedAddrs(), zoneByAddr))

			require.Never(t, func() bool {
				return len(distinctZones(queriedAddrs(), zoneByAddr)) > tc.expectZoneCount
			}, 100*time.Millisecond, 10*time.Millisecond, "no more than %d zones should be queried", tc.expectZoneCount)

			queriedZones := distinctZones(queriedAddrs(), zoneByAddr)
			require.Len(t, queriedZones, tc.expectZoneCount)
			for _, zone := range tc.expectZones {
				require.Contains(t, queriedZones, zone)
			}

			// Every ingester in a queried zone must be queried, as a zone only
			// holds a complete copy of the data across all of its ingesters.
			require.Len(t, queriedAddrs(), tc.expectZoneCount*2)
		})
	}
}

func TestIngesterQuerier_zoneRestrictedReadsFallBackOnFailure(t *testing.T) {
	t.Parallel()

	ingesters, zoneByAddr := zonedIngesters("A", "B", "C")
	ringMock := newZoneAwareReadRingMock(ingesters, 1, 3)

	// Every ingester in the preferred zone fails, so the querier has to fall back.
	factory, queriedAddrs := newPerAddrClientFactory(func(addr string) *querierClientMock {
		c := newQuerierClientMock()
		if zoneByAddr[addr] == "A" {
			c.On("Label", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("zone A is down"))
		} else {
			c.On("Label", mock.Anything, mock.Anything, mock.Anything).Return(new(logproto.LabelResponse), nil)
		}
		return c
	})

	cfg := mockQuerierConfig()
	cfg.PreferAvailabilityZones = []string{"A"}
	cfg.IngesterQueryZones = 1

	ingesterQuerier, err := newTestIngesterQuerierWithConfig(cfg, ringMock, factory)
	require.NoError(t, err)

	_, err = ingesterQuerier.Label(context.Background(), nil)
	require.NoError(t, err, "the query should succeed by falling back to another zone")

	queriedZones := distinctZones(queriedAddrs(), zoneByAddr)
	require.Contains(t, queriedZones, "A", "the preferred zone should be tried first")
	require.Len(t, queriedZones, 2, "exactly one zone should be used as fallback")
}
