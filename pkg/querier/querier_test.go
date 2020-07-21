package querier

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logql"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/validation"
)

const (
	// Custom query timeout used in tests
	queryTimeout = 12 * time.Second
)

func TestQuerier_Label_QueryTimeoutConfigFlag(t *testing.T) {
	startTime := time.Now().Add(-1 * time.Minute)
	endTime := time.Now()

	request := logproto.LabelRequest{
		Name:   "test",
		Values: true,
		Start:  &startTime,
		End:    &endTime,
	}

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Label", mock.Anything, &request, mock.Anything).Return(mockLabelResponse([]string{}), nil)

	store := newStoreMock()
	store.On("LabelValuesForMetricName", mock.Anything, "test", model.TimeFromUnixNano(startTime.UnixNano()), model.TimeFromUnixNano(endTime.UnixNano()), "logs", "test").Return([]string{"foo", "bar"}, nil)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = q.Label(ctx, &request)
	require.NoError(t, err)

	calls := ingesterClient.GetMockedCallsByMethod("Label")
	assert.Equal(t, 1, len(calls))
	deadline, ok := calls[0].Arguments.Get(0).(context.Context).Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, deadline, time.Now().Add(queryTimeout), 1*time.Second)

	calls = store.GetMockedCallsByMethod("LabelValuesForMetricName")
	assert.Equal(t, 1, len(calls))
	deadline, ok = calls[0].Arguments.Get(0).(context.Context).Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, deadline, time.Now().Add(queryTimeout), 1*time.Second)

	store.AssertExpectations(t)
}

func TestQuerier_Tail_QueryTimeoutConfigFlag(t *testing.T) {
	request := logproto.TailRequest{
		Query:    "{type=\"test\"}",
		DelayFor: 0,
		Limit:    10,
		Start:    time.Now(),
	}

	store := newStoreMock()
	store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(1, 2), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 2)}), nil)

	tailClient := newTailClientMock()
	tailClient.On("Recv").Return(mockTailResponse(mockStream(1, 2)), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)
	ingesterClient.On("Tail", mock.Anything, &request, mock.Anything).Return(tailClient, nil)
	ingesterClient.On("TailersCount", mock.Anything, mock.Anything, mock.Anything).Return(&logproto.TailersCountResponse{}, nil)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = q.Tail(ctx, &request)
	require.NoError(t, err)

	calls := ingesterClient.GetMockedCallsByMethod("Query")
	assert.Equal(t, 1, len(calls))
	deadline, ok := calls[0].Arguments.Get(0).(context.Context).Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, deadline, time.Now().Add(queryTimeout), 1*time.Second)

	calls = ingesterClient.GetMockedCallsByMethod("Tail")
	assert.Equal(t, 1, len(calls))
	_, ok = calls[0].Arguments.Get(0).(context.Context).Deadline()
	assert.False(t, ok)

	calls = store.GetMockedCallsByMethod("SelectLogs")
	assert.Equal(t, 1, len(calls))
	deadline, ok = calls[0].Arguments.Get(0).(context.Context).Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, deadline, time.Now().Add(queryTimeout), 1*time.Second)

	store.AssertExpectations(t)
}

func TestQuerier_tailDisconnectedIngesters(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		connectedIngestersAddr []string
		ringIngesters          []ring.IngesterDesc
		expectedClientsAddr    []string
	}{
		"no connected ingesters and empty ring": {
			connectedIngestersAddr: []string{},
			ringIngesters:          []ring.IngesterDesc{},
			expectedClientsAddr:    []string{},
		},
		"no connected ingesters and ring containing new ingesters": {
			connectedIngestersAddr: []string{},
			ringIngesters:          []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE)},
			expectedClientsAddr:    []string{"1.1.1.1"},
		},
		"connected ingesters and ring contain the same ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2"},
			ringIngesters:          []ring.IngesterDesc{mockIngesterDesc("2.2.2.2", ring.ACTIVE), mockIngesterDesc("1.1.1.1", ring.ACTIVE)},
			expectedClientsAddr:    []string{},
		},
		"ring contains new ingesters compared to the connected one": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE), mockIngesterDesc("2.2.2.2", ring.ACTIVE), mockIngesterDesc("3.3.3.3", ring.ACTIVE)},
			expectedClientsAddr:    []string{"2.2.2.2", "3.3.3.3"},
		},
		"connected ingesters contain ingesters not in the ring anymore": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			ringIngesters:          []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE), mockIngesterDesc("3.3.3.3", ring.ACTIVE)},
			expectedClientsAddr:    []string{},
		},
		"connected ingesters contain ingesters not in the ring anymore and the ring contains new ingesters too": {
			connectedIngestersAddr: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			ringIngesters:          []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE), mockIngesterDesc("3.3.3.3", ring.ACTIVE), mockIngesterDesc("4.4.4.4", ring.ACTIVE)},
			expectedClientsAddr:    []string{"4.4.4.4"},
		},
		"ring contains ingester in LEAVING state not listed in the connected ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE), mockIngesterDesc("2.2.2.2", ring.LEAVING)},
			expectedClientsAddr:    []string{},
		},
		"ring contains ingester in PENDING state not listed in the connected ingesters": {
			connectedIngestersAddr: []string{"1.1.1.1"},
			ringIngesters:          []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE), mockIngesterDesc("2.2.2.2", ring.PENDING)},
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

			limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
			require.NoError(t, err)

			q, err := newQuerier(
				mockQuerierConfig(),
				mockIngesterClientConfig(),
				newIngesterClientMockFactory(ingesterClient),
				newReadRingMock(testData.ringIngesters),
				newStoreMock(), limits)
			require.NoError(t, err)

			actualClients, err := q.tailDisconnectedIngesters(context.Background(), &req, testData.connectedIngestersAddr)
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

func mockQuerierConfig() Config {
	return Config{
		TailMaxDuration: 1 * time.Minute,
		QueryTimeout:    queryTimeout,
	}
}

func mockQueryResponse(streams []logproto.Stream) *logproto.QueryResponse {
	return &logproto.QueryResponse{
		Streams: streams,
	}
}

func mockLabelResponse(values []string) *logproto.LabelResponse {
	return &logproto.LabelResponse{
		Values: values,
	}
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}

func TestQuerier_validateQueryRequest(t *testing.T) {
	request := logproto.QueryRequest{
		Selector:  "{type=\"test\", fail=\"yes\"} |= \"foo\"",
		Limit:     10,
		Start:     time.Now().Add(-1 * time.Minute),
		End:       time.Now(),
		Direction: logproto.FORWARD,
	}

	store := newStoreMock()
	store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(1, 2), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 2)}), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, &request, mock.Anything).Return(queryClient, nil)

	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.MaxStreamsMatchersPerQuery = 1
	defaultLimits.MaxQueryLength = 2 * time.Minute

	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")

	_, err = q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &request})
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "max streams matchers per query exceeded, matchers-count > limit (2 > 1)"), err)

	request.Selector = "{type=\"test\"}"
	_, err = q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &request})
	require.NoError(t, err)

	request.Start = request.End.Add(-3 * time.Minute)
	_, err = q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &request})
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "the query time range exceeds the limit (query length: 3m0s, limit: 2m0s)"), err)
}

func TestQuerier_SeriesAPI(t *testing.T) {
	mkReq := func(groups []string) *logproto.SeriesRequest {
		return &logproto.SeriesRequest{
			Start:  time.Unix(0, 0),
			End:    time.Unix(10, 0),
			Groups: groups,
		}
	}

	mockSeriesResponse := func(series []map[string]string) *logproto.SeriesResponse {
		resp := &logproto.SeriesResponse{}
		for _, s := range series {
			resp.Series = append(resp.Series, logproto.SeriesIdentifier{
				Labels: s,
			})
		}
		return resp
	}

	for _, tc := range []struct {
		desc  string
		req   *logproto.SeriesRequest
		setup func(*storeMock, *queryClientMock, *querierClientMock, validation.Limits, *logproto.SeriesRequest)
		run   func(*testing.T, *Querier, *logproto.SeriesRequest)
	}{
		{
			"ingester error",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(nil, errors.New("tst-err"))

				store.On("GetSeries", mock.Anything, mock.Anything).Return(nil, nil)
			},
			func(t *testing.T, q *Querier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				_, err := q.Series(ctx, req)
				require.Error(t, err)
			},
		},
		{
			"store error",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"a": "1"},
				}), nil)

				store.On("GetSeries", mock.Anything, mock.Anything).Return(nil, context.DeadlineExceeded)
			},
			func(t *testing.T, q *Querier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				_, err := q.Series(ctx, req)
				require.Error(t, err)
			},
		},
		{
			"no matches",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse(nil), nil)
				store.On("GetSeries", mock.Anything, mock.Anything).Return(nil, nil)
			},
			func(t *testing.T, q *Querier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.Equal(t, &logproto.SeriesResponse{Series: make([]logproto.SeriesIdentifier, 0)}, resp)
			},
		},
		{
			"returns series",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"a": "1", "b": "2"},
					{"a": "1", "b": "3"},
				}), nil)

				store.On("GetSeries", mock.Anything, mock.Anything).Return([]logproto.SeriesIdentifier{
					{Labels: map[string]string{"a": "1", "b": "4"}},
					{Labels: map[string]string{"a": "1", "b": "5"}},
				}, nil)
			},
			func(t *testing.T, q *Querier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.ElementsMatch(t, []logproto.SeriesIdentifier{
					{Labels: map[string]string{"a": "1", "b": "2"}},
					{Labels: map[string]string{"a": "1", "b": "3"}},
					{Labels: map[string]string{"a": "1", "b": "4"}},
					{Labels: map[string]string{"a": "1", "b": "5"}},
				}, resp.GetSeries())
			},
		},
		{
			"dedupes",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"a": "1", "b": "2"},
				}), nil)

				store.On("GetSeries", mock.Anything, mock.Anything).Return([]logproto.SeriesIdentifier{
					{Labels: map[string]string{"a": "1", "b": "2"}},
					{Labels: map[string]string{"a": "1", "b": "3"}},
				}, nil)

			},
			func(t *testing.T, q *Querier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.ElementsMatch(t, []logproto.SeriesIdentifier{
					{Labels: map[string]string{"a": "1", "b": "2"}},
					{Labels: map[string]string{"a": "1", "b": "3"}},
				}, resp.GetSeries())
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			store := newStoreMock()
			queryClient := newQueryClientMock()
			ingesterClient := newQuerierClientMock()
			defaultLimits := defaultLimitsTestConfig()
			if tc.setup != nil {
				tc.setup(store, queryClient, ingesterClient, defaultLimits, tc.req)
			}

			limits, err := validation.NewOverrides(defaultLimits, nil)
			require.NoError(t, err)

			q, err := newQuerier(
				mockQuerierConfig(),
				mockIngesterClientConfig(),
				newIngesterClientMockFactory(ingesterClient),
				mockReadRingWithOneActiveIngester(),
				store, limits)
			require.NoError(t, err)

			tc.run(t, q, tc.req)
		})
	}
}

func TestQuerier_IngesterMaxQueryLookback(t *testing.T) {

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	for _, tc := range []struct {
		desc          string
		lookback      time.Duration
		end           time.Time
		skipIngesters bool
	}{
		{
			desc:          "0 value always queries ingesters",
			lookback:      0,
			end:           time.Now().Add(time.Hour),
			skipIngesters: false,
		},
		{
			desc:          "query ingester",
			lookback:      time.Hour,
			end:           time.Now(),
			skipIngesters: false,
		},
		{
			desc:          "skip ingester",
			lookback:      time.Hour,
			end:           time.Now().Add(-2 * time.Hour),
			skipIngesters: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {

			req := logproto.QueryRequest{
				Selector:  `{app="foo"}`,
				Limit:     1000,
				Start:     tc.end.Add(-6 * time.Hour),
				End:       tc.end,
				Direction: logproto.FORWARD,
			}

			queryClient := newQueryClientMock()
			ingesterClient := newQuerierClientMock()

			if !tc.skipIngesters {
				ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)
				queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 1)}), nil).Once()
				queryClient.On("Recv").Return(nil, io.EOF).Once()
			}

			store := newStoreMock()
			store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(0, 1), nil)

			conf := mockQuerierConfig()
			conf.QueryIngestersWithin = tc.lookback
			q, err := newQuerier(
				conf,
				mockIngesterClientConfig(),
				newIngesterClientMockFactory(ingesterClient),
				mockReadRingWithOneActiveIngester(),
				store, limits)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test")

			res, err := q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &req})
			require.Nil(t, err)

			// since streams are loaded lazily, force iterators to exhaust
			for res.Next() {
			}
			queryClient.AssertExpectations(t)
			ingesterClient.AssertExpectations(t)
			store.AssertExpectations(t)

		})
	}
}

func TestQuerier_concurrentTailLimits(t *testing.T) {
	request := logproto.TailRequest{
		Query:    "{type=\"test\"}",
		DelayFor: 0,
		Limit:    10,
		Start:    time.Now(),
	}

	t.Parallel()

	tests := map[string]struct {
		ringIngesters []ring.IngesterDesc
		expectedError error
		tailersCount  uint32
	}{
		"empty ring": {
			ringIngesters: []ring.IngesterDesc{},
			expectedError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
		},
		"ring containing one pending ingester": {
			ringIngesters: []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.PENDING)},
			expectedError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
		},
		"ring containing one active ingester and 0 active tailers": {
			ringIngesters: []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE)},
		},
		"ring containing one active ingester and 1 active tailer": {
			ringIngesters: []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE)},
			tailersCount:  1,
		},
		"ring containing one pending and active ingester with 1 active tailer": {
			ringIngesters: []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.PENDING), mockIngesterDesc("2.2.2.2", ring.ACTIVE)},
			tailersCount:  1,
		},
		"ring containing one active ingester and max active tailers": {
			ringIngesters: []ring.IngesterDesc{mockIngesterDesc("1.1.1.1", ring.ACTIVE)},
			expectedError: httpgrpc.Errorf(http.StatusBadRequest,
				"max concurrent tail requests limit exceeded, count > limit (%d > %d)", 6, 5),
			tailersCount: 5,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			// For this test's purpose, whenever a new ingester client needs to
			// be created, the factory will always return the same mock instance
			store := newStoreMock()
			store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(1, 2), nil)

			queryClient := newQueryClientMock()
			queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 2)}), nil)

			tailClient := newTailClientMock()
			tailClient.On("Recv").Return(mockTailResponse(mockStream(1, 2)), nil)

			ingesterClient := newQuerierClientMock()
			ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)
			ingesterClient.On("Tail", mock.Anything, &request, mock.Anything).Return(tailClient, nil)
			ingesterClient.On("TailersCount", mock.Anything, mock.Anything, mock.Anything).Return(&logproto.TailersCountResponse{Count: testData.tailersCount}, nil)

			defaultLimits := defaultLimitsTestConfig()
			defaultLimits.MaxConcurrentTailRequests = 5

			limits, err := validation.NewOverrides(defaultLimits, nil)
			require.NoError(t, err)

			q, err := newQuerier(
				mockQuerierConfig(),
				mockIngesterClientConfig(),
				newIngesterClientMockFactory(ingesterClient),
				newReadRingMock(testData.ringIngesters),
				store, limits)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test")
			_, err = q.Tail(ctx, &request)
			assert.Equal(t, testData.expectedError, err)
		})
	}
}
