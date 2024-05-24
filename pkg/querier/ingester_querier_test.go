package querier

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"go.uber.org/atomic"

	"google.golang.org/grpc/codes"
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
			testName, testData, retErr := testName, testData, retErr
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
			retVal: newTailClientMock(),
		},
	}

	for testName, testData := range tests {
		for _, retErr := range []bool{true, false} {
			testName, testData, retErr := testName, testData, retErr
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
		testData := testData

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
			ingesterClient.On("Tail", mock.Anything, &req, mock.Anything).Return(newTailClientMock(), nil)

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
		mockIngesterClientConfig(),
		readRingMock,
		mockQuerierConfig().ExtraQueryDelay,
		newIngesterClientMockFactory(ingesterClient),
		constants.Loki,
		log.NewNopLogger(),
	)
}
