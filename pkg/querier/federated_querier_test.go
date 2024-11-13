package querier

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

func Test_FederatedQuerier(t *testing.T) {
	fq := buildFederatedQuerier()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()

	for _, tc := range []struct {
		name   string
		mockFn func(mockQuerier1, mockQuerier2 *MockIngesterQuerier)
		fn     func(params ...interface{}) (interface{}, error)
		expect func(t *testing.T, resp interface{})
	}{
		{
			name: "SelectLogs",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("SelectLogs", mock.Anything, mock.Anything).Return([]iter.EntryIterator{
					iter.NewStreamIterator(logproto.Stream{
						Labels:  `{type="test"}`,
						Entries: []logproto.Entry{{Line: "line 1"}},
					}),
				}, nil)
				mockQuerier2.On("SelectLogs", mock.Anything, mock.Anything).Return([]iter.EntryIterator{
					iter.NewStreamIterator(logproto.Stream{
						Labels:  `{type="test"}`,
						Entries: []logproto.Entry{{Line: "line 2"}},
					}),
				}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.SelectLogs(ctx, logql.SelectLogParams{})
			},
			expect: func(t *testing.T, resp interface{}) {
				itr := resp.([]iter.EntryIterator)

				require.Len(t, itr, 2)
				require.Equal(t, `{type="test"}`, itr[0].Labels())
				require.Equal(t, `{type="test"}`, itr[1].Labels())
				lines := make([]string, 0, 2)
				itr[0].Next()
				lines = append(lines, itr[0].At().Line)
				itr[1].Next()
				lines = append(lines, itr[1].At().Line)
				sort.Strings(lines)
				require.Equal(t, "line 1", lines[0])
				require.Equal(t, "line 2", lines[1])
			},
		},
		{
			name: "SelectSample",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("SelectSample", mock.Anything, mock.Anything).Return([]iter.SampleIterator{
					iter.NewSortSampleIterator([]iter.SampleIterator{iter.NewSeriesIterator(logproto.Series{
						Labels:  `{app="foo"}`,
						Samples: []logproto.Sample{{Value: 1., Timestamp: 1}},
					})})}, nil)
				mockQuerier2.On("SelectSample", mock.Anything, mock.Anything).Return([]iter.SampleIterator{
					iter.NewSortSampleIterator([]iter.SampleIterator{iter.NewSeriesIterator(logproto.Series{
						Labels:  `{app="foo"}`,
						Samples: []logproto.Sample{{Value: 2., Timestamp: 1}},
					})})}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.SelectSample(ctx, logql.SelectSampleParams{})
			},
			expect: func(t *testing.T, resp interface{}) {
				itr := resp.([]iter.SampleIterator)
				require.Len(t, itr, 2)
				require.Equal(t, `{app="foo"}`, itr[0].Labels())
				require.Equal(t, `{app="foo"}`, itr[1].Labels())
				samples := make([]float64, 0, 2)
				itr[0].Next()
				samples = append(samples, itr[0].At().Value)
				itr[1].Next()
				samples = append(samples, itr[1].At().Value)
				sort.Float64s(samples)
				require.Equal(t, 1., samples[0])
				require.Equal(t, 2., samples[1])
			},
		},
		{
			name: "Label",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("Label", mock.Anything, mock.Anything).Return([][]string{{"test1"}}, nil)
				mockQuerier2.On("Label", mock.Anything, mock.Anything).Return([][]string{{"test2"}}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.Label(ctx, &logproto.LabelRequest{})
			},
			expect: func(t *testing.T, resp interface{}) {
				lbs := resp.([][]string)
				ls := make([]string, 0, 2)
				ls = append(ls, lbs[0][0], lbs[1][0])
				sort.Strings(ls)

				require.Len(t, lbs, 2)
				require.Equal(t, "test1", ls[0])
				require.Equal(t, "test2", ls[1])
			},
		},
		{
			name: "Tail",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				tailClient := newTailClientMock()
				mockQuerier1.On("Tail", mock.Anything, mock.Anything, mock.Anything).Return(
					map[string]logproto.Querier_TailClient{"client1": tailClient}, nil)
				mockQuerier2.On("Tail", mock.Anything, mock.Anything, mock.Anything).Return(
					map[string]logproto.Querier_TailClient{"client2": tailClient}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.Tail(ctx, &logproto.TailRequest{})
			},
			expect: func(t *testing.T, resp interface{}) {
				tailers := resp.(map[string]logproto.Querier_TailClient)
				require.Len(t, tailers, 2)
			},
		},
		{
			name: "Series",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("Series", mock.Anything, mock.Anything).Return([][]logproto.SeriesIdentifier{
					{{Labels: logproto.MustNewSeriesEntries("a", "1", "b", "1")}},
				}, nil)
				mockQuerier2.On("Series", mock.Anything, mock.Anything).Return([][]logproto.SeriesIdentifier{
					{{Labels: logproto.MustNewSeriesEntries("a", "2", "b", "2")}},
				}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.Series(ctx, &logproto.SeriesRequest{})
			},
			expect: func(t *testing.T, resp interface{}) {
				series := resp.([][]logproto.SeriesIdentifier)
				require.Len(t, series, 2)
				res := make([]string, 0, 2)
				res = append(res, series[0][0].String(), series[1][0].String())
				sort.Strings(res)
				require.Equal(t, `&SeriesIdentifier{Labels:[]SeriesIdentifier_LabelsEntry{{a 1},{b 1},},}`, res[0])
				require.Equal(t, `&SeriesIdentifier{Labels:[]SeriesIdentifier_LabelsEntry{{a 2},{b 2},},}`, res[1])
			},
		},
		{
			name: "TailersCount",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("TailersCount", mock.Anything, mock.Anything, mock.Anything).Return([]uint32{1}, nil)
				mockQuerier2.On("TailersCount", mock.Anything, mock.Anything, mock.Anything).Return([]uint32{1}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.TailersCount(ctx)
			},
			expect: func(t *testing.T, resp interface{}) {
				counts := resp.([]uint32)
				require.Len(t, counts, 2)
			},
		},
		{
			name: "GetChunkIDs",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier2.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{"chunkId2"}, nil)
				mockQuerier1.On("GetChunkIDs", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{"chunkId1"}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.GetChunkIDs(ctx, model.Time(0), model.Time(0), &labels.Matcher{})
			},
			expect: func(t *testing.T, resp interface{}) {
				chunkIDs := resp.([]string)
				require.Len(t, chunkIDs, 2)
				ids := make([]string, 0, 2)
				ids = append(ids, chunkIDs[0], chunkIDs[1])
				sort.Strings(ids)
				require.Equal(t, "chunkId1", ids[0])
				require.Equal(t, "chunkId2", ids[1])
			},
		},
		{
			name: "Volume",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("Volume", mock.Anything, "test-user", mock.Anything, mock.Anything, int32(100), []string{"foo"}, "bar", mock.Anything).Return(
					&logproto.VolumeResponse{Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 10},
					}, Limit: 10}, nil)
				mockQuerier2.On("Volume", mock.Anything, "test-user", mock.Anything, mock.Anything, int32(100), []string{"foo"}, "bar", mock.Anything).Return(
					&logproto.VolumeResponse{Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: 20},
					}, Limit: 10}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.Volume(ctx, "test-user", model.Time(0), model.Time(0), 100, []string{"foo"}, "bar", &labels.Matcher{})
			},
			expect: func(t *testing.T, resp interface{}) {
				volume := resp.(*logproto.VolumeResponse)
				require.Len(t, volume.Volumes, 1)
				require.Equal(t, []logproto.Volume{{Name: `{foo="bar"}`, Volume: 30}}, volume.Volumes)
			},
		},
		{
			name: "DetectedLabel",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("DetectedLabel", mock.Anything, mock.Anything, mock.Anything).Return(&logproto.LabelToValuesResponse{
					Labels: map[string]*logproto.UniqueLabelValues{"pod": {Values: []string{"pod1"}}},
				}, nil)
				mockQuerier2.On("DetectedLabel", mock.Anything, mock.Anything, mock.Anything).Return(&logproto.LabelToValuesResponse{
					Labels: map[string]*logproto.UniqueLabelValues{"host": {Values: []string{"host1"}}},
				}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.DetectedLabel(ctx, &logproto.DetectedLabelsRequest{})
			},
			expect: func(t *testing.T, resp interface{}) {
				r := resp.(*logproto.LabelToValuesResponse)
				require.Equal(t, map[string]*logproto.UniqueLabelValues{"host": {Values: []string{"host1"}}, "pod": {Values: []string{"pod1"}}}, r.Labels)
			},
		},
		{
			name: "Stats",
			mockFn: func(mockQuerier1, mockQuerier2 *MockIngesterQuerier) {
				mockQuerier1.On("Stats", mock.Anything, "test-user", mock.Anything, mock.Anything, mock.Anything).Return(
					&stats.Stats{Chunks: 10}, nil)
				mockQuerier2.On("Stats", mock.Anything, "test-user", mock.Anything, mock.Anything, mock.Anything).Return(
					&stats.Stats{Chunks: 20}, nil)
			},
			fn: func(params ...interface{}) (interface{}, error) {
				return fq.Stats(ctx, "test-user", model.Time(0), model.Time(0), &labels.Matcher{})
			},
			expect: func(t *testing.T, resp interface{}) {
				s := resp.(*stats.Stats)
				require.Equal(t, 30, int(s.Chunks))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.mockFn(fq.ingesterQueriers[0].(*MockIngesterQuerier), fq.ingesterQueriers[1].(*MockIngesterQuerier))
			resp, err := tc.fn()
			require.NoError(t, err)
			tc.expect(t, resp)
		})
	}
}

func buildFederatedQuerier() *FederatedQuerier {
	mockQuerier1 := new(MockIngesterQuerier)
	mockQuerier2 := new(MockIngesterQuerier)

	// Initialize FederatedQuerier with mock ingester queriers
	fq := &FederatedQuerier{
		ingesterQueriers: []storage.IIngesterQuerier{mockQuerier1, mockQuerier2},
	}
	return fq
}

type MockIngesterQuerier struct {
	mock.Mock
}

func (m *MockIngesterQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	args := m.Called(ctx, params)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).([]iter.EntryIterator), args.Error(1)
}

func (m *MockIngesterQuerier) SelectSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	args := m.Called(ctx, params)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).([]iter.SampleIterator), args.Error(1)
}

func (m *MockIngesterQuerier) Label(ctx context.Context, req *logproto.LabelRequest) ([][]string, error) {
	args := m.Called(ctx, req)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).([][]string), args.Error(1)
}

func (m *MockIngesterQuerier) Tail(ctx context.Context, req *logproto.TailRequest) (map[string]logproto.Querier_TailClient, error) {
	args := m.Called(ctx, req)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).(map[string]logproto.Querier_TailClient), args.Error(1)
}

func (m *MockIngesterQuerier) TailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	args := m.Called(ctx, req, connectedIngestersAddr)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).(map[string]logproto.Querier_TailClient), args.Error(1)
}

func (m *MockIngesterQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) ([][]logproto.SeriesIdentifier, error) {
	args := m.Called(ctx, req)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).([][]logproto.SeriesIdentifier), args.Error(1)
}

func (m *MockIngesterQuerier) TailersCount(ctx context.Context) ([]uint32, error) {
	args := m.Called(ctx)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).([]uint32), args.Error(1)
}

func (m *MockIngesterQuerier) GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	args := m.Called(ctx, from, through, matchers)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockIngesterQuerier) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	args := m.Called(ctx, userID, from, through, limit, targetLabels, aggregateBy, matchers)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).(*logproto.VolumeResponse), args.Error(1)
}

func (m *MockIngesterQuerier) DetectedLabel(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.LabelToValuesResponse, error) {
	args := m.Called(ctx, req)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).(*logproto.LabelToValuesResponse), args.Error(1)
}

func (m *MockIngesterQuerier) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	args := m.Called(ctx, userID, from, through, matchers)
	time.Sleep(time.Millisecond * 100)
	return args.Get(0).(*stats.Stats), args.Error(1)
}
