package querier

import (
	"context"
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

	limits, err := validation.NewOverrides(defaultLimitsTestConfig())
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
	store.On("LazyQuery", mock.Anything, mock.Anything).Return(mockStreamIterator(1, 2), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]*logproto.Stream{mockStream(1, 2)}), nil)

	tailClient := newTailClientMock()
	tailClient.On("Recv").Return(mockTailResponse(mockStream(1, 2)), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)
	ingesterClient.On("Tail", mock.Anything, &request, mock.Anything).Return(tailClient, nil)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig())
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

	calls = store.GetMockedCallsByMethod("LazyQuery")
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

			limits, err := validation.NewOverrides(defaultLimitsTestConfig())
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

func mockQueryResponse(streams []*logproto.Stream) *logproto.QueryResponse {
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
	store.On("LazyQuery", mock.Anything, mock.Anything).Return(mockStreamIterator(1, 2), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]*logproto.Stream{mockStream(1, 2)}), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, &request, mock.Anything).Return(queryClient, nil)

	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.MaxStreamsMatchersPerQuery = 1
	defaultLimits.MaxQueryLength = 2 * time.Minute

	limits, err := validation.NewOverrides(defaultLimits)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")

	_, err = q.Select(ctx, logql.SelectParams{QueryRequest: &request})
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "max streams matchers per query exceeded, matchers-count > limit (2 > 1)"), err)

	request.Selector = "{type=\"test\"}"
	_, err = q.Select(ctx, logql.SelectParams{QueryRequest: &request})
	require.NoError(t, err)

	request.Start = request.End.Add(-3 * time.Minute)
	_, err = q.Select(ctx, logql.SelectParams{QueryRequest: &request})
	require.Equal(t, httpgrpc.Errorf(http.StatusBadRequest, "invalid query, length > limit (3m0s > 2m0s)"), err)
}
