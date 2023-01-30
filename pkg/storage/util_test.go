package storage

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	chunkclient "github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	index_stats "github.com/grafana/loki/pkg/storage/stores/index/stats"
	loki_util "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	fooLabelsWithName = labels.Labels{{Name: "foo", Value: "bar"}, {Name: "__name__", Value: "logs"}}
	fooLabels         = labels.Labels{{Name: "foo", Value: "bar"}}
)

var from = time.Unix(0, time.Millisecond.Nanoseconds())

func assertStream(t *testing.T, expected, actual []logproto.Stream) {
	if len(expected) != len(actual) {
		t.Fatalf("error stream length are different expected %d actual %d\n%s", len(expected), len(actual), spew.Sdump(expected, actual))
		return
	}
	sort.Slice(expected, func(i int, j int) bool { return expected[i].Labels < expected[j].Labels })
	sort.Slice(actual, func(i int, j int) bool { return actual[i].Labels < actual[j].Labels })
	for i := range expected {
		assert.Equal(t, expected[i].Labels, actual[i].Labels)
		if len(expected[i].Entries) != len(actual[i].Entries) {
			t.Fatalf("error entries length are different expected %d actual %d\n%s", len(expected[i].Entries), len(actual[i].Entries), spew.Sdump(expected[i].Entries, actual[i].Entries))

			return
		}
		for j := range expected[i].Entries {
			assert.Equal(t, expected[i].Entries[j].Timestamp.UnixNano(), actual[i].Entries[j].Timestamp.UnixNano())
			assert.Equal(t, expected[i].Entries[j].Line, actual[i].Entries[j].Line)
		}
	}
}

func assertSeries(t *testing.T, expected, actual []logproto.Series) {
	if len(expected) != len(actual) {
		t.Fatalf("error stream length are different expected %d actual %d\n%s", len(expected), len(actual), spew.Sdump(expected, actual))
		return
	}
	sort.Slice(expected, func(i int, j int) bool { return expected[i].Labels < expected[j].Labels })
	sort.Slice(actual, func(i int, j int) bool { return actual[i].Labels < actual[j].Labels })
	for i := range expected {
		assert.Equal(t, expected[i].Labels, actual[i].Labels)
		if len(expected[i].Samples) != len(actual[i].Samples) {
			t.Fatalf("error entries length are different expected %d actual%d\n%s", len(expected[i].Samples), len(actual[i].Samples), spew.Sdump(expected[i].Samples, actual[i].Samples))

			return
		}
		for j := range expected[i].Samples {
			assert.Equal(t, expected[i].Samples[j].Timestamp, actual[i].Samples[j].Timestamp)
			assert.Equal(t, expected[i].Samples[j].Value, actual[i].Samples[j].Value)
			assert.Equal(t, expected[i].Samples[j].Hash, actual[i].Samples[j].Hash)
		}
	}
}

func newLazyChunk(stream logproto.Stream) *LazyChunk {
	return &LazyChunk{
		Fetcher: nil,
		IsValid: true,
		Chunk:   newChunk(stream),
	}
}

func newLazyInvalidChunk(stream logproto.Stream) *LazyChunk {
	return &LazyChunk{
		Fetcher: nil,
		IsValid: false,
		Chunk:   newChunk(stream),
	}
}

func newChunk(stream logproto.Stream) chunk.Chunk {
	lbs, err := syntax.ParseLabels(stream.Labels)
	if err != nil {
		panic(err)
	}
	if !lbs.Has(labels.MetricName) {
		builder := labels.NewBuilder(lbs)
		builder.Set(labels.MetricName, "logs")
		lbs = builder.Labels(nil)
	}
	from, through := loki_util.RoundToMilliseconds(stream.Entries[0].Timestamp, stream.Entries[len(stream.Entries)-1].Timestamp)
	chk := chunkenc.NewMemChunk(chunkenc.EncGZIP, chunkenc.UnorderedHeadBlockFmt, 256*1024, 0)
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

func newMatchers(matchers string) []*labels.Matcher {
	res, err := syntax.ParseMatchers(matchers)
	if err != nil {
		panic(err)
	}
	return res
}

func newQuery(query string, start, end time.Time, shards []astmapper.ShardAnnotation, deletes []*logproto.Delete) *logproto.QueryRequest {
	req := &logproto.QueryRequest{
		Selector:  query,
		Start:     start,
		Limit:     1000,
		End:       end,
		Direction: logproto.FORWARD,
		Deletes:   deletes,
	}
	for _, shard := range shards {
		req.Shards = append(req.Shards, shard.String())
	}
	return req
}

func newSampleQuery(query string, start, end time.Time, deletes []*logproto.Delete) *logproto.SampleQueryRequest {
	req := &logproto.SampleQueryRequest{
		Selector: query,
		Start:    start,
		End:      end,
		Deletes:  deletes,
	}
	return req
}

type mockChunkStore struct {
	schemas config.SchemaConfig
	chunks  []chunk.Chunk
	client  *mockChunkStoreClient
	f       chunk.RequestChunkFilterer
}

// mockChunkStore cannot implement both chunk.Store and chunk.Client,
// since there is a conflict in signature for DeleteChunk method.
var (
	_ stores.Store       = &mockChunkStore{}
	_ chunkclient.Client = &mockChunkStoreClient{}
)

func newMockChunkStore(streams []*logproto.Stream) *mockChunkStore {
	chunks := make([]chunk.Chunk, 0, len(streams))
	for _, s := range streams {
		chunks = append(chunks, newChunk(*s))
	}
	return &mockChunkStore{schemas: config.SchemaConfig{}, chunks: chunks, client: &mockChunkStoreClient{chunks: chunks, scfg: config.SchemaConfig{}}}
}

func (m *mockChunkStore) Put(ctx context.Context, chunks []chunk.Chunk) error { return nil }
func (m *mockChunkStore) PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error {
	return nil
}

func (m *mockChunkStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
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
			l := labels.NewBuilder(c.Metric).Del(labels.MetricName).Labels(nil)
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

func (m *mockChunkStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (m *mockChunkStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	return nil, nil
}

func (m *mockChunkStore) SetChunkFilterer(f chunk.RequestChunkFilterer) {
	m.f = f
}

func (m *mockChunkStore) DeleteChunk(ctx context.Context, from, through model.Time, userID, chunkID string, metric labels.Labels, partiallyDeletedInterval *model.Interval) error {
	return nil
}

func (m *mockChunkStore) DeleteSeriesIDs(ctx context.Context, from, through model.Time, userID string, metric labels.Labels) error {
	return nil
}
func (m *mockChunkStore) Stop() {}
func (m *mockChunkStore) Get(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]chunk.Chunk, error) {
	return nil, nil
}

func (m *mockChunkStore) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return nil
}

func (m *mockChunkStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	refs := make([]chunk.Chunk, 0, len(m.chunks))
	// transform real chunks into ref chunks.
	for _, c := range m.chunks {
		r, err := chunk.ParseExternalKey("fake", m.schemas.ExternalKey(c.ChunkRef))
		if err != nil {
			panic(err)
		}
		refs = append(refs, r)
	}

	cache, err := cache.New(cache.Config{Prefix: "chunks"}, nil, util_log.Logger, stats.ChunkCache)
	if err != nil {
		panic(err)
	}

	f, err := fetcher.New(cache, false, m.schemas, m.client, 10, 100)
	if err != nil {
		panic(err)
	}
	return [][]chunk.Chunk{refs}, []*fetcher.Fetcher{f}, nil
}

func (m *mockChunkStore) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*index_stats.Stats, error) {
	return nil, nil
}

type mockChunkStoreClient struct {
	chunks []chunk.Chunk
	scfg   config.SchemaConfig
}

func (m mockChunkStoreClient) Stop() {
	panic("implement me")
}

func (m mockChunkStoreClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	return nil
}

func (m mockChunkStoreClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	var res []chunk.Chunk
	for _, c := range chunks {
		for _, sc := range m.chunks {
			// only returns chunks requested using the external key
			if m.scfg.ExternalKey(c.ChunkRef) == m.scfg.ExternalKey(sc.ChunkRef) {
				res = append(res, sc)
			}
		}
	}
	return res, nil
}

func (m mockChunkStoreClient) DeleteChunk(ctx context.Context, userID, chunkID string) error {
	return nil
}

func (m mockChunkStoreClient) IsChunkNotFoundErr(_ error) bool {
	return false
}

var streamsFixture = []*logproto.Stream{
	{
		Labels: "{foo=\"bar\"}",
		Entries: []logproto.Entry{
			{
				Timestamp: from,
				Line:      "1",
			},

			{
				Timestamp: from.Add(time.Millisecond),
				Line:      "2",
			},
			{
				Timestamp: from.Add(2 * time.Millisecond),
				Line:      "3",
			},
		},
	},
	{
		Labels: "{foo=\"bar\"}",
		Entries: []logproto.Entry{
			{
				Timestamp: from.Add(2 * time.Millisecond),
				Line:      "3",
			},
			{
				Timestamp: from.Add(3 * time.Millisecond),
				Line:      "4",
			},

			{
				Timestamp: from.Add(4 * time.Millisecond),
				Line:      "5",
			},
			{
				Timestamp: from.Add(5 * time.Millisecond),
				Line:      "6",
			},
		},
	},
	{
		Labels: "{foo=\"bazz\"}",
		Entries: []logproto.Entry{
			{
				Timestamp: from,
				Line:      "1",
			},

			{
				Timestamp: from.Add(time.Millisecond),
				Line:      "2",
			},
			{
				Timestamp: from.Add(2 * time.Millisecond),
				Line:      "3",
			},
		},
	},
	{
		Labels: "{foo=\"bazz\"}",
		Entries: []logproto.Entry{
			{
				Timestamp: from.Add(2 * time.Millisecond),
				Line:      "3",
			},
			{
				Timestamp: from.Add(3 * time.Millisecond),
				Line:      "4",
			},

			{
				Timestamp: from.Add(4 * time.Millisecond),
				Line:      "5",
			},
			{
				Timestamp: from.Add(5 * time.Millisecond),
				Line:      "6",
			},
		},
	},
}
var storeFixture = newMockChunkStore(streamsFixture)
