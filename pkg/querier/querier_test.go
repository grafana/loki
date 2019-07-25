package querier

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

const (
	// Custom query timeout used in tests
	queryTimeout = 12 * time.Second
)

func TestQuerier_Query_QueryTimeoutConfigFlag(t *testing.T) {
	request := logproto.QueryRequest{
		Query:     "{type=\"test\"}",
		Limit:     10,
		Start:     time.Now().Add(-1 * time.Minute),
		End:       time.Now(),
		Direction: logproto.FORWARD,
		Regex:     "",
	}

	store := newStoreMock()
	store.On("LazyQuery", mock.Anything, mock.Anything).Return(mockStreamIterator(), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]*logproto.Stream{mockStream()}), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, &request, mock.Anything).Return(queryClient, nil)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = q.Query(ctx, &request)
	require.NoError(t, err)

	calls := ingesterClient.GetMockedCallsByMethod("Query")
	assert.Equal(t, 1, len(calls))
	deadline, ok := calls[0].Arguments.Get(0).(context.Context).Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, deadline, time.Now().Add(queryTimeout), 1*time.Second)

	calls = store.GetMockedCallsByMethod("LazyQuery")
	assert.Equal(t, 1, len(calls))
	deadline, ok = calls[0].Arguments.Get(0).(context.Context).Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, deadline, time.Now().Add(queryTimeout), 1*time.Second)

	store.AssertExpectations(t)
}

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
	store.On("LabelValuesForMetricName", mock.Anything, model.TimeFromUnixNano(startTime.UnixNano()), model.TimeFromUnixNano(endTime.UnixNano()), "logs", "test").Return([]string{"foo", "bar"}, nil)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store)
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
		Regex:    "",
		DelayFor: 0,
		Limit:    10,
		Start:    time.Now(),
	}

	store := newStoreMock()
	store.On("LazyQuery", mock.Anything, mock.Anything).Return(mockStreamIterator(), nil)

	queryClient := newQueryClientMock()
	queryClient.On("Recv").Return(mockQueryResponse([]*logproto.Stream{mockStream()}), nil)

	tailClient := newTailClientMock()
	tailClient.On("Recv").Return(mockTailResponse(mockStream()), nil)

	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)
	ingesterClient.On("Tail", mock.Anything, &request, mock.Anything).Return(tailClient, nil)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store)
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

func mockQuerierConfig() Config {
	return Config{
		TailMaxDuration: 1 * time.Minute,
		QueryTimeout:    queryTimeout,
	}
}

func mockStreamIterator() iter.EntryIterator {
	return iter.NewStreamIterator(mockStream())
}

func mockStream() *logproto.Stream {
	entries := []logproto.Entry{
		{Timestamp: time.Now(), Line: "line 1"},
		{Timestamp: time.Now(), Line: "line 2"},
	}

	labels := "{type=\"test\"}"

	return &logproto.Stream{
		Entries: entries,
		Labels:  labels,
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

func mockTailResponse(stream *logproto.Stream) *logproto.TailResponse {
	return &logproto.TailResponse{
		Stream:         stream,
		DroppedStreams: []*logproto.DroppedStream{},
	}
}
