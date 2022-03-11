package querier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/validation"
)

func TestMultiTenantQuerier_SelectLogs(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		orgID     string
		expLabels []string
		expLines  []string
	}{
		{
			"two tenants",
			"1|2",
			[]string{
				`{__tenant_id__="1", type="test"}`,
				`{__tenant_id__="1", type="test"}`,
				`{__tenant_id__="2", type="test"}`,
				`{__tenant_id__="2", type="test"}`,
			},
			[]string{"line 1", "line 2", "line 1", "line 2"},
		},
		{
			"one tenant",
			"1",
			[]string{
				`{type="test"}`,
				`{type="test"}`,
			},
			[]string{"line 1", "line 2"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			querier := newQuerierMock()
			querier.On("SelectLogs", mock.Anything, mock.Anything).Return(func() iter.EntryIterator { return mockStreamIterator(1, 2) }, nil)

			multiTenantQuerier := NewMultiTenantQuerier(querier, log.NewNopLogger())

			ctx := user.InjectOrgID(context.Background(), tc.orgID)
			params := logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
				Selector:  `{type="test"}`,
				Direction: logproto.BACKWARD,
				Limit:     0,
				Shards:    nil,
				Start:     time.Unix(0, 1),
				End:       time.Unix(0, time.Now().UnixNano()),
			}}
			iter, err := multiTenantQuerier.SelectLogs(ctx, params)
			require.NoError(t, err)

			entriesCount := 0
			for iter.Next() {
				require.Equal(t, tc.expLabels[entriesCount], iter.Labels())
				require.Equal(t, tc.expLines[entriesCount], iter.Entry().Line)
				entriesCount++
			}
			require.Equalf(t, len(tc.expLabels), entriesCount, "Expected %d entries but got %d", len(tc.expLabels), entriesCount)
		})
	}
}

func TestMultiTenantQuerier_SelectSamples(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		orgID     string
		expLabels []string
	}{
		{
			"two tenants",
			"1|2",
			[]string{
				`{__tenant_id__="1", app="foo"}`,
				`{__tenant_id__="2", app="foo"}`,
				`{__tenant_id__="2", app="bar"}`,
				`{__tenant_id__="1", app="bar"}`,
				`{__tenant_id__="1", app="foo"}`,
				`{__tenant_id__="2", app="foo"}`,
				`{__tenant_id__="2", app="bar"}`,
				`{__tenant_id__="1", app="bar"}`,
			},
		},
		{
			"one tenant",
			"1",
			[]string{
				`{app="foo"}`,
				`{app="bar"}`,
				`{app="foo"}`,
				`{app="bar"}`,
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			querier := newQuerierMock()
			querier.On("SelectSamples", mock.Anything, mock.Anything).Return(func() iter.SampleIterator { return newSampleIterator() }, nil)

			multiTenantQuerier := NewMultiTenantQuerier(querier, log.NewNopLogger())

			ctx := user.InjectOrgID(context.Background(), tc.orgID)
			params := logql.SelectSampleParams{}
			iter, err := multiTenantQuerier.SelectSamples(ctx, params)
			require.NoError(t, err)

			entriesCount := 0
			for iter.Next() {
				require.Equalf(t, tc.expLabels[entriesCount], iter.Labels(), "Entry %d", entriesCount)
				entriesCount++
			}
			require.Equalf(t, len(tc.expLabels), entriesCount, "Expected %d entries but got %d", len(tc.expLabels), entriesCount)
		})
	}
}

var samples = []logproto.Sample{
	{Timestamp: time.Unix(2, 0).UnixNano(), Hash: 1, Value: 1.},
	{Timestamp: time.Unix(5, 0).UnixNano(), Hash: 2, Value: 1.},
}

var (
	labelFoo, _ = logql.ParseLabels("{app=\"foo\"}")
	labelBar, _ = logql.ParseLabels("{app=\"bar\"}")
)

func newSampleIterator() iter.SampleIterator {
	return iter.NewSortSampleIterator([]iter.SampleIterator{
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelFoo.String(),
			Samples:    samples,
			StreamHash: labelFoo.Hash(),
		}),
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelBar.String(),
			Samples:    samples,
			StreamHash: labelBar.Hash(),
		}),
	})
}

func TestMultiTenantQuerier_Series(t *testing.T) {
	mockSeriesRequest := func(groups []string) *logproto.SeriesRequest {
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
		run   func(*testing.T, *MultiTenantQuerier, *logproto.SeriesRequest)
	}{
		{
			desc: "ingester error",
			req:  mockSeriesRequest([]string{`{a="1"}`}),
			setup: func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(nil, errors.New("tst-err"))
				store.On("GetSeries", mock.Anything, mock.Anything).Return(nil, nil)
			},
			run: func(t *testing.T, q *MultiTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "1|2")
				_, err := q.Series(ctx, req)
				require.Error(t, err)
			},
		},
		{
			desc: "store error",
			req:  mockSeriesRequest([]string{`{a="1"}`}),
			setup: func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"a": "1"},
				}), nil)
				store.On("GetSeries", mock.Anything, mock.Anything).Return(nil, context.DeadlineExceeded)
			},
			run: func(t *testing.T, q *MultiTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "1|2")
				_, err := q.Series(ctx, req)
				require.Error(t, err)
			},
		},
		{
			desc: "no matches",
			req:  mockSeriesRequest([]string{`{a="1"}`}),
			setup: func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse(nil), nil)
				store.On("GetSeries", mock.Anything, mock.Anything).Return(nil, nil)
			},
			run: func(t *testing.T, q *MultiTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "1|2")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.Equal(t, &logproto.SeriesResponse{Series: make([]logproto.SeriesIdentifier, 0)}, resp)
			},
		},
		{
			desc: "returns series",
			req:  mockSeriesRequest([]string{`{a="1"}`}),
			setup: func(store *storeMock, querier *queryClientMock, ingester *querierClientMock, limits validation.Limits, req *logproto.SeriesRequest) {
				ingester.On("Series", mock.Anything, req, mock.Anything).Return(mockSeriesResponse([]map[string]string{
					{"__tenant_id__": "1", "a": "1", "b": "2"},
					{"__tenant_id__": "1", "a": "1", "b": "3"},
					{"__tenant_id__": "2", "a": "1", "b": "2"},
					{"__tenant_id__": "2", "a": "1", "b": "3"},
				}), nil)

				store.On("GetSeries", mock.Anything, mock.Anything).Return([]logproto.SeriesIdentifier{
					{Labels: map[string]string{"__tenant_id__": "1", "a": "1", "b": "4"}},
					{Labels: map[string]string{"__tenant_id__": "1", "a": "1", "b": "5"}},
					{Labels: map[string]string{"__tenant_id__": "2", "a": "1", "b": "4"}},
					{Labels: map[string]string{"__tenant_id__": "2", "a": "1", "b": "5"}},
				}, nil)
			},
			run: func(t *testing.T, q *MultiTenantQuerier, req *logproto.SeriesRequest) {
				ctx := user.InjectOrgID(context.Background(), "1|2")
				resp, err := q.Series(ctx, req)
				require.Nil(t, err)
				require.ElementsMatch(t, []logproto.SeriesIdentifier{
					{Labels: map[string]string{"__tenant_id__": "1", "a": "1", "b": "2"}},
					{Labels: map[string]string{"__tenant_id__": "1", "a": "1", "b": "3"}},
					{Labels: map[string]string{"__tenant_id__": "1", "a": "1", "b": "4"}},
					{Labels: map[string]string{"__tenant_id__": "1", "a": "1", "b": "5"}},
					{Labels: map[string]string{"__tenant_id__": "2", "a": "1", "b": "2"}},
					{Labels: map[string]string{"__tenant_id__": "2", "a": "1", "b": "3"}},
					{Labels: map[string]string{"__tenant_id__": "2", "a": "1", "b": "4"}},
					{Labels: map[string]string{"__tenant_id__": "2", "a": "1", "b": "5"}},
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

			multiTenantQuerier := NewMultiTenantQuerier(q, log.NewNopLogger())

			tc.run(t, multiTenantQuerier, tc.req)
		})
	}
}

// TODO(jordanrushing): Expand this test to include other scenarios
func TestMultiTenantQuerier_Label(t *testing.T) {
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
	store.On("LabelValuesForMetricName", mock.Anything, mock.Anything, model.TimeFromUnixNano(startTime.UnixNano()), model.TimeFromUnixNano(endTime.UnixNano()), "logs", "test").Return([]string{"test", "test"}, nil)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store, limits)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "1|2")

	multiTenantQuerier := NewMultiTenantQuerier(q, log.NewNopLogger())

	resp, labelErr := multiTenantQuerier.Label(ctx, &request)
	require.NoError(t, labelErr)
	require.ElementsMatch(t, []string{"test"}, resp.GetValues())
}
