package storage

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/davecgh/go-spew/spew"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
)

var fooLabels = "{foo=\"bar\"}"

var from = time.Unix(0, time.Millisecond.Nanoseconds())

func assertStream(t *testing.T, expected, actual []*logproto.Stream) {
	if len(expected) != len(actual) {
		t.Fatalf("error stream length are different expected %d actual %d\n%s", len(expected), len(actual), spew.Sdump(expected, actual))
		return
	}
	sort.Slice(expected, func(i int, j int) bool { return expected[i].Labels < expected[j].Labels })
	sort.Slice(actual, func(i int, j int) bool { return actual[i].Labels < actual[j].Labels })
	for i := range expected {
		assert.Equal(t, expected[i].Labels, actual[i].Labels)
		if len(expected[i].Entries) != len(actual[i].Entries) {
			t.Fatalf("error entries length are different expected %d actual%d\n%s", len(expected[i].Entries), len(actual[i].Entries), spew.Sdump(expected[i].Entries, actual[i].Entries))

			return
		}
		for j := range expected[i].Entries {
			assert.Equal(t, expected[i].Entries[j].Timestamp.UnixNano(), actual[i].Entries[j].Timestamp.UnixNano())
			assert.Equal(t, expected[i].Entries[j].Line, actual[i].Entries[j].Line)
		}
	}
}

func newLazyChunk(stream logproto.Stream) *chunkenc.LazyChunk {
	return &chunkenc.LazyChunk{
		Fetcher: nil,
		Chunk:   newChunk(stream),
	}
}

func newChunk(stream logproto.Stream) chunk.Chunk {
	lbs, err := util.ToClientLabels(stream.Labels)
	if err != nil {
		panic(err)
	}
	l := client.FromLabelAdaptersToLabels(lbs)
	if !l.Has(labels.MetricName) {
		builder := labels.NewBuilder(l)
		builder.Set(labels.MetricName, "logs")
		l = builder.Labels()
	}
	from, through := model.TimeFromUnixNano(stream.Entries[0].Timestamp.UnixNano()), model.TimeFromUnixNano(stream.Entries[0].Timestamp.UnixNano())
	chk := chunkenc.NewMemChunk(chunkenc.EncGZIP)
	for _, e := range stream.Entries {
		if e.Timestamp.UnixNano() < from.UnixNano() {
			from = model.TimeFromUnixNano(e.Timestamp.UnixNano())
		}
		if e.Timestamp.UnixNano() > through.UnixNano() {
			through = model.TimeFromUnixNano(e.Timestamp.UnixNano())
		}
		_ = chk.Append(&e)
	}
	chk.Close()
	c := chunk.NewChunk("fake", client.Fingerprint(l), l, chunkenc.NewFacade(chk), from, through)
	// force the checksum creation
	if err := c.Encode(); err != nil {
		panic(err)
	}
	return c
}

func newMatchers(matchers string) []*labels.Matcher {
	res, err := logql.ParseMatchers(matchers)
	if err != nil {
		panic(err)
	}
	return res
}

func newQuery(query string, start, end time.Time, direction logproto.Direction) *logproto.QueryRequest {
	return &logproto.QueryRequest{
		Selector:  query,
		Start:     start,
		Limit:     1000,
		End:       end,
		Direction: direction,
	}
}

type mockChunkStore struct {
	chunks []chunk.Chunk
}

func newMockChunkStore(streams []*logproto.Stream) *mockChunkStore {
	chunks := make([]chunk.Chunk, 0, len(streams))
	for _, s := range streams {
		chunks = append(chunks, newChunk(*s))
	}
	return &mockChunkStore{chunks: chunks}
}
func (m *mockChunkStore) Put(ctx context.Context, chunks []chunk.Chunk) error { return nil }
func (m *mockChunkStore) PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error {
	return nil
}
func (m *mockChunkStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string) ([]string, error) {
	return nil, nil
}
func (m *mockChunkStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	return nil, nil
}
func (m *mockChunkStore) Stop() {}
func (m *mockChunkStore) Get(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]chunk.Chunk, error) {
	return nil, nil
}

// PutChunks implements ObjectClient from Fetcher
func (m *mockChunkStore) PutChunks(ctx context.Context, chunks []chunk.Chunk) error { return nil }

// GetChunks implements ObjectClient from Fetcher
func (m *mockChunkStore) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	var res []chunk.Chunk
	for _, c := range chunks {
		for _, sc := range m.chunks {
			// only returns chunks requested using the external key
			if c.ExternalKey() == sc.ExternalKey() {
				res = append(res, sc)
			}
		}
	}
	return res, nil
}

func (m *mockChunkStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	refs := make([]chunk.Chunk, 0, len(m.chunks))
	// transform real chunks into ref chunks.
	for _, c := range m.chunks {
		r, err := chunk.ParseExternalKey("fake", c.ExternalKey())
		if err != nil {
			panic(err)
		}
		refs = append(refs, r)
	}
	f, err := chunk.NewChunkFetcher(cache.Config{}, false, m)
	if err != nil {
		panic(err)
	}
	return [][]chunk.Chunk{refs}, []*chunk.Fetcher{f}, nil
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
