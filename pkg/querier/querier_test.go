package querier

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/v3/pkg/util/log"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
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
	limitsCfg := defaultLimitsTestConfig()
	limitsCfg.QueryTimeout = model.Duration(queryTimeout)
	limits, err := validation.NewOverrides(limitsCfg, nil)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		&mockDeleteGettter{},
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
		Query:    `{type="test"}`,
		DelayFor: 0,
		Limit:    10,
		Start:    time.Now(),
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{type="test"}`),
		},
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

	limitsCfg := defaultLimitsTestConfig()
	limitsCfg.QueryTimeout = model.Duration(queryTimeout)
	limits, err := validation.NewOverrides(limitsCfg, nil)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		&mockDeleteGettter{},
		store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = q.Tail(ctx, &request, false)
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

func mockQuerierConfig() Config {
	return Config{
		TailMaxDuration: 1 * time.Minute,
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
		Selector:  `{type="test", fail="yes"} |= "foo"`,
		Limit:     10,
		Start:     time.Now().Add(-1 * time.Minute),
		End:       time.Now(),
		Direction: logproto.FORWARD,
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{type="test", fail="yes"} |= "foo"`),
		},
	}

	store := newStoreMock()
	store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(1, 2), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 2)}), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, &request, mock.Anything).Return(queryClient, nil)

	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.MaxStreamsMatchersPerQuery = 1
	defaultLimits.MaxQueryLength = model.Duration(2 * time.Minute)

	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		&mockDeleteGettter{},
		store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")

	_, err = q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &request})
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "max streams matchers per query exceeded, matchers-count > limit (2 > 1)"), err)

	request.Selector = `{type="test"}`
	request.Plan = &plan.QueryPlan{
		AST: syntax.MustParseExpr(`{type="test"}`),
	}
	_, err = q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &request})
	require.NoError(t, err)

	request.Start = request.End.Add(-3*time.Minute - 2*time.Second)
	_, err = q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &request})
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "the query time range exceeds the limit (query length: 3m2s, limit: 2m)"), err)
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
			resp.Series = append(resp.Series, logproto.SeriesIdentifierFromMap(s))
		}
		return resp
	}

	for _, tc := range []struct {
		desc  string
		req   *logproto.SeriesRequest
		setup func(*storeMock, *queryClientMock, *querierClientMock, validation.Limits, *logproto.SeriesRequest)
		run   func(*testing.T, *SingleTenantQuerier, *logproto.SeriesRequest)
	}{
		{
			"ingester error",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, _ *queryClientMock, ingester *querierClientMock, _ validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(nil, errors.New("tst-err"))

				store.On("SelectSeries", mock.Anything, mock.Anything).Return(nil, nil)
			},
			func(t *testing.T, q *SingleTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				_, err := q.Series(ctx, req)
				require.Error(t, err)
			},
		},
		{
			"store error",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, _ *queryClientMock, ingester *querierClientMock, _ validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"a": "1"},
				}), nil)

				store.On("SelectSeries", mock.Anything, mock.Anything).Return(nil, context.DeadlineExceeded)
			},
			func(t *testing.T, q *SingleTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				_, err := q.Series(ctx, req)
				require.Error(t, err)
			},
		},
		{
			"no matches",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, _ *queryClientMock, ingester *querierClientMock, _ validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse(nil), nil)
				store.On("SelectSeries", mock.Anything, mock.Anything).Return(nil, nil)
			},
			func(t *testing.T, q *SingleTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.Equal(t, &logproto.SeriesResponse{Series: make([]logproto.SeriesIdentifier, 0)}, resp)
			},
		},
		{
			"returns series",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, _ *queryClientMock, ingester *querierClientMock, _ validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"a": "1", "b": "2"},
					{"a": "1", "b": "3"},
				}), nil)

				store.On("SelectSeries", mock.Anything, mock.Anything).Return([]logproto.SeriesIdentifier{
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "4"},
					}},
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "5"},
					}},
				}, nil)
			},
			func(t *testing.T, q *SingleTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.ElementsMatch(t, []logproto.SeriesIdentifier{
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "2"},
					}},
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "a", Value: "1"},
							{Key: "b", Value: "3"},
						},
					},
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "4"},
					}},
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "5"},
					}},
				}, resp.GetSeries())
			},
		},
		{
			"dedupes",
			mkReq([]string{`{a="1"}`}),
			func(store *storeMock, _ *queryClientMock, ingester *querierClientMock, _ validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"a": "1", "b": "2"},
				}), nil)

				store.On("SelectSeries", mock.Anything, mock.Anything).Return([]logproto.SeriesIdentifier{
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "2"},
					}},
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "3"},
					}},
				}, nil)
			},
			func(t *testing.T, q *SingleTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "test")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.ElementsMatch(t, []logproto.SeriesIdentifier{
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "2"},
					}},
					{Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "a", Value: "1"},
						{Key: "b", Value: "3"},
					}},
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
				&mockDeleteGettter{},
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
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`{app="foo"}`),
				},
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
				&mockDeleteGettter{},
				store, limits)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test")

			res, err := q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &req})
			require.Nil(t, err)

			// since streams are loaded lazily, force iterators to exhaust
			//nolint:revive
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
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr("{type=\"test\"}"),
		},
	}

	t.Parallel()

	tests := map[string]struct {
		ringIngesters []ring.InstanceDesc
		expectedError error
		tailersCount  uint32
	}{
		"empty ring": {
			ringIngesters: []ring.InstanceDesc{},
			expectedError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
		},
		"ring containing one pending ingester": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.PENDING)},
			expectedError: httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found"),
		},
		"ring containing one active ingester and 0 active tailers": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
		},
		"ring containing one active ingester and 1 active tailer": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
			tailersCount:  1,
		},
		"ring containing one pending and active ingester with 1 active tailer": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.PENDING), mockInstanceDesc("2.2.2.2", ring.ACTIVE)},
			tailersCount:  1,
		},
		"ring containing one active ingester and max active tailers": {
			ringIngesters: []ring.InstanceDesc{mockInstanceDesc("1.1.1.1", ring.ACTIVE)},
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
				newReadRingMock(testData.ringIngesters, 0),
				&mockDeleteGettter{},
				store, limits)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test")
			_, err = q.Tail(ctx, &request, false)
			assert.Equal(t, testData.expectedError, err)
		})
	}
}

func TestQuerier_buildQueryIntervals(t *testing.T) {
	// For simplicity it is always assumed that ingesterQueryStoreMaxLookback and queryIngestersWithin both would be set upto 11 hours so
	// overlappingQuery has range of last 11 hours while nonOverlappingQuery has range older than last 11 hours.
	// We would test the cases below with both the queries.
	overlappingQuery := interval{
		start: time.Now().Add(-6 * time.Hour),
		end:   time.Now(),
	}

	nonOverlappingQuery := interval{
		start: time.Now().Add(-24 * time.Hour),
		end:   time.Now().Add(-12 * time.Hour),
	}

	type response struct {
		ingesterQueryInterval *interval
		storeQueryInterval    *interval
	}

	compareResponse := func(t *testing.T, expectedResponse, actualResponse response) {
		if expectedResponse.ingesterQueryInterval == nil {
			require.Nil(t, actualResponse.ingesterQueryInterval)
		} else {
			require.InDelta(t, expectedResponse.ingesterQueryInterval.start.Unix(), actualResponse.ingesterQueryInterval.start.Unix(), 1)
			require.InDelta(t, expectedResponse.ingesterQueryInterval.end.Unix(), actualResponse.ingesterQueryInterval.end.Unix(), 1)
		}

		if expectedResponse.storeQueryInterval == nil {
			require.Nil(t, actualResponse.storeQueryInterval)
		} else {
			require.InDelta(t, expectedResponse.storeQueryInterval.start.Unix(), actualResponse.storeQueryInterval.start.Unix(), 1)
			require.InDelta(t, expectedResponse.storeQueryInterval.end.Unix(), actualResponse.storeQueryInterval.end.Unix(), 1)
		}
	}

	for _, tc := range []struct {
		name                                string
		ingesterQueryStoreMaxLookback       time.Duration
		queryIngestersWithin                time.Duration
		overlappingQueryExpectedResponse    response
		nonOverlappingQueryExpectedResponse response
	}{
		{
			name: "default values, query ingesters and store for whole duration",
			overlappingQueryExpectedResponse: response{ // query both store and ingesters
				ingesterQueryInterval: &overlappingQuery,
				storeQueryInterval:    &overlappingQuery,
			},
			nonOverlappingQueryExpectedResponse: response{ // query both store and ingesters
				ingesterQueryInterval: &nonOverlappingQuery,
				storeQueryInterval:    &nonOverlappingQuery,
			},
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to 1h",
			ingesterQueryStoreMaxLookback: time.Hour,
			overlappingQueryExpectedResponse: response{ // query ingesters for last 1h and store until last 1h.
				ingesterQueryInterval: &interval{
					start: time.Now().Add(-time.Hour),
					end:   overlappingQuery.end,
				},
				storeQueryInterval: &interval{
					start: overlappingQuery.start,
					end:   time.Now().Add(-time.Hour),
				},
			},
			nonOverlappingQueryExpectedResponse: response{ // query just the store
				storeQueryInterval: &nonOverlappingQuery,
			},
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to 10h",
			ingesterQueryStoreMaxLookback: 10 * time.Hour,
			overlappingQueryExpectedResponse: response{ // query just the ingesters.
				ingesterQueryInterval: &overlappingQuery,
			},
			nonOverlappingQueryExpectedResponse: response{ // query just the store
				storeQueryInterval: &nonOverlappingQuery,
			},
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to 1h and queryIngestersWithin set to 2h, ingesterQueryStoreMaxLookback takes precedence",
			ingesterQueryStoreMaxLookback: time.Hour,
			queryIngestersWithin:          2 * time.Hour,
			overlappingQueryExpectedResponse: response{ // query ingesters for last 1h and store until last 1h.
				ingesterQueryInterval: &interval{
					start: time.Now().Add(-time.Hour),
					end:   overlappingQuery.end,
				},
				storeQueryInterval: &interval{
					start: overlappingQuery.start,
					end:   time.Now().Add(-time.Hour),
				},
			},
			nonOverlappingQueryExpectedResponse: response{ // query just the store
				storeQueryInterval: &nonOverlappingQuery,
			},
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to 2h and queryIngestersWithin set to 1h, ingesterQueryStoreMaxLookback takes precedence",
			ingesterQueryStoreMaxLookback: 2 * time.Hour,
			queryIngestersWithin:          time.Hour,
			overlappingQueryExpectedResponse: response{ // query ingesters for last 2h and store until last 2h.
				ingesterQueryInterval: &interval{
					start: time.Now().Add(-2 * time.Hour),
					end:   overlappingQuery.end,
				},
				storeQueryInterval: &interval{
					start: overlappingQuery.start,
					end:   time.Now().Add(-2 * time.Hour),
				},
			},
			nonOverlappingQueryExpectedResponse: response{ // query just the store
				storeQueryInterval: &nonOverlappingQuery,
			},
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to -1, query just ingesters",
			ingesterQueryStoreMaxLookback: -1,
			overlappingQueryExpectedResponse: response{
				ingesterQueryInterval: &overlappingQuery,
			},
			nonOverlappingQueryExpectedResponse: response{
				ingesterQueryInterval: &nonOverlappingQuery,
			},
		},
		{
			name:                 "queryIngestersWithin set to 1h",
			queryIngestersWithin: time.Hour,
			overlappingQueryExpectedResponse: response{ // query both store and ingesters since query overlaps queryIngestersWithin
				ingesterQueryInterval: &overlappingQuery,
				storeQueryInterval:    &overlappingQuery,
			},
			nonOverlappingQueryExpectedResponse: response{ // query just the store since query doesn't overlap queryIngestersWithin
				storeQueryInterval: &nonOverlappingQuery,
			},
		},
		{
			name:                 "queryIngestersWithin set to 10h",
			queryIngestersWithin: 10 * time.Hour,
			overlappingQueryExpectedResponse: response{ // query both store and ingesters since query overlaps queryIngestersWithin
				ingesterQueryInterval: &overlappingQuery,
				storeQueryInterval:    &overlappingQuery,
			},
			nonOverlappingQueryExpectedResponse: response{ // query just the store since query doesn't overlap queryIngestersWithin
				storeQueryInterval: &nonOverlappingQuery,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			querier := SingleTenantQuerier{cfg: Config{
				IngesterQueryStoreMaxLookback: tc.ingesterQueryStoreMaxLookback,
				QueryIngestersWithin:          tc.queryIngestersWithin,
			}}

			ingesterQueryInterval, storeQueryInterval := querier.buildQueryIntervals(overlappingQuery.start, overlappingQuery.end)
			compareResponse(t, tc.overlappingQueryExpectedResponse, response{
				ingesterQueryInterval: ingesterQueryInterval,
				storeQueryInterval:    storeQueryInterval,
			})

			ingesterQueryInterval, storeQueryInterval = querier.buildQueryIntervals(nonOverlappingQuery.start, nonOverlappingQuery.end)
			compareResponse(t, tc.nonOverlappingQueryExpectedResponse, response{
				ingesterQueryInterval: ingesterQueryInterval,
				storeQueryInterval:    storeQueryInterval,
			})
		})
	}
}

func TestQuerier_calculateIngesterMaxLookbackPeriod(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		ingesterQueryStoreMaxLookback time.Duration
		queryIngestersWithin          time.Duration
		expected                      time.Duration
	}{
		{
			name:     "defaults are set; infinite lookback period if no values are set",
			expected: -1,
		},
		{
			name:                          "only setting ingesterQueryStoreMaxLookback",
			ingesterQueryStoreMaxLookback: time.Hour,
			expected:                      time.Hour,
		},
		{
			name:                          "setting both ingesterQueryStoreMaxLookback and queryIngestersWithin; ingesterQueryStoreMaxLookback takes precedence",
			ingesterQueryStoreMaxLookback: time.Hour,
			queryIngestersWithin:          time.Minute,
			expected:                      time.Hour,
		},
		{
			name:                 "only setting queryIngestersWithin",
			queryIngestersWithin: time.Minute,
			expected:             time.Minute,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			querier := SingleTenantQuerier{cfg: Config{
				IngesterQueryStoreMaxLookback: tc.ingesterQueryStoreMaxLookback,
				QueryIngestersWithin:          tc.queryIngestersWithin,
			}}

			assert.Equal(t, tc.expected, querier.calculateIngesterMaxLookbackPeriod())
		})
	}
}

func TestQuerier_isWithinIngesterMaxLookbackPeriod(t *testing.T) {
	overlappingQuery := interval{
		start: time.Now().Add(-6 * time.Hour),
		end:   time.Now(),
	}

	nonOverlappingQuery := interval{
		start: time.Now().Add(-24 * time.Hour),
		end:   time.Now().Add(-12 * time.Hour),
	}

	for _, tc := range []struct {
		name                          string
		ingesterQueryStoreMaxLookback time.Duration
		queryIngestersWithin          time.Duration
		overlappingWithinRange        bool
		nonOverlappingWithinRange     bool
	}{
		{
			name:                      "default values, query ingesters and store for whole duration",
			overlappingWithinRange:    true,
			nonOverlappingWithinRange: true,
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to 1h",
			ingesterQueryStoreMaxLookback: time.Hour,
			overlappingWithinRange:        true,
			nonOverlappingWithinRange:     false,
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to 10h",
			ingesterQueryStoreMaxLookback: 10 * time.Hour,
			overlappingWithinRange:        true,
			nonOverlappingWithinRange:     false,
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to 1h and queryIngestersWithin set to 16h, ingesterQueryStoreMaxLookback takes precedence",
			ingesterQueryStoreMaxLookback: time.Hour,
			queryIngestersWithin:          16 * time.Hour, // if used, this would put the nonOverlapping query in range
			overlappingWithinRange:        true,
			nonOverlappingWithinRange:     false,
		},
		{
			name:                          "ingesterQueryStoreMaxLookback set to -1, query just ingesters",
			ingesterQueryStoreMaxLookback: -1,
			overlappingWithinRange:        true,
			nonOverlappingWithinRange:     true,
		},
		{
			name:                      "queryIngestersWithin set to 1h",
			queryIngestersWithin:      time.Hour,
			overlappingWithinRange:    true,
			nonOverlappingWithinRange: false,
		},
		{
			name:                      "queryIngestersWithin set to 10h",
			queryIngestersWithin:      10 * time.Hour,
			overlappingWithinRange:    true,
			nonOverlappingWithinRange: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			querier := SingleTenantQuerier{cfg: Config{
				IngesterQueryStoreMaxLookback: tc.ingesterQueryStoreMaxLookback,
				QueryIngestersWithin:          tc.queryIngestersWithin,
			}}

			lookbackPeriod := querier.calculateIngesterMaxLookbackPeriod()
			assert.Equal(t, tc.overlappingWithinRange, querier.isWithinIngesterMaxLookbackPeriod(lookbackPeriod, overlappingQuery.end))
			assert.Equal(t, tc.nonOverlappingWithinRange, querier.isWithinIngesterMaxLookbackPeriod(lookbackPeriod, nonOverlappingQuery.end))
		})
	}
}

func TestQuerier_RequestingIngesters(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "test")

	requestMapping := map[string]struct {
		ingesterMethod string
		storeMethod    string
	}{
		"SelectLogs": {
			ingesterMethod: "Query",
			storeMethod:    "SelectLogs",
		},
		"SelectSamples": {
			ingesterMethod: "QuerySample",
			storeMethod:    "SelectSamples",
		},
		"LabelValuesForMetricName": {
			ingesterMethod: "Label",
			storeMethod:    "LabelValuesForMetricName",
		},
		"LabelNamesForMetricName": {
			ingesterMethod: "Label",
			storeMethod:    "LabelNamesForMetricName",
		},
		"Series": {
			ingesterMethod: "Series",
			storeMethod:    "SelectSeries",
		},
	}

	tests := []struct {
		desc                             string
		start, end                       time.Time
		setIngesterQueryStoreMaxLookback bool
		expectedCallsStore               int
		expectedCallsIngesters           int
	}{
		{
			desc:                   "Data in storage and ingesters",
			start:                  time.Now().Add(-time.Hour * 2),
			end:                    time.Now(),
			expectedCallsStore:     1,
			expectedCallsIngesters: 1,
		},
		{
			desc:                   "Data in ingesters (IngesterQueryStoreMaxLookback not set)",
			start:                  time.Now().Add(-time.Minute * 15),
			end:                    time.Now(),
			expectedCallsStore:     1,
			expectedCallsIngesters: 1,
		},
		{
			desc:                   "Data only in storage",
			start:                  time.Now().Add(-time.Hour * 2),
			end:                    time.Now().Add(-time.Hour * 1),
			expectedCallsStore:     1,
			expectedCallsIngesters: 0,
		},
		{
			desc:                             "Data in ingesters (IngesterQueryStoreMaxLookback set)",
			start:                            time.Now().Add(-time.Minute * 15),
			end:                              time.Now(),
			setIngesterQueryStoreMaxLookback: true,
			expectedCallsStore:               0,
			expectedCallsIngesters:           1,
		},
	}

	requests := []struct {
		name string
		do   func(querier *SingleTenantQuerier, start, end time.Time) error
	}{
		{
			name: "SelectLogs",
			do: func(querier *SingleTenantQuerier, start, end time.Time) error {
				_, err := querier.SelectLogs(ctx, logql.SelectLogParams{
					QueryRequest: &logproto.QueryRequest{
						Selector:  `{type="test", fail="yes"} |= "foo"`,
						Limit:     10,
						Start:     start,
						End:       end,
						Direction: logproto.FORWARD,
						Plan: &plan.QueryPlan{
							AST: syntax.MustParseExpr(`{type="test", fail="yes"} |= "foo"`),
						},
					},
				})

				return err
			},
		},
		{
			name: "SelectSamples",
			do: func(querier *SingleTenantQuerier, start, end time.Time) error {
				_, err := querier.SelectSamples(ctx, logql.SelectSampleParams{
					SampleQueryRequest: &logproto.SampleQueryRequest{
						Selector: `count_over_time({foo="bar"}[5m])`,
						Start:    start,
						End:      end,
						Plan: &plan.QueryPlan{
							AST: syntax.MustParseExpr(`count_over_time({foo="bar"}[5m])`),
						},
					},
				})
				return err
			},
		},
		{
			name: "LabelValuesForMetricName",
			do: func(querier *SingleTenantQuerier, start, end time.Time) error {
				_, err := querier.Label(ctx, &logproto.LabelRequest{
					Name:   "type",
					Values: true,
					Start:  &start,
					End:    &end,
				})
				return err
			},
		},
		{
			name: "LabelNamesForMetricName",
			do: func(querier *SingleTenantQuerier, start, end time.Time) error {
				_, err := querier.Label(ctx, &logproto.LabelRequest{
					Values: false,
					Start:  &start,
					End:    &end,
				})
				return err
			},
		},
		{
			name: "Series",
			do: func(querier *SingleTenantQuerier, start, end time.Time) error {
				_, err := querier.Series(ctx, &logproto.SeriesRequest{
					Start: start,
					End:   end,
				})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			conf := mockQuerierConfig()
			conf.QueryIngestersWithin = time.Minute * 30
			if tc.setIngesterQueryStoreMaxLookback {
				conf.IngesterQueryStoreMaxLookback = conf.QueryIngestersWithin
			}

			limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
			require.NoError(t, err)

			for _, request := range requests {
				t.Run(request.name, func(t *testing.T) {
					ingesterClient, store, querier, err := setupIngesterQuerierMocks(conf, limits)
					require.NoError(t, err)

					err = request.do(querier, tc.start, tc.end)
					require.NoError(t, err)

					callsIngesters := ingesterClient.GetMockedCallsByMethod(requestMapping[request.name].ingesterMethod)
					assert.Equal(t, tc.expectedCallsIngesters, len(callsIngesters))

					callsStore := store.GetMockedCallsByMethod(requestMapping[request.name].storeMethod)
					assert.Equal(t, tc.expectedCallsStore, len(callsStore))
				})
			}
		})
	}
}

func TestQuerier_Volumes(t *testing.T) {
	t.Run("it returns volumes from the store", func(t *testing.T) {
		ret := &logproto.VolumeResponse{Volumes: []logproto.Volume{
			{Name: "foo", Volume: 38},
		}}

		limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
		require.NoError(t, err)

		ingesterClient := newQuerierClientMock()
		store := newStoreMock()
		store.On("Volume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ret, nil)

		conf := mockQuerierConfig()
		conf.QueryIngestersWithin = time.Minute * 30
		conf.IngesterQueryStoreMaxLookback = conf.QueryIngestersWithin

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			store, limits)
		require.NoError(t, err)

		now := time.Now()
		from := model.TimeFromUnix(now.Add(-1 * time.Hour).Unix())
		through := model.TimeFromUnix(now.Add(-35 * time.Minute).Unix())
		req := &logproto.VolumeRequest{From: from, Through: through, Matchers: `{}`, Limit: 10}
		ctx := user.InjectOrgID(context.Background(), "test")
		resp, err := querier.Volume(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []logproto.Volume{{Name: "foo", Volume: 38}}, resp.Volumes)
	})

	t.Run("it returns volumes from the ingester", func(t *testing.T) {
		ret := &logproto.VolumeResponse{Volumes: []logproto.Volume{
			{Name: "foo", Volume: 38},
		}}

		limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
		require.NoError(t, err)

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("GetVolume", mock.Anything, mock.Anything, mock.Anything).Return(ret, nil)

		store := newStoreMock()

		conf := mockQuerierConfig()
		conf.QueryIngestersWithin = time.Minute * 30
		conf.IngesterQueryStoreMaxLookback = conf.QueryIngestersWithin

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			store, limits)
		require.NoError(t, err)

		now := time.Now()
		from := model.TimeFromUnix(now.Add(-15 * time.Minute).Unix())
		through := model.TimeFromUnix(now.Unix())
		req := &logproto.VolumeRequest{From: from, Through: through, Matchers: `{}`, Limit: 10}
		ctx := user.InjectOrgID(context.Background(), "test")
		resp, err := querier.Volume(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []logproto.Volume{{Name: "foo", Volume: 38}}, resp.Volumes)
	})

	t.Run("it merges volumes from the store and ingester", func(t *testing.T) {
		ret := &logproto.VolumeResponse{Volumes: []logproto.Volume{
			{Name: "foo", Volume: 38},
		}}

		limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
		require.NoError(t, err)

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("GetVolume", mock.Anything, mock.Anything, mock.Anything).Return(ret, nil)

		store := newStoreMock()
		store.On("Volume", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(ret, nil)

		conf := mockQuerierConfig()
		conf.QueryIngestersWithin = time.Minute * 30
		conf.IngesterQueryStoreMaxLookback = conf.QueryIngestersWithin

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			store, limits)
		require.NoError(t, err)

		now := time.Now()
		from := model.TimeFromUnix(now.Add(-time.Hour).Unix())
		through := model.TimeFromUnix(now.Unix())
		req := &logproto.VolumeRequest{From: from, Through: through, Matchers: `{}`, Limit: 10}
		ctx := user.InjectOrgID(context.Background(), "test")
		resp, err := querier.Volume(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []logproto.Volume{{Name: "foo", Volume: 76}}, resp.Volumes)
	})
}

func setupIngesterQuerierMocks(conf Config, limits *validation.Overrides) (*querierClientMock, *storeMock, *SingleTenantQuerier, error) {
	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 1)}), nil)

	querySampleClient := newQuerySampleClientMock()
	querySampleClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 1)}), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)
	ingesterClient.On("QuerySample", mock.Anything, mock.Anything, mock.Anything).Return(querySampleClient, nil)
	ingesterClient.On("Label", mock.Anything, mock.Anything, mock.Anything).Return(mockLabelResponse([]string{"bar"}), nil)
	ingesterClient.On("Series", mock.Anything, mock.Anything, mock.Anything).Return(&logproto.SeriesResponse{
		Series: []logproto.SeriesIdentifier{
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "bar", Value: "1"}},
			},
		},
	}, nil)
	ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything).Return(&logproto.DetectedLabelsResponse{
		DetectedLabels: []*logproto.DetectedLabel{
			{Label: "pod", Cardinality: 1},
			{Label: "namespace", Cardinality: 3},
			{Label: "customerId", Cardinality: 200},
		},
	}, nil)

	store := newStoreMock()
	store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(0, 1), nil)
	store.On("SelectSamples", mock.Anything, mock.Anything).Return(mockSampleIterator(querySampleClient), nil)
	store.On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{"1", "2", "3"}, nil)
	store.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{"foo"}, nil)
	store.On("SelectSeries", mock.Anything, mock.Anything).Return([]logproto.SeriesIdentifier{
		{Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "foo", Value: "1"}}},
	}, nil)

	querier, err := newQuerier(
		conf,
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		&mockDeleteGettter{},
		store, limits)
	if err != nil {
		return nil, nil, nil, err
	}

	return ingesterClient, store, querier, nil
}

type fakeTimeLimits struct {
	maxQueryLookback time.Duration
	maxQueryLength   time.Duration
}

func (f fakeTimeLimits) MaxQueryLookback(_ context.Context, _ string) time.Duration {
	return f.maxQueryLookback
}

func (f fakeTimeLimits) MaxQueryLength(_ context.Context, _ string) time.Duration {
	return f.maxQueryLength
}

func Test_validateQueryTimeRangeLimits(t *testing.T) {
	now := time.Now()
	nowFunc = func() time.Time { return now }
	tests := []struct {
		name        string
		limits      TimeRangeLimits
		from        time.Time
		through     time.Time
		wantFrom    time.Time
		wantThrough time.Time
		wantErr     bool
	}{
		{"no change", fakeTimeLimits{1000 * time.Hour, 1000 * time.Hour}, now, now.Add(24 * time.Hour), now, now.Add(24 * time.Hour), false},
		{"clamped to 24h", fakeTimeLimits{24 * time.Hour, 1000 * time.Hour}, now.Add(-48 * time.Hour), now, now.Add(-24 * time.Hour), now, false},
		{"end before start", fakeTimeLimits{}, now, now.Add(-48 * time.Hour), time.Time{}, time.Time{}, true},
		{"query too long", fakeTimeLimits{maxQueryLength: 24 * time.Hour}, now.Add(-48 * time.Hour), now, time.Time{}, time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, through, err := validateQueryTimeRangeLimits(context.Background(), "foo", tt.limits, tt.from, tt.through)
			if tt.wantErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
			require.Equal(t, tt.wantFrom, from, "wanted (%s) got (%s)", tt.wantFrom, from)
			require.Equal(t, tt.wantThrough, through)
		})
	}
}

func TestQuerier_SelectLogWithDeletes(t *testing.T) {
	store := newStoreMock()
	store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(1, 2), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 2)}), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	delGetter := &mockDeleteGettter{
		results: []deletion.DeleteRequest{
			{Query: `0`, StartTime: 0, EndTime: 100},
			{Query: `1`, StartTime: 200, EndTime: 400},
			{Query: `2`, StartTime: 400, EndTime: 500},
			{Query: `3`, StartTime: 500, EndTime: 700},
			{Query: `4`, StartTime: 700, EndTime: 900},
		},
	}

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		delGetter, store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")

	request := logproto.QueryRequest{
		Selector:  `{type="test"} |= "foo"`,
		Limit:     10,
		Start:     time.Unix(0, 300000000),
		End:       time.Unix(0, 600000000),
		Direction: logproto.FORWARD,
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{type="test"} |= "foo"`),
		},
	}

	_, err = q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &request})
	require.NoError(t, err)

	expectedRequest := &logproto.QueryRequest{
		Selector:  request.Selector,
		Limit:     request.Limit,
		Start:     request.Start,
		End:       request.End,
		Direction: request.Direction,
		Deletes: []*logproto.Delete{
			{Selector: "1", Start: 200000000, End: 400000000},
			{Selector: "2", Start: 400000000, End: 500000000},
			{Selector: "3", Start: 500000000, End: 700000000},
		},
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(request.Selector),
		},
	}

	require.Contains(t, store.Calls[0].Arguments, logql.SelectLogParams{QueryRequest: expectedRequest})
	require.Contains(t, ingesterClient.Calls[0].Arguments, expectedRequest)
	require.Equal(t, "test", delGetter.user)
}

func TestQuerier_SelectSamplesWithDeletes(t *testing.T) {
	queryClient := newQuerySampleClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(1, 2)}), nil)

	store := newStoreMock()
	store.On("SelectSamples", mock.Anything, mock.Anything).Return(mockSampleIterator(queryClient), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("QuerySample", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	delGetter := &mockDeleteGettter{
		results: []deletion.DeleteRequest{
			{Query: `0`, StartTime: 0, EndTime: 100},
			{Query: `1`, StartTime: 200, EndTime: 400},
			{Query: `2`, StartTime: 400, EndTime: 500},
			{Query: `3`, StartTime: 500, EndTime: 700},
			{Query: `4`, StartTime: 700, EndTime: 900},
		},
	}

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		delGetter, store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")

	request := logproto.SampleQueryRequest{
		Selector: `count_over_time({foo="bar"}[5m])`,
		Start:    time.Unix(0, 300000000),
		End:      time.Unix(0, 600000000),
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`count_over_time({foo="bar"}[5m])`),
		},
	}

	_, err = q.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: &request})
	require.NoError(t, err)

	expectedRequest := logql.SelectSampleParams{
		SampleQueryRequest: &logproto.SampleQueryRequest{
			Selector: request.Selector,
			Start:    request.Start,
			End:      request.End,
			Deletes: []*logproto.Delete{
				{Selector: "1", Start: 200000000, End: 400000000},
				{Selector: "2", Start: 400000000, End: 500000000},
				{Selector: "3", Start: 500000000, End: 700000000},
			},
			Plan: &plan.QueryPlan{
				AST: syntax.MustParseExpr(request.Selector),
			},
		},
	}

	require.Contains(t, store.Calls[0].Arguments, expectedRequest)
	require.Contains(t, ingesterClient.Calls[0].Arguments, expectedRequest.SampleQueryRequest)
	require.Equal(t, "test", delGetter.user)
}

func newQuerier(cfg Config, clientCfg client.Config, clientFactory ring_client.PoolFactory, ring ring.ReadRing, dg *mockDeleteGettter, store storage.Store, limits *validation.Overrides) (*SingleTenantQuerier, error) {
	iq, err := newIngesterQuerier(clientCfg, ring, cfg.ExtraQueryDelay, clientFactory, constants.Loki, util_log.Logger)
	if err != nil {
		return nil, err
	}

	return New(cfg, store, iq, limits, dg, nil, log.NewNopLogger())
}

type mockDeleteGettter struct {
	user    string
	results []deletion.DeleteRequest
}

func (d *mockDeleteGettter) GetAllDeleteRequestsForUser(_ context.Context, userID string) ([]deletion.DeleteRequest, error) {
	d.user = userID
	return d.results, nil
}

func TestQuerier_DetectedLabels(t *testing.T) {
	manyValues := []string{}
	now := time.Now()
	for i := 0; i < 60; i++ {
		manyValues = append(manyValues, "a"+strconv.Itoa(i))
	}

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	ctx := user.InjectOrgID(context.Background(), "test")

	conf := mockQuerierConfig()
	conf.IngesterQueryStoreMaxLookback = 0

	request := logproto.DetectedLabelsRequest{
		Start: now,
		End:   now,
		Query: "",
	}

	t.Run("when both store and ingester responses are present, a combined response is returned", func(t *testing.T) {
		ingesterResponse := logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
			"cluster":       {Values: []string{"ingester"}},
			"ingesterLabel": {Values: []string{"abc", "def", "ghi", "abc"}},
		}}

		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&ingesterResponse, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{"storeLabel"}, nil).
			On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "storeLabel", mock.Anything).
			Return([]string{"val1", "val2"}, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)

		calls := ingesterClient.GetMockedCallsByMethod("GetDetectedLabels")
		assert.Equal(t, 1, len(calls))

		detectedLabels := resp.DetectedLabels
		assert.Len(t, detectedLabels, 3)
		expectedCardinality := map[string]uint64{"storeLabel": 2, "ingesterLabel": 3, "cluster": 1}
		for _, d := range detectedLabels {
			card := expectedCardinality[d.Label]
			assert.Equal(t, d.Cardinality, card, "Expected cardinality mismatch for: ", d.Label)
		}
	})

	t.Run("when both store and ingester responses are present, duplicates are removed", func(t *testing.T) {
		ingesterResponse := logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
			"cluster":       {Values: []string{"ingester"}},
			"ingesterLabel": {Values: []string{"abc", "def", "ghi", "abc"}},
			"commonLabel":   {Values: []string{"abc", "def", "ghi", "abc"}},
		}}

		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&ingesterResponse, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{"storeLabel", "commonLabel"}, nil).
			On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "storeLabel", mock.Anything).
			Return([]string{"val1", "val2"}, nil).
			On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "commonLabel", mock.Anything).
			Return([]string{"def", "xyz", "lmo", "abc"}, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)

		calls := ingesterClient.GetMockedCallsByMethod("GetDetectedLabels")
		assert.Equal(t, 1, len(calls))

		detectedLabels := resp.DetectedLabels
		assert.Len(t, detectedLabels, 4)

		expectedCardinality := map[string]uint64{"storeLabel": 2, "ingesterLabel": 3, "cluster": 1, "commonLabel": 5}
		for _, d := range detectedLabels {
			card := expectedCardinality[d.Label]
			assert.Equal(t, d.Cardinality, card, "Expected cardinality mismatch for: ", d.Label)
		}
	})

	t.Run("returns a response when ingester data is empty", func(t *testing.T) {
		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&logproto.LabelToValuesResponse{}, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{"storeLabel1", "storeLabel2"}, nil).
			On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "storeLabel1", mock.Anything).
			Return([]string{"val1", "val2"}, nil).
			On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "storeLabel2", mock.Anything).
			Return([]string{"val1", "val2"}, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)

		detectedLabels := resp.DetectedLabels
		assert.Len(t, detectedLabels, 2)
		expectedCardinality := map[string]uint64{"storeLabel1": 2, "storeLabel2": 2}
		for _, d := range detectedLabels {
			card := expectedCardinality[d.Label]
			assert.Equal(t, d.Cardinality, card, "Expected cardinality mismatch for: ", d.Label)
		}
	})

	t.Run("label is not present when the values are nil", func(t *testing.T) {
		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&logproto.LabelToValuesResponse{}, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{"storeLabel1", "pod"}, nil).
			On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "storeLabel1", mock.Anything).
			Return([]string{"val1", "val2"}, nil).
			On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "pod", mock.Anything).
			Return(nil, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)

		detectedLabels := resp.DetectedLabels
		assert.Len(t, detectedLabels, 1)
		expectedCardinality := map[string]uint64{"storeLabel1": 2}
		for _, d := range detectedLabels {
			card := expectedCardinality[d.Label]
			assert.Equal(t, d.Cardinality, card, "Expected cardinality mismatch for: ", d.Label)
		}
	})

	t.Run("returns a response when store data is empty", func(t *testing.T) {
		ingesterResponse := logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
			"cluster":       {Values: []string{"ingester"}},
			"ingesterLabel": {Values: []string{"abc", "def", "ghi", "abc"}},
		}}

		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&ingesterResponse, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{}, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)

		detectedLabels := resp.DetectedLabels
		assert.Len(t, detectedLabels, 2)
		expectedCardinality := map[string]uint64{"cluster": 1, "ingesterLabel": 3}
		for _, d := range detectedLabels {
			card := expectedCardinality[d.Label]
			assert.Equal(t, d.Cardinality, card, "Expected cardinality mismatch for: ", d.Label)
		}
	})

	t.Run("id types like uuids, guids and numbers are not relevant detected labels", func(t *testing.T) {
		ingesterResponse := logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
			"all-ints":   {Values: []string{"1", "2", "3", "4"}},
			"all-floats": {Values: []string{"1.2", "2.3", "3.4", "4.5"}},
			"all-uuids":  {Values: []string{"751e8ee6-b377-4b2e-b7b5-5508fbe980ef", "6b7e2663-8ecb-42e1-8bdc-0c5de70185b3", "2e1e67ff-be4f-47b8-aee1-5d67ff1ddabf", "c95b2d62-74ed-4ed7-a8a1-eb72fc67946e"}},
		}}

		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&ingesterResponse, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{}, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)

		detectedLabels := resp.DetectedLabels
		assert.Len(t, detectedLabels, 0)
	})

	t.Run("static labels are always returned no matter their cardinality or value types", func(t *testing.T) {
		ingesterResponse := logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
			"cluster":   {Values: []string{"val1"}},
			"namespace": {Values: manyValues},
			"pod":       {Values: []string{"1", "2", "3", "4"}},
		}}

		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(&ingesterResponse, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{}, nil)
		request := logproto.DetectedLabelsRequest{
			Start: now,
			End:   now,
			Query: "",
		}

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)

		detectedLabels := resp.DetectedLabels
		assert.Len(t, detectedLabels, 3)
		expectedCardinality := map[string]uint64{"cluster": 1, "pod": 4, "namespace": 60}
		for _, d := range detectedLabels {
			card := expectedCardinality[d.Label]
			assert.Equal(t, d.Cardinality, card, "Expected cardinality mismatch for: ", d.Label)
		}
	})

	t.Run("no panics with ingester response is nil", func(t *testing.T) {
		ingesterClient := newQuerierClientMock()
		storeClient := newStoreMock()

		ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil, nil)
		storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return([]string{}, nil)
		request := logproto.DetectedLabelsRequest{
			Start: now,
			End:   now.Add(2 * time.Hour),
			Query: "",
		}

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			storeClient, limits)
		require.NoError(t, err)

		_, err = querier.DetectedLabels(ctx, &request)
		require.NoError(t, err)
	})
}

func BenchmarkQuerierDetectedLabels(b *testing.B) {
	now := time.Now()

	limits, _ := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	ctx := user.InjectOrgID(context.Background(), "test")

	conf := mockQuerierConfig()
	conf.IngesterQueryStoreMaxLookback = 0

	request := logproto.DetectedLabelsRequest{
		Start: now,
		End:   now,
		Query: "",
	}
	ingesterResponse := logproto.LabelToValuesResponse{Labels: map[string]*logproto.UniqueLabelValues{
		"cluster":       {Values: []string{"ingester"}},
		"ingesterLabel": {Values: []string{"abc", "def", "ghi", "abc"}},
	}}

	ingesterClient := newQuerierClientMock()
	storeClient := newStoreMock()

	ingesterClient.On("GetDetectedLabels", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&ingesterResponse, nil)
	storeClient.On("LabelNamesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"storeLabel"}, nil).
		On("LabelValuesForMetricName", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "storeLabel", mock.Anything).
		Return([]string{"val1", "val2"}, nil)

	querier, _ := newQuerier(
		conf,
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		&mockDeleteGettter{},
		storeClient, limits)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := querier.DetectedLabels(ctx, &request)
		assert.NoError(b, err)
	}
}

func TestQuerier_DetectedFields(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	ctx := user.InjectOrgID(context.Background(), "test")

	conf := mockQuerierConfig()
	conf.IngesterQueryStoreMaxLookback = 0

	request := logproto.DetectedFieldsRequest{
		Start:      time.Now().Add(-1 * time.Minute),
		End:        time.Now(),
		Query:      `{type="test"}`,
		LineLimit:  1000,
		FieldLimit: 1000,
	}

	t.Run("returns detected fields from queried logs", func(t *testing.T) {
		store := newStoreMock()
		store.On("SelectLogs", mock.Anything, mock.Anything).
			Return(mockLogfmtStreamIterator(1, 5), nil)

		queryClient := newQueryClientMock()
		queryClient.On("Recv").
			Return(mockQueryResponse([]logproto.Stream{mockLogfmtStream(1, 5)}), nil)

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).
			Return(queryClient, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			store, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedFields(ctx, &request)
		require.NoError(t, err)

		detectedFields := resp.Fields
		// log lines come from querier_mock_test.go
		// message="line %d" count=%d fake=true bytes=%dMB duration=%dms percent=%f even=%t
		assert.Len(t, detectedFields, 8)
		expectedCardinality := map[string]uint64{
			"message":        5,
			"count":          5,
			"fake":           1,
			"bytes":          5,
			"duration":       5,
			"percent":        5,
			"even":           2,
			"name_extracted": 1,
		}
		for _, d := range detectedFields {
			card := expectedCardinality[d.Label]
			assert.Equal(t, card, d.Cardinality, "Expected cardinality mismatch for: %s", d.Label)
		}
	})

	t.Run("returns detected fields with structured metadata from queried logs", func(t *testing.T) {
		store := newStoreMock()
		store.On("SelectLogs", mock.Anything, mock.Anything).
			Return(mockLogfmtStreamIterator(1, 5), nil)

		queryClient := newQueryClientMock()
		queryClient.On("Recv").
			Return(mockQueryResponse([]logproto.Stream{mockLogfmtStreamWithStructuredMetadata(1, 5)}), nil)

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).
			Return(queryClient, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			store, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedFields(ctx, &request)
		require.NoError(t, err)

		detectedFields := resp.Fields
		// log lines come from querier_mock_test.go
		// message="line %d" count=%d fake=true bytes=%dMB duration=%dms percent=%f even=%t
		assert.Len(t, detectedFields, 10)
		expectedCardinality := map[string]uint64{
			"variable":       5,
			"constant":       1,
			"message":        5,
			"count":          5,
			"fake":           1,
			"bytes":          5,
			"duration":       5,
			"percent":        5,
			"even":           2,
			"name_extracted": 1,
		}
		for _, d := range detectedFields {
			card := expectedCardinality[d.Label]
			assert.Equal(t, card, d.Cardinality, "Expected cardinality mismatch for: %s", d.Label)
		}
	})

	t.Run("correctly identifies different field types", func(t *testing.T) {
		store := newStoreMock()
		store.On("SelectLogs", mock.Anything, mock.Anything).
			Return(mockLogfmtStreamIterator(1, 2), nil)

		queryClient := newQueryClientMock()
		queryClient.On("Recv").
			Return(mockQueryResponse([]logproto.Stream{mockLogfmtStream(1, 2)}), nil)

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).
			Return(queryClient, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			store, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedFields(ctx, &request)
		require.NoError(t, err)

		detectedFields := resp.Fields
		// log lines come from querier_mock_test.go
		// message="line %d" count=%d fake=true bytes=%dMB duration=%dms percent=%f even=%t
		assert.Len(t, detectedFields, 8)

		var messageField, countField, bytesField, durationField, floatField, evenField *logproto.DetectedField
		for _, field := range detectedFields {
			switch field.Label {
			case "message":
				messageField = field
			case "count":
				countField = field
			case "bytes":
				bytesField = field
			case "duration":
				durationField = field
			case "percent":
				floatField = field
			case "even":
				evenField = field
			}
		}

		assert.Equal(t, logproto.DetectedFieldString, messageField.Type)
		assert.Equal(t, logproto.DetectedFieldInt, countField.Type)
		assert.Equal(t, logproto.DetectedFieldBytes, bytesField.Type)
		assert.Equal(t, logproto.DetectedFieldDuration, durationField.Type)
		assert.Equal(t, logproto.DetectedFieldFloat, floatField.Type)
		assert.Equal(t, logproto.DetectedFieldBoolean, evenField.Type)
	})

	t.Run("correctly identifies parser to use with logfmt and structured metadata", func(t *testing.T) {
		store := newStoreMock()
		store.On("SelectLogs", mock.Anything, mock.Anything).
			Return(mockLogfmtStreamIterator(1, 2), nil)

		queryClient := newQueryClientMock()
		queryClient.On("Recv").
			Return(mockQueryResponse([]logproto.Stream{mockLogfmtStreamWithStructuredMetadata(1, 2)}), nil)

		ingesterClient := newQuerierClientMock()
		ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).
			Return(queryClient, nil)

		querier, err := newQuerier(
			conf,
			mockIngesterClientConfig(),
			newIngesterClientMockFactory(ingesterClient),
			mockReadRingWithOneActiveIngester(),
			&mockDeleteGettter{},
			store, limits)
		require.NoError(t, err)

		resp, err := querier.DetectedFields(ctx, &request)
		require.NoError(t, err)

		detectedFields := resp.Fields
		// log lines come from querier_mock_test.go
		// message="line %d" count=%d fake=true bytes=%dMB duration=%dms percent=%f even=%t
		assert.Len(t, detectedFields, 10)

		var messageField, countField, bytesField, durationField, floatField, evenField, constantField, variableField *logproto.DetectedField
		for _, field := range detectedFields {
			switch field.Label {
			case "message":
				messageField = field
			case "count":
				countField = field
			case "bytes":
				bytesField = field
			case "duration":
				durationField = field
			case "percent":
				floatField = field
			case "even":
				evenField = field
			case "constant":
				constantField = field
			case "variable":
				variableField = field
			}
		}

		assert.Equal(t, []string{"logfmt"}, messageField.Parsers)
		assert.Equal(t, []string{"logfmt"}, countField.Parsers)
		assert.Equal(t, []string{"logfmt"}, bytesField.Parsers)
		assert.Equal(t, []string{"logfmt"}, durationField.Parsers)
		assert.Equal(t, []string{"logfmt"}, floatField.Parsers)
		assert.Equal(t, []string{"logfmt"}, evenField.Parsers)
		assert.Equal(t, []string(nil), constantField.Parsers)
		assert.Equal(t, []string(nil), variableField.Parsers)
	},
	)

	t.Run(
		"adds _extracted suffix to detected fields that conflict with indexed labels",
		func(t *testing.T) {
			store := newStoreMock()
			store.On("SelectLogs", mock.Anything, mock.Anything).
				Return(mockLogfmtStreamIterator(1, 2), nil)

			queryClient := newQueryClientMock()
			queryClient.On("Recv").
				Return(mockQueryResponse([]logproto.Stream{mockLogfmtStreamWithStructuredMetadata(1, 2)}), nil)

			ingesterClient := newQuerierClientMock()
			ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).
				Return(queryClient, nil)

			querier, err := newQuerier(
				conf,
				mockIngesterClientConfig(),
				newIngesterClientMockFactory(ingesterClient),
				mockReadRingWithOneActiveIngester(),
				&mockDeleteGettter{},
				store, limits)
			require.NoError(t, err)

			resp, err := querier.DetectedFields(ctx, &request)
			require.NoError(t, err)

			detectedFields := resp.Fields
			// log lines come from querier_mock_test.go
			// message="line %d" count=%d fake=true bytes=%dMB duration=%dms percent=%f even=%t
			assert.Len(t, detectedFields, 10)

			var nameField *logproto.DetectedField
			for _, field := range detectedFields {
				switch field.Label {
				case "name_extracted":
					nameField = field
				}
			}

			assert.NotNil(t, nameField)
			assert.Equal(t, "name_extracted", nameField.Label)
			assert.Equal(t, logproto.DetectedFieldString, nameField.Type)
			assert.Equal(t, []string{"logfmt"}, nameField.Parsers)
			assert.Equal(t, uint64(1), nameField.Cardinality)
		},
	)
}

func BenchmarkQuerierDetectedFields(b *testing.B) {
	limits, _ := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	ctx := user.InjectOrgID(context.Background(), "test")

	conf := mockQuerierConfig()
	conf.IngesterQueryStoreMaxLookback = 0

	request := logproto.DetectedFieldsRequest{
		Start:      time.Now().Add(-1 * time.Minute),
		End:        time.Now(),
		Query:      `{type="test"}`,
		LineLimit:  1000,
		FieldLimit: 1000,
	}

	store := newStoreMock()
	store.On("SelectLogs", mock.Anything, mock.Anything).
		Return(mockLogfmtStreamIterator(1, 2), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").
		Return(mockQueryResponse([]logproto.Stream{mockLogfmtStream(1, 2)}), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).
		Return(queryClient, nil)

	querier, _ := newQuerier(
		conf,
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		&mockDeleteGettter{},
		store, limits)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := querier.DetectedFields(ctx, &request)
		assert.NoError(b, err)
	}
}

func Test_getParsersFromExpr(t *testing.T) {
	t.Run("detects logfmt parser", func(t *testing.T) {
		exprStr := `{foo="bar"} | logfmt`
		expr, err := syntax.ParseLogSelector(exprStr, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"logfmt"}, getParsersFromExpr(expr))
	})

	t.Run("detects json parser", func(t *testing.T) {
		exprStr := `{foo="bar"} | json`
		expr, err := syntax.ParseLogSelector(exprStr, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"json"}, getParsersFromExpr(expr))
	})

	t.Run("detects multiple parsers", func(t *testing.T) {
		exprStr := `{foo="bar"} | logfmt | json`
		expr, err := syntax.ParseLogSelector(exprStr, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"logfmt", "json"}, getParsersFromExpr(expr))
	})

	t.Run("detects logfmt expression parser", func(t *testing.T) {
		exprStr := `{foo="bar"} | logfmt msg="message"`
		expr, err := syntax.ParseLogSelector(exprStr, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"logfmt"}, getParsersFromExpr(expr))
	})

	t.Run("detects json expression parser", func(t *testing.T) {
		exprStr := `{foo="bar"} | json first_server="servers[0]"`
		expr, err := syntax.ParseLogSelector(exprStr, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"json"}, getParsersFromExpr(expr))
	})

	t.Run("detects multiple expression parsers", func(t *testing.T) {
		exprStr := `{foo="bar"} | logfmt msg="message" | json first_server="servers[0]"`
		expr, err := syntax.ParseLogSelector(exprStr, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"logfmt", "json"}, getParsersFromExpr(expr))
	})
}

func Test_parseDetectedFeilds(t *testing.T) {
	now := time.Now()

	t.Run("when no parsers are supplied", func(t *testing.T) {
		infoDetectdFiledMetadata := []push.LabelAdapter{
			{
				Name:  "detected_level",
				Value: "info",
			},
		}

		rulerLines := []push.Entry{
			{Timestamp: now, Line: "ts=2024-09-05T15:36:38.757788067Z caller=grpc_logging.go:66 tenant=2419 level=info method=/cortex.Ingester/Push duration=19.098s msg=gRPC", StructuredMetadata: infoDetectdFiledMetadata},
			{Timestamp: now, Line: "ts=2024-09-05T15:36:38.698375619Z caller=grpc_logging.go:66 tenant=29 level=info method=/cortex.Ingester/Push duration=5.471s msg=gRPC", StructuredMetadata: infoDetectdFiledMetadata},
			{Timestamp: now, Line: "ts=2024-09-05T15:36:38.629424175Z caller=grpc_logging.go:66 tenant=2919 level=info method=/cortex.Ingester/Push duration=29.234s msg=gRPC", StructuredMetadata: infoDetectdFiledMetadata},
		}

		rulerLbls := `{cluster="us-east-1", namespace="mimir-dev", pod="mimir-ruler-nfb37", service_name="mimir-ruler"}`
		rulerMetric, err := parser.ParseMetric(rulerLbls)
		require.NoError(t, err)

		rulerStream := push.Stream{
			Labels:  rulerLbls,
			Entries: rulerLines,
			Hash:    rulerMetric.Hash(),
		}

		debugDetectedFieldMetadata := []push.LabelAdapter{
			{
				Name:  "detected_level",
				Value: "debug",
			},
		}

		nginxJSONLines := []push.Entry{
			{Timestamp: now, Line: `{"host":"100.117.38.203", "user-identifier":"nader3722", "datetime":"05/Sep/2024:16:13:56 +0000", "method": "PATCH", "request": "/api/loki/v1/push", "protocol":"HTTP/2.0", "status":200, "bytes":9664, "referer": "https://www.seniorbleeding-edge.net/exploit/robust/whiteboard"}`, StructuredMetadata: debugDetectedFieldMetadata},
			{Timestamp: now, Line: `{"host":"66.134.9.30", "user-identifier":"-", "datetime":"05/Sep/2024:16:13:55 +0000", "method": "DELETE", "request": "/api/mimir/v1/push", "protocol":"HTTP/1.1", "status":200, "bytes":18688, "referer": "https://www.districtiterate.biz/synergistic/next-generation/extend"}`, StructuredMetadata: debugDetectedFieldMetadata},
			{Timestamp: now, Line: `{"host":"66.134.9.30", "user-identifier":"-", "datetime":"05/Sep/2024:16:13:55 +0000", "method": "GET", "request": "/api/loki/v1/label/names", "protocol":"HTTP/1.1", "status":200, "bytes":9314, "referer": "https://www.dynamicimplement.info/enterprise/distributed/incentivize/strategic"}`, StructuredMetadata: debugDetectedFieldMetadata},
		}

		nginxLbls := `{ cluster="eu-west-1", level="debug", namespace="gateway", pod="nginx-json-oghco", service_name="nginx-json" }`
		nginxMetric, err := parser.ParseMetric(nginxLbls)
		require.NoError(t, err)

		nginxStream := push.Stream{
			Labels:  nginxLbls,
			Entries: nginxJSONLines,
			Hash:    nginxMetric.Hash(),
		}

		t.Run("detect logfmt fields when with no supplied parsers", func(t *testing.T) {
			df := parseDetectedFields(uint32(15), logqlmodel.Streams([]push.Stream{rulerStream}))
			for _, expected := range []string{"ts", "caller", "tenant", "level", "method", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "logfmt", parsers[0])
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("detect json fields when with no supplied parsers", func(t *testing.T) {
			df := parseDetectedFields(uint32(15), logqlmodel.Streams([]push.Stream{nginxStream}))
			for _, expected := range []string{"host", "user_identifier", "datetime", "method", "request", "protocol", "status", "bytes", "referer"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "json", parsers[0])
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("detect mixed fields when with no supplied parsers", func(t *testing.T) {
			df := parseDetectedFields(uint32(20), logqlmodel.Streams([]push.Stream{rulerStream, nginxStream}))

			for _, expected := range []string{"ts", "caller", "tenant", "level", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1, "expected only logfmt parser for %s", expected)
				require.Equal(t, "logfmt", parsers[0], "expected only logfmt parser for %s", expected)
			}

			for _, expected := range []string{"host", "user_identifier", "datetime", "request", "protocol", "status", "bytes", "referer"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1, "expected only json parser for %s", expected)
				require.Equal(t, "json", parsers[0], "expected only json parser for %s", expected)
			}

			// multiple parsers for fields that exist in both streams
			for _, expected := range []string{"method"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 2, "expected logfmt and json parser for %s", expected)
				require.Contains(t, parsers, "logfmt", "expected logfmt parser for %s", expected)
				require.Contains(t, parsers, "json", "expected json parser for %s", expected)
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("correctly applies _extracted for a single stream", func(t *testing.T) {
			rulerLbls := `{cluster="us-east-1", namespace="mimir-dev", pod="mimir-ruler-nfb37", service_name="mimir-ruler", tenant="42", caller="inside-the-house"}`
			rulerMetric, err := parser.ParseMetric(rulerLbls)
			require.NoError(t, err)

			rulerStream := push.Stream{
				Labels:  rulerLbls,
				Entries: rulerLines,
				Hash:    rulerMetric.Hash(),
			}

			df := parseDetectedFields(uint32(15), logqlmodel.Streams([]push.Stream{rulerStream}))
			for _, expected := range []string{"ts", "caller_extracted", "tenant_extracted", "level", "method", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "logfmt", parsers[0])
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("correctly applies _extracted for multiple streams", func(t *testing.T) {
			rulerLbls := `{cluster="us-east-1", namespace="mimir-dev", pod="mimir-ruler-nfb37", service_name="mimir-ruler", tenant="42", caller="inside-the-house"}`
			rulerMetric, err := parser.ParseMetric(rulerLbls)
			require.NoError(t, err)

			rulerStream := push.Stream{
				Labels:  rulerLbls,
				Entries: rulerLines,
				Hash:    rulerMetric.Hash(),
			}

			nginxLbls := `{ cluster="eu-west-1", level="debug", namespace="gateway", pod="nginx-json-oghco", service_name="nginx-json", host="localhost"}`
			nginxMetric, err := parser.ParseMetric(nginxLbls)
			require.NoError(t, err)

			nginxStream := push.Stream{
				Labels:  nginxLbls,
				Entries: nginxJSONLines,
				Hash:    nginxMetric.Hash(),
			}

			df := parseDetectedFields(uint32(20), logqlmodel.Streams([]push.Stream{rulerStream, nginxStream}))
			for _, expected := range []string{"ts", "caller_extracted", "tenant_extracted", "level", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "logfmt", parsers[0])
			}

			for _, expected := range []string{"host_extracted", "user_identifier", "datetime", "request", "protocol", "status", "bytes", "referer"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1, "expected only json parser for %s", expected)
				require.Equal(t, "json", parsers[0], "expected only json parser for %s", expected)
			}

			// multiple parsers for fields that exist in both streams
			for _, expected := range []string{"method"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 2, "expected logfmt and json parser for %s", expected)
				require.Contains(t, parsers, "logfmt", "expected logfmt parser for %s", expected)
				require.Contains(t, parsers, "json", "expected json parser for %s", expected)
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})
	})

	t.Run("when parsers are supplied", func(t *testing.T) {
		infoDetectdFiledMetadata := []push.LabelAdapter{
			{
				Name:  "detected_level",
				Value: "info",
			},
		}

		parsedRulerFields := func(ts, tenant, duration string) []push.LabelAdapter {
			return []push.LabelAdapter{
				{
					Name:  "ts",
					Value: ts,
				},
				{
					Name:  "caller",
					Value: "grpc_logging.go:66",
				},
				{
					Name:  "tenant",
					Value: tenant,
				},
				{
					Name:  "level",
					Value: "info",
				},
				{
					Name:  "method",
					Value: "/cortex.Ingester/Push",
				},
				{
					Name:  "duration",
					Value: duration,
				},
				{
					Name:  "msg",
					Value: "gRPC",
				},
			}
		}

		rulerLines := []push.Entry{
			{
				Timestamp:          now,
				Line:               "ts=2024-09-05T15:36:38.757788067Z caller=grpc_logging.go:66 tenant=2419 level=info method=/cortex.Ingester/Push duration=19.098s msg=gRPC",
				StructuredMetadata: infoDetectdFiledMetadata,
				Parsed:             parsedRulerFields("2024-09-05T15:36:38.757788067Z", "2419", "19.098s"),
			},
			{
				Timestamp:          now,
				Line:               "ts=2024-09-05T15:36:38.698375619Z caller=grpc_logging.go:66 tenant=29 level=info method=/cortex.Ingester/Push duration=5.471s msg=gRPC",
				StructuredMetadata: infoDetectdFiledMetadata,
				Parsed:             parsedRulerFields("2024-09-05T15:36:38.698375619Z", "29", "5.471s"),
			},
			{
				Timestamp:          now,
				Line:               "ts=2024-09-05T15:36:38.629424175Z caller=grpc_logging.go:66 tenant=2919 level=info method=/cortex.Ingester/Push duration=29.234s msg=gRPC",
				StructuredMetadata: infoDetectdFiledMetadata,
				Parsed:             parsedRulerFields("2024-09-05T15:36:38.629424175Z", "2919", "29.234s"),
			},
		}

		rulerLbls := `{cluster="us-east-1", namespace="mimir-dev", pod="mimir-ruler-nfb37", service_name="mimir-ruler"}`
		rulerMetric, err := parser.ParseMetric(rulerLbls)
		require.NoError(t, err)

		rulerStream := push.Stream{
			Labels:  rulerLbls,
			Entries: rulerLines,
			Hash:    rulerMetric.Hash(),
		}

		debugDetectedFieldMetadata := []push.LabelAdapter{
			{
				Name:  "detected_level",
				Value: "debug",
			},
		}

		parsedNginxFields := func(host, userIdentifier, datetime, method, request, protocol, status, bytes, referer string) []push.LabelAdapter {
			return []push.LabelAdapter{
				{
					Name:  "host",
					Value: host,
				},
				{
					Name:  "user_identifier",
					Value: userIdentifier,
				},
				{
					Name:  "datetime",
					Value: datetime,
				},
				{
					Name:  "method",
					Value: method,
				},
				{
					Name:  "request",
					Value: request,
				},
				{
					Name:  "protocol",
					Value: protocol,
				},
				{
					Name:  "status",
					Value: status,
				},
				{
					Name:  "bytes",
					Value: bytes,
				},
				{
					Name:  "referer",
					Value: referer,
				},
			}
		}

		nginxJSONLines := []push.Entry{
			{
				Timestamp:          now,
				Line:               `{"host":"100.117.38.203", "user-identifier":"nader3722", "datetime":"05/Sep/2024:16:13:56 +0000", "method": "PATCH", "request": "/api/loki/v1/push", "protocol":"HTTP/2.0", "status":200, "bytes":9664, "referer": "https://www.seniorbleeding-edge.net/exploit/robust/whiteboard"}`,
				StructuredMetadata: debugDetectedFieldMetadata,
				Parsed:             parsedNginxFields("100.117.38.203", "nadre3722", "05/Sep/2024:16:13:56 +0000", "PATCH", "/api/loki/v1/push", "HTTP/2.0", "200", "9664", "https://www.seniorbleeding-edge.net/exploit/robust/whiteboard"),
			},
			{
				Timestamp:          now,
				Line:               `{"host":"66.134.9.30", "user-identifier":"-", "datetime":"05/Sep/2024:16:13:55 +0000", "method": "DELETE", "request": "/api/mimir/v1/push", "protocol":"HTTP/1.1", "status":200, "bytes":18688, "referer": "https://www.districtiterate.biz/synergistic/next-generation/extend"}`,
				StructuredMetadata: debugDetectedFieldMetadata,
				Parsed:             parsedNginxFields("66.134.9.30", "-", "05/Sep/2024:16:13:55 +0000", "DELETE", "/api/mimir/v1/push", "HTTP/1.1", "200", "18688", "https://www.districtiterate.biz/synergistic/next-generation/extend"),
			},
			{
				Timestamp:          now,
				Line:               `{"host":"66.134.9.30", "user-identifier":"-", "datetime":"05/Sep/2024:16:13:55 +0000", "method": "GET", "request": "/api/loki/v1/label/names", "protocol":"HTTP/1.1", "status":200, "bytes":9314, "referer": "https://www.dynamicimplement.info/enterprise/distributed/incentivize/strategic"}`,
				StructuredMetadata: debugDetectedFieldMetadata,
				Parsed:             parsedNginxFields("66.134.9.30", "-", "05/Sep/2024:16:13:55 +0000", "GET", "/api/loki/v1/label/names", "HTTP/1.1", "200", "9314", "https://www.dynamicimplement.info/enterprise/distributed/incentivize/strategic"),
			},
		}

		nginxLbls := `{ cluster="eu-west-1", level="debug", namespace="gateway", pod="nginx-json-oghco", service_name="nginx-json" }`
		nginxMetric, err := parser.ParseMetric(nginxLbls)
		require.NoError(t, err)

		nginxStream := push.Stream{
			Labels:  nginxLbls,
			Entries: nginxJSONLines,
			Hash:    nginxMetric.Hash(),
		}

		t.Run("detect logfmt fields", func(t *testing.T) {
			df := parseDetectedFields(uint32(15), logqlmodel.Streams([]push.Stream{rulerStream}))
			for _, expected := range []string{"ts", "caller", "tenant", "level", "method", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "logfmt", parsers[0])
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("detect json fields", func(t *testing.T) {
			df := parseDetectedFields(uint32(15), logqlmodel.Streams([]push.Stream{nginxStream}))
			for _, expected := range []string{"host", "user_identifier", "datetime", "method", "request", "protocol", "status", "bytes", "referer"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "json", parsers[0])
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("detect mixed fields", func(t *testing.T) {
			df := parseDetectedFields(uint32(20), logqlmodel.Streams([]push.Stream{rulerStream, nginxStream}))

			for _, expected := range []string{"ts", "caller", "tenant", "level", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1, "expected only logfmt parser for %s", expected)
				require.Equal(t, "logfmt", parsers[0], "expected only logfmt parser for %s", expected)
			}

			for _, expected := range []string{"host", "user_identifier", "datetime", "request", "protocol", "status", "bytes", "referer"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1, "expected only json parser for %s", expected)
				require.Equal(t, "json", parsers[0], "expected only json parser for %s", expected)
			}

			// multiple parsers for fields that exist in both streams
			for _, expected := range []string{"method"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 2, "expected logfmt and json parser for %s", expected)
				require.Contains(t, parsers, "logfmt", "expected logfmt parser for %s", expected)
				require.Contains(t, parsers, "json", "expected json parser for %s", expected)
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("correctly applies _extracted for a single stream", func(t *testing.T) {
			rulerLbls := `{cluster="us-east-1", namespace="mimir-dev", pod="mimir-ruler-nfb37", service_name="mimir-ruler", tenant="42", caller="inside-the-house"}`
			rulerMetric, err := parser.ParseMetric(rulerLbls)
			require.NoError(t, err)

			rulerStream := push.Stream{
				Labels:  rulerLbls,
				Entries: rulerLines,
				Hash:    rulerMetric.Hash(),
			}

			df := parseDetectedFields(uint32(15), logqlmodel.Streams([]push.Stream{rulerStream}))
			for _, expected := range []string{"ts", "caller_extracted", "tenant_extracted", "level", "method", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "logfmt", parsers[0])
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})

		t.Run("correctly applies _extracted for multiple streams", func(t *testing.T) {
			rulerLbls := `{cluster="us-east-1", namespace="mimir-dev", pod="mimir-ruler-nfb37", service_name="mimir-ruler", tenant="42", caller="inside-the-house"}`
			rulerMetric, err := parser.ParseMetric(rulerLbls)
			require.NoError(t, err)

			rulerStream := push.Stream{
				Labels:  rulerLbls,
				Entries: rulerLines,
				Hash:    rulerMetric.Hash(),
			}

			nginxLbls := `{ cluster="eu-west-1", level="debug", namespace="gateway", pod="nginx-json-oghco", service_name="nginx-json", host="localhost"}`
			nginxMetric, err := parser.ParseMetric(nginxLbls)
			require.NoError(t, err)

			nginxStream := push.Stream{
				Labels:  nginxLbls,
				Entries: nginxJSONLines,
				Hash:    nginxMetric.Hash(),
			}

			df := parseDetectedFields(uint32(20), logqlmodel.Streams([]push.Stream{rulerStream, nginxStream}))
			for _, expected := range []string{"ts", "caller_extracted", "tenant_extracted", "level", "duration", "msg"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1)
				require.Equal(t, "logfmt", parsers[0])
			}

			for _, expected := range []string{"host_extracted", "user_identifier", "datetime", "request", "protocol", "status", "bytes", "referer"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 1, "expected only json parser for %s", expected)
				require.Equal(t, "json", parsers[0], "expected only json parser for %s", expected)
			}

			// multiple parsers for fields that exist in both streams
			for _, expected := range []string{"method"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 2, "expected logfmt and json parser for %s", expected)
				require.Contains(t, parsers, "logfmt", "expected logfmt parser for %s", expected)
				require.Contains(t, parsers, "json", "expected json parser for %s", expected)
			}

			// no parsers for structed metadata
			for _, expected := range []string{"detected_level"} {
				require.Contains(t, df, expected)
				parsers := df[expected].parsers

				require.Len(t, parsers, 0)
			}
		})
	})

	t.Run("handles level in all the places", func(t *testing.T) {
		rulerLbls := `{cluster="us-east-1", namespace="mimir-dev", pod="mimir-ruler-nfb37", service_name="mimir-ruler", tenant="42", caller="inside-the-house", level="debug"}`
		rulerMetric, err := parser.ParseMetric(rulerLbls)
		require.NoError(t, err)

		rulerStream := push.Stream{
			Labels: rulerLbls,
			Entries: []push.Entry{
				{
					Timestamp: now,
					Line:      "ts=2024-09-05T15:36:38.757788067Z caller=grpc_logging.go:66 tenant=2419 level=info method=/cortex.Ingester/Push duration=19.098s msg=gRPC",
					StructuredMetadata: []push.LabelAdapter{
						{
							Name:  "detected_level",
							Value: "debug",
						},
					},
					Parsed: []push.LabelAdapter{
						{
							Name:  "level",
							Value: "info",
						},
					},
				},
			},
			Hash: rulerMetric.Hash(),
		}

		df := parseDetectedFields(uint32(20), logqlmodel.Streams([]push.Stream{rulerStream, rulerStream}))

		detectedLevelField := df["detected_level"]
		require.Len(t, detectedLevelField.parsers, 0)
		require.Equal(t, uint64(1), detectedLevelField.sketch.Estimate())

		levelField := df["level_extracted"]
		require.Len(t, levelField.parsers, 1)
		require.Contains(t, levelField.parsers, "logfmt")
		require.Equal(t, uint64(1), levelField.sketch.Estimate())
	})
}
