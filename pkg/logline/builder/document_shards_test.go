package builder

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// documentShardsConfig is roundTripConfig at the given version and document
// shard count with a 16s interval, validated like a service config.
func documentShardsConfig(t *testing.T, version string, shards int) Config {
	t.Helper()
	cfg := roundTripConfig(t, 1, 64)
	cfg.Index.Version = version
	cfg.Index.DocumentShards = shards
	cfg.Index.DocumentInterval = 16 * time.Second
	require.NoError(t, cfg.Index.Validate())
	require.NoError(t, validateIndexSettings(cfg.Index))
	return cfg
}

// readDocsAndPostings reads a .lidx into its documents table and
// term(hex) → docIDs.
func readDocsAndPostings(t *testing.T, path string) ([]format.DocumentMetadata, map[string][]uint32) {
	t.Helper()
	reader, _, err := logline.OpenFile(path)
	require.NoError(t, err)
	defer reader.Close()

	postings := map[string][]uint32{}
	it, err := reader.NewTermIterator()
	require.NoError(t, err)
	for it.Next() {
		require.False(t, it.Bitmap().MatchesAll, "density must be disabled in this test")
		postings[fmt.Sprintf("%x", it.Term())] = it.Bitmap().Roaring.ToArray()
	}
	require.NoError(t, it.Err())
	return reader.Documents(), postings
}

// TestStreamIngester_DocumentShardIsIngesterFingerprint pins the builder's
// stream hash to the ingester's: queriers match document shards against chunk
// fingerprints, so any drift silently drops data once hints prune by shard.
func TestStreamIngester_DocumentShardIsIngesterFingerprint(t *testing.T) {
	const shards = 32
	b, err := newIndexBuilder(documentShardsConfig(t, "v5", shards), "", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()

	for _, s := range []string{
		`{app="api"}`,
		`{app="api", cluster="eu-west-1", namespace="loki"}`,
		`{service_name="checkout", env="prod", pod="checkout-7d9f8c6b5-x2x4q"}`,
		// Over 1 KiB, where StableHash switches to its streaming path.
		`{app="` + strings.Repeat("x", 2048) + `"}`,
	} {
		ls := parseLabelsOrNil(s)
		require.NotNil(t, ls, s)

		// The ingester's fingerprint (instance.getHashForLabels).
		ingesterFP, _ := ls.HashWithoutLabels(nil)
		require.Equal(t, labels.StableHash(*ls), ingesterFP, s)

		got := b.ing.documentShard(ls)
		require.Equal(t, logline.DocumentShard(ingesterFP, shards), got, s)
		require.True(t, index.NewShard(got, shards).Match(model.Fingerprint(ingesterFP)), s)
	}
}

// TestBuilder_DocumentShardsOneMatchesTimeOnly checks that a v5 index with
// one document shard has exactly the documents and postings of a time-only
// v4 index (same extractor) over the same data.
func TestBuilder_DocumentShardsOneMatchesTimeOnly(t *testing.T) {
	build := func(version string, shards int) ([]format.DocumentMetadata, map[string][]uint32) {
		cfg := documentShardsConfig(t, version, shards)
		b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
		require.NoError(t, err)
		defer b.clear()

		stream := &logproto.Stream{Labels: `{app="api"}`, Entries: retryTestEntries()}
		require.NoError(t, b.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{}))
		files, err := b.prepareIndexes()
		require.NoError(t, err)
		require.Len(t, files, 1)
		return readDocsAndPostings(t, files[0].file.Name())
	}

	wantDocs, wantPostings := build("v4", 0)
	gotDocs, gotPostings := build("v5", 1)
	require.NotEmpty(t, wantDocs)
	require.Equal(t, wantDocs, gotDocs)
	require.Equal(t, wantPostings, gotPostings)
}

// streamsInDifferentDocumentShards returns two label sets whose streams land
// in different document shards, ordered by shard.
func streamsInDifferentDocumentShards(t *testing.T, shards int) (lo, hi labels.Labels) {
	t.Helper()
	first := labels.FromStrings("app", "stream-0")
	firstShard := logline.DocumentShard(labels.StableHash(first), shards)
	for i := 1; i < 1000; i++ {
		ls := labels.FromStrings("app", fmt.Sprintf("stream-%d", i))
		if s := logline.DocumentShard(labels.StableHash(ls), shards); s != firstShard {
			if s < firstShard {
				return ls, first
			}
			return first, ls
		}
	}
	t.Fatal("no two streams in different document shards")
	return labels.EmptyLabels(), labels.EmptyLabels()
}

// TestBuilder_DocumentShardsSplitCells checks that two streams in different
// document shards get separate documents for the same interval. Both
// documents carry the interval's bounds, so time-only readers still see one
// range per term.
func TestBuilder_DocumentShardsSplitCells(t *testing.T) {
	const shards = 32
	lo, hi := streamsInDifferentDocumentShards(t, shards)

	cfg := documentShardsConfig(t, "v5", shards)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()

	ts := time.Date(2026, 3, 22, 12, 0, 5, 0, time.UTC)
	loLines := []string{"shared payment processed line", "alpha only lowshard message"}
	hiLines := []string{"shared payment processed line", "bravo unique highshard text"}
	process := func(ls labels.Labels, lines []string) {
		var entries []logproto.Entry
		for _, line := range lines {
			entries = append(entries, logproto.Entry{Timestamp: ts, Line: line})
		}
		require.NoError(t, b.processStream(&logproto.Stream{Labels: ls.String(), Entries: entries}, &ls, time.Now(), recordRef{}))
	}
	process(hi, hiLines)
	process(lo, loLines)

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Len(t, files, 1)
	docs, postings := readDocsAndPostings(t, files[0].file.Name())

	// Dense ranks follow cell order, so the lower document shard is doc 0.
	start := ts.Truncate(16 * time.Second)
	require.Equal(t, []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: start.UnixMilli(), MaxTimeUnix: start.Add(16 * time.Second).UnixMilli()},
		{ID: 1, MinTimeUnix: start.UnixMilli(), MaxTimeUnix: start.Add(16 * time.Second).UnixMilli()},
	}, docs)

	extractFn, err := logline.ExtractorForVersion("v5")
	require.NoError(t, err)
	termsOf := func(ls labels.Labels, lines []string) map[string]bool {
		var values []string
		ls.Range(func(l labels.Label) { values = append(values, l.Value) })
		out := map[string]bool{}
		for _, line := range lines {
			for _, g := range extractFn(cfg.Index.NgramLength, line, nil, values, nil) {
				out[fmt.Sprintf("%x", g)] = true
			}
		}
		return out
	}
	loTerms, hiTerms := termsOf(lo, loLines), termsOf(hi, hiLines)

	want := map[string][]uint32{}
	for term := range loTerms {
		want[term] = []uint32{0}
	}
	for term := range hiTerms {
		want[term] = append(want[term], 1)
	}
	require.Equal(t, want, postings)
	shared := 0
	for _, ids := range want {
		if len(ids) == 2 {
			shared++
		}
	}
	require.Positive(t, shared, "the shared line must produce terms in both documents")

	for term, ranges := range readTermRanges(t, files[0].file.Name()) {
		require.Len(t, ranges, 1, "term %s must read back as one time range", term)
	}
}

// TestBuilder_DocumentShardsWindowBoundary checks that composite cells use the
// whole uint32 window: the last interval before the window end indexes every
// document shard, and the window end panics.
func TestBuilder_DocumentShardsWindowBoundary(t *testing.T) {
	const shards = 32
	cfg := documentShardsConfig(t, "v5", shards)
	b, err := newIndexBuilder(cfg, "", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()

	windowEnd := docIDWindowEnd(cfg.Index.DocumentInterval, shards)
	require.Equal(t, time.Date(2094, 1, 19, 3, 14, 8, 0, time.UTC), windowEnd)

	lastShard := b.ing.postings.absCell(windowEnd.Add(-time.Nanosecond).UnixNano(), shards-1)
	tick, ok := b.ing.postings.tick(lastShard)
	require.True(t, ok)
	require.Equal(t, uint32(1<<32-1), tick)

	stream := &logproto.Stream{Labels: `{app="api"}`, Entries: []logproto.Entry{{Timestamp: windowEnd, Line: "boundary probe line"}}}
	defer func() {
		msg, _ := recover().(string)
		require.Contains(t, msg, "16s document_interval × 32 document_shards")
	}()
	_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})
}
