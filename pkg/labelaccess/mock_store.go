package labelaccess

import (
	"context"
	"sync"
	"time"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/storage/types"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

func MustParseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{Time: model.TimeFromUnix(t.Unix())}
}

var defaultPeriodConfigs = []config.PeriodConfig{
	{
		From:      MustParseDayTime("1900-01-01"),
		IndexType: types.StorageTypeBigTable,
		Schema:    "v13",
	},
}

type mockStore struct {
	mtx    sync.Mutex
	chunks map[string][]chunk.Chunk
}

func (s *mockStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	userid, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	s.chunks[userid] = append(s.chunks[userid], chunks...)
	return nil
}

func (s *mockStore) SelectLogs(_ context.Context, _ logql.SelectLogParams) (iter.EntryIterator, error) {
	return nil, nil
}

func (s *mockStore) SelectSamples(_ context.Context, _ logql.SelectSampleParams) (iter.SampleIterator, error) {
	return nil, nil
}

func (s *mockStore) SelectSeries(_ context.Context, _ logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	return nil, nil
}

func (s *mockStore) GetSchemaConfigs() []config.PeriodConfig {
	return defaultPeriodConfigs
}

func (s *mockStore) SetChunkFilterer(_ chunk.RequestChunkFilterer) {
}

// chunk.Store methods
func (s *mockStore) PutOne(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return nil
}

func (s *mockStore) GetChunks(_ context.Context, _ string, _, _ model.Time, _ chunk.Predicate, _ *logproto.ChunkRefGroup) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	return nil, nil, nil
}

func (s *mockStore) GetChunkRefsWithSizingInfo(_ context.Context, _ string, _, _ model.Time, _ chunk.Predicate) ([]logproto.ChunkRefWithSizingInfo, error) {
	return nil, nil
}

func (s *mockStore) HasChunkSizingInfo(_, _ model.Time) bool {
	return false
}

func (s *mockStore) LabelValuesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ string, _ ...*labels.Matcher) ([]string, error) {
	return []string{"val1", "val2"}, nil
}

func (s *mockStore) LabelNamesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string) ([]string, error) {
	return nil, nil
}

func (s *mockStore) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return nil
}

func (s *mockStore) Stats(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return &stats.Stats{
		Streams: 2,
		Chunks:  5,
		Bytes:   25,
		Entries: 100,
	}, nil
}

func (s *mockStore) GetShards(_ context.Context, _ string, _, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return nil, nil
}

func (s *mockStore) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return nil, false
}

func (s *mockStore) Volume(_ context.Context, _ string, _, _ model.Time, limit int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return &logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 38},
		},
		Limit: limit,
	}, nil
}

func (s *mockStore) Stop() {}
