package stores

import (
	"context"
	"sort"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	chunkclient "github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	indexstats "github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type MockChunkStore struct {
	schemas config.SchemaConfig
	chunks  []chunk.Chunk
	client  *mockChunkStoreClient
	f       chunk.RequestChunkFilterer
}

// mockChunkStore cannot implement both chunk.Store and chunk.Client,
// since there is a conflict in signature for DeleteChunk method.
var (
	_ Store              = &MockChunkStore{}
	_ chunkclient.Client = &mockChunkStoreClient{}
)

func NewMockChunkStore(chunkFormat byte, headfmt chunkenc.HeadBlockFmt, streams []*logproto.Stream) *MockChunkStore {
	chunks := make([]chunk.Chunk, 0, len(streams))
	for _, s := range streams {
		chunks = append(chunks, NewTestChunk(chunkFormat, headfmt, *s))
	}
	return NewMockChunkStoreWithChunks(chunks)
}

func NewMockChunkStoreWithChunks(chunks []chunk.Chunk) *MockChunkStore {
	schemas := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{From: config.DayTime{Time: 0}, Schema: "v13"},
		},
	}
	return &MockChunkStore{
		schemas: schemas,
		chunks:  chunks,
		client: &mockChunkStoreClient{
			chunks:  chunks,
			schemas: schemas,
		},
	}
}

func (m *MockChunkStore) Put(_ context.Context, _ []chunk.Chunk) error { return nil }
func (m *MockChunkStore) PutOne(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return nil
}

func (m *MockChunkStore) GetSeries(ctx context.Context, _ string, _, _ model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	result := make([]labels.Labels, 0, len(m.chunks))
	unique := map[uint64]struct{}{}
Outer:
	for _, c := range m.chunks {
		if _, ok := unique[c.Fingerprint]; !ok {
			for _, m := range matchers {
				if !m.Matches(c.Metric.Get(m.Name)) {
					continue Outer
				}
			}
			l := labels.NewBuilder(c.Metric).Del(labels.MetricName).Labels()
			if m.f != nil {
				if m.f.ForRequest(ctx).ShouldFilter(l) {
					continue
				}
			}

			result = append(result, l)
			unique[c.Fingerprint] = struct{}{}
		}
	}
	sort.Slice(result, func(i, j int) bool { return labels.Compare(result[i], result[j]) < 0 })
	return result, nil
}

func (m *MockChunkStore) LabelValuesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string, _ string, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (m *MockChunkStore) LabelNamesForMetricName(_ context.Context, _ string, _, _ model.Time, _ string) ([]string, error) {
	return nil, nil
}

func (m *MockChunkStore) SetChunkFilterer(f chunk.RequestChunkFilterer) {
	m.f = f
}

func (m *MockChunkStore) DeleteChunk(_ context.Context, _, _ model.Time, _, _ string, _ labels.Labels, _ *model.Interval) error {
	return nil
}

func (m *MockChunkStore) DeleteSeriesIDs(_ context.Context, _, _ model.Time, _ string, _ labels.Labels) error {
	return nil
}
func (m *MockChunkStore) Stop() {}
func (m *MockChunkStore) Get(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) ([]chunk.Chunk, error) {
	return nil, nil
}

func (m *MockChunkStore) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return nil
}

func (m *MockChunkStore) GetChunks(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	periodCfg := m.schemas.Configs[0]
	refs := make([]chunk.Chunk, 0, len(m.chunks))
	// transform real chunks into ref chunks.
	for _, c := range m.chunks {
		r, err := chunk.ParseExternalKey("fake", periodCfg.ExternalKey(c.ChunkRef))
		if err != nil {
			panic(err)
		}
		refs = append(refs, r)
	}

	cache, err := cache.New(cache.Config{Prefix: "chunks"}, nil, util_log.Logger, stats.ChunkCache)
	if err != nil {
		panic(err)
	}

	f, err := fetcher.New(cache, nil, false, periodCfg, m.client, 10, 100, 0)
	if err != nil {
		panic(err)
	}
	return [][]chunk.Chunk{refs}, []*fetcher.Fetcher{f}, nil
}

func (m *MockChunkStore) Stats(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) (*indexstats.Stats, error) {
	return nil, nil
}

func (m *MockChunkStore) Volume(_ context.Context, _ string, _, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return nil, nil
}

type mockChunkStoreClient struct {
	schemas config.SchemaConfig
	chunks  []chunk.Chunk
}

func (m mockChunkStoreClient) Stop() {
	panic("implement me")
}

func (m mockChunkStoreClient) PutChunks(_ context.Context, _ []chunk.Chunk) error {
	return nil
}

func (m mockChunkStoreClient) GetChunks(_ context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	var res []chunk.Chunk
	for _, c := range chunks {
		for _, sc := range m.chunks {
			// only returns chunks requested using the external key
			if m.schemas.ExternalKey(c.ChunkRef) == m.schemas.ExternalKey(sc.ChunkRef) {
				res = append(res, sc)
			}
		}
	}
	return res, nil
}

func (m mockChunkStoreClient) DeleteChunk(_ context.Context, _, _ string) error {
	return nil
}

func (m mockChunkStoreClient) IsChunkNotFoundErr(_ error) bool {
	return false
}

func (m mockChunkStoreClient) IsRetryableErr(_ error) bool {
	return false
}

func NewTestChunk(chunkFormat byte, headBlockFmt chunkenc.HeadBlockFmt, stream logproto.Stream) chunk.Chunk {
	lbs, err := syntax.ParseLabels(stream.Labels)
	if err != nil {
		panic(err)
	}
	if !lbs.Has(labels.MetricName) {
		builder := labels.NewBuilder(lbs)
		builder.Set(labels.MetricName, "logs")
		lbs = builder.Labels()
	}
	from, through := util.RoundToMilliseconds(stream.Entries[0].Timestamp, stream.Entries[len(stream.Entries)-1].Timestamp)
	chk := chunkenc.NewMemChunk(chunkFormat, chunkenc.EncGZIP, headBlockFmt, 256*1024, 0)
	for _, e := range stream.Entries {
		_ = chk.Append(&e)
	}
	chk.Close()
	c := chunk.NewChunk("fake", client.Fingerprint(lbs), lbs, chunkenc.NewFacade(chk, 0, 0), from, through)
	// force the checksum creation
	if err := c.Encode(); err != nil {
		panic(err)
	}
	return c
}

func BuildTestStream(labels labels.Labels, fromTs, toTs time.Time) logproto.Stream {
	stream := logproto.Stream{
		Labels:  labels.String(),
		Hash:    labels.Hash(),
		Entries: []logproto.Entry{},
	}

	for from := fromTs; from.Before(toTs); from = from.Add(time.Second) {
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: from,
			Line:      from.String(),
		})
	}

	return stream
}
