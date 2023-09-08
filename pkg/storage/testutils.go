package storage

import (
	"sort"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/stores"
)

var (
	fooLabelsWithName = labels.Labels{{Name: "foo", Value: "bar"}, {Name: "__name__", Value: "logs"}}
	fooLabels         = labels.Labels{{Name: "foo", Value: "bar"}}
)

var from = time.Unix(0, time.Millisecond.Nanoseconds())

func AssertStream(t *testing.T, expected, actual []logproto.Stream) {
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

func AssertSeries(t *testing.T, expected, actual []logproto.Series) {
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

func newLazyChunk(chunkFormat byte, headfmt chunkenc.HeadBlockFmt, stream logproto.Stream) *LazyChunk {
	return &LazyChunk{
		Fetcher: nil,
		IsValid: true,
		Chunk:   stores.NewTestChunk(chunkFormat, headfmt, stream),
	}
}

func newLazyInvalidChunk(chunkFormat byte, headfmt chunkenc.HeadBlockFmt, stream logproto.Stream) *LazyChunk {
	return &LazyChunk{
		Fetcher: nil,
		IsValid: false,
		Chunk:   stores.NewTestChunk(chunkFormat, headfmt, stream),
	}
}

func newMatchers(matchers string) []*labels.Matcher {
	res, err := syntax.ParseMatchers(matchers, true)
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
