package builder

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
// shard bits with a 16s interval, validated like a service config.
func documentShardsConfig(t *testing.T, version string, shardBits uint) Config {
	t.Helper()
	cfg := roundTripConfig(t, 1, 64)
	cfg.Index.Version = version
	cfg.Index.DocumentShardBits = int(shardBits)
	cfg.Index.DocumentInterval = 16 * time.Second
	require.NoError(t, cfg.Index.Validate())
	require.NoError(t, validateIndexSettings(cfg.Index))
	return cfg
}

// readLayoutAndPostings reads a v5 .lidx into its document layout and
// term(hex) → cells.
func readLayoutAndPostings(t *testing.T, path string) (format.DocumentLayout, format.HeaderInfo, map[string][]uint32) {
	t.Helper()
	reader, version, err := logline.OpenFile(path)
	require.NoError(t, err)
	defer reader.Close()
	require.Equal(t, "v5", version)
	require.Nil(t, reader.Documents(), "v5 indexes have no document table")
	layout, ok := logline.DocumentLayout(reader)
	require.True(t, ok)

	postings := map[string][]uint32{}
	it, err := reader.NewTermIterator()
	require.NoError(t, err)
	for it.Next() {
		require.False(t, it.Bitmap().MatchesAll, "density must be disabled in this test")
		postings[fmt.Sprintf("%x", it.Term())] = it.Bitmap().Roaring.ToArray()
	}
	require.NoError(t, it.Err())
	return layout, reader.ReadHeader(), postings
}

// TestStreamIngester_DocumentShardIsStreamFingerprint pins the builder's
// stream hash to logline.StreamFingerprint, which pkg/ingester's
// TestGetHashForLabels_MatchesLoglineStreamFingerprint pins to the ingester:
// queriers match document shards against chunk fingerprints, so any drift
// silently drops data once hints prune by shard.
func TestStreamIngester_DocumentShardIsStreamFingerprint(t *testing.T) {
	const shardBits = 5
	b, err := newIndexBuilder(documentShardsConfig(t, "v5", shardBits), "", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
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

		fp, _ := logline.StreamFingerprint(*ls, nil)
		require.Equal(t, labels.StableHash(*ls), fp, s)

		got := b.ing.documentShard(ls)
		require.Equal(t, logline.DocumentShard(fp, shardBits), got, s)
		require.True(t, index.NewShard(got, 1<<shardBits).Match(model.Fingerprint(fp)), s)
	}
}

// TestBuilder_DocumentShardsOneMatchesTimeOnly checks that a v5 index with
// 0 document shard bits (one shard) maps every term to the same time ranges
// as a time-only v4 index (same extractor) over the same data, and that its
// postings are the day's buckets rather than dense ranks.
func TestBuilder_DocumentShardsOneMatchesTimeOnly(t *testing.T) {
	build := func(version string, read func(path string)) {
		cfg := documentShardsConfig(t, version, 0)
		b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
		require.NoError(t, err)
		defer b.clear()

		stream := &logproto.Stream{Labels: `{app="api"}`, Entries: retryTestEntries()}
		require.NoError(t, b.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{}))
		files, err := b.prepareIndexes()
		require.NoError(t, err)
		require.Len(t, files, 1)
		read(files[0].file.Name())
	}

	var want, got map[string]map[docRange]bool
	var layout format.DocumentLayout
	var postings map[string][]uint32
	build("v4", func(path string) { want = readTermRanges(t, path) })
	build("v5", func(path string) {
		got = readTermRanges(t, path)
		layout, _, postings = readLayoutAndPostings(t, path)
	})
	require.NotEmpty(t, want)
	require.Equal(t, want, got)

	require.Equal(t, format.DocumentLayout{Interval: 16 * time.Second, DayStart: time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)}, layout)
	// The entries span 12:00:00 to 12:00:49: buckets 2700 to 2703 of the day.
	cells := map[uint32]bool{}
	for _, ids := range postings {
		for _, id := range ids {
			cells[id] = true
		}
	}
	require.Equal(t, map[uint32]bool{2700: true, 2701: true, 2702: true, 2703: true}, cells)
}

// streamsInDifferentDocumentShards returns two label sets whose streams land
// in different document shards, ordered by shard.
func streamsInDifferentDocumentShards(t *testing.T, shardBits uint) (lo, hi labels.Labels) {
	t.Helper()
	first := labels.FromStrings("app", "stream-0")
	firstShard := logline.DocumentShard(labels.StableHash(first), shardBits)
	for i := 1; i < 1000; i++ {
		ls := labels.FromStrings("app", fmt.Sprintf("stream-%d", i))
		if s := logline.DocumentShard(labels.StableHash(ls), shardBits); s != firstShard {
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
// document shards get separate cells of the same interval: the cell is the
// day's bucket shifted by the shard bits, plus the stream's document shard.
// Both cells map to the interval's bounds, so time-only readers still see one
// range per term.
func TestBuilder_DocumentShardsSplitCells(t *testing.T) {
	const shardBits = 5
	lo, hi := streamsInDifferentDocumentShards(t, shardBits)

	cfg := documentShardsConfig(t, "v5", shardBits)
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
	layout, header, postings := readLayoutAndPostings(t, files[0].file.Name())
	day := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	require.Equal(t, format.DocumentLayout{Interval: 16 * time.Second, ShardBits: shardBits, DayStart: day}, layout)
	require.Equal(t, uint32(2), header.DocumentCount, "DocumentCount counts the referenced cells")

	bucket := uint32(ts.Sub(day) / (16 * time.Second))
	loCell := bucket<<shardBits | logline.DocumentShard(labels.StableHash(lo), shardBits)
	hiCell := bucket<<shardBits | logline.DocumentShard(labels.StableHash(hi), shardBits)
	for _, cell := range []uint32{loCell, hiCell} {
		start, end := layout.TimeRange(cell)
		require.Equal(t, ts.Truncate(16*time.Second), start)
		require.Equal(t, start.Add(16*time.Second), end)
	}

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
		want[term] = []uint32{loCell}
	}
	for term := range hiTerms {
		want[term] = append(want[term], hiCell)
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

// TestBuilder_DocumentShardsSplitAtMidnight checks that a v5 build spanning
// midnight writes one file per day, each starting at its own midnight, so the
// last bucket of one day and the first of the next are cells of different
// files rather than consecutive cells of one.
func TestBuilder_DocumentShardsSplitAtMidnight(t *testing.T) {
	const shardBits = 5
	cfg := documentShardsConfig(t, "v5", shardBits)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()

	ls := labels.FromStrings("app", "api")
	documentShard := logline.DocumentShard(labels.StableHash(ls), shardBits)
	day1 := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	entries := []logproto.Entry{
		{Timestamp: day2.Add(-5 * time.Second), Line: "midnight crossing payment processed"},
		{Timestamp: day2.Add(5 * time.Second), Line: "midnight crossing payment processed"},
	}
	require.NoError(t, b.processStream(&logproto.Stream{Labels: ls.String(), Entries: entries}, &ls, time.Now(), recordRef{}))

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Len(t, files, 2)
	byDate := map[string]string{}
	for _, f := range files {
		byDate[f.date] = f.file.Name()
	}

	for _, tt := range []struct {
		date     string
		dayStart time.Time
		cell     uint32
	}{
		{date: "2026-03-22", dayStart: day1, cell: 5399<<shardBits | documentShard},
		{date: "2026-03-23", dayStart: day2, cell: documentShard},
	} {
		require.Contains(t, byDate, tt.date)
		layout, header, postings := readLayoutAndPostings(t, byDate[tt.date])
		require.Equal(t, format.DocumentLayout{Interval: 16 * time.Second, ShardBits: shardBits, DayStart: tt.dayStart}, layout, tt.date)
		require.Equal(t, uint32(1), header.DocumentCount, tt.date)
		require.NotEmpty(t, postings, tt.date)
		for term, cells := range postings {
			require.Equal(t, []uint32{tt.cell}, cells, "%s term %s", tt.date, term)
		}
	}
}

// TestBuilder_DocumentShardsReplayedIndexMerges checks that a v5 index built
// later for a day, as Replay does, merges with the index already built for that
// day: neither file depends on the builder that wrote it, so equal cells are
// equal documents.
func TestBuilder_DocumentShardsReplayedIndexMerges(t *testing.T) {
	const shardBits = 5
	day := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	build := func(ls labels.Labels, ts time.Time) string {
		b, err := newIndexBuilder(documentShardsConfig(t, "v5", shardBits), "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
		require.NoError(t, err)
		t.Cleanup(b.clear)
		entries := []logproto.Entry{{Timestamp: ts, Line: "replayed payment processed line"}}
		require.NoError(t, b.processStream(&logproto.Stream{Labels: ls.String(), Entries: entries}, &ls, time.Now(), recordRef{}))
		files, err := b.prepareIndexes()
		require.NoError(t, err)
		require.Len(t, files, 1)
		return files[0].file.Name()
	}
	live, replayed := streamsInDifferentDocumentShards(t, shardBits)
	livePath := build(live, day.Add(12*time.Hour))
	replayedPath := build(replayed, day.Add(12*time.Hour+time.Second))

	var readers []io.ReaderAt
	var sizes []int64
	for _, path := range []string{livePath, replayedPath} {
		f, err := os.Open(path)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		fi, err := f.Stat()
		require.NoError(t, err)
		readers = append(readers, f)
		sizes = append(sizes, fi.Size())
	}
	merger, err := logline.NewMerger("v5", &format.WriterConfig{DayStart: day})
	require.NoError(t, err)
	mergedPath := filepath.Join(t.TempDir(), "merged.lidx")
	out, err := os.Create(mergedPath)
	require.NoError(t, err)
	_, err = merger.Merge(context.Background(), readers, sizes, out)
	require.NoError(t, err)
	require.NoError(t, out.Close())

	bucket := uint32(12 * time.Hour / (16 * time.Second))
	liveCell := bucket<<shardBits | logline.DocumentShard(labels.StableHash(live), shardBits)
	replayedCell := bucket<<shardBits | logline.DocumentShard(labels.StableHash(replayed), shardBits)
	want := map[string][]uint32{}
	for _, path := range []string{livePath, replayedPath} {
		_, _, postings := readLayoutAndPostings(t, path)
		for term, cells := range postings {
			want[term] = append(want[term], cells...)
		}
	}
	require.Contains(t, want, fmt.Sprintf("%x", [8]byte{'R', 'E', 'P', 'L', 'A', 'Y'}))

	layout, header, postings := readLayoutAndPostings(t, mergedPath)
	require.Equal(t, format.DocumentLayout{Interval: 16 * time.Second, ShardBits: shardBits, DayStart: day}, layout)
	require.Equal(t, uint32(2), header.DocumentCount)
	require.Equal(t, want, postings)
	require.Equal(t, []uint32{liveCell, replayedCell}, postings[fmt.Sprintf("%x", [8]byte{'R', 'E', 'P', 'L', 'A', 'Y'})])
}

// TestBuilder_DocumentShardsWindowBoundary checks that composite cells use the
// whole uint32 window: the last interval before the window end indexes every
// document shard, and the window end panics.
func TestBuilder_DocumentShardsWindowBoundary(t *testing.T) {
	const shardBits = 5
	cfg := documentShardsConfig(t, "v5", shardBits)
	b, err := newIndexBuilder(cfg, "", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()

	windowEnd := docIDWindowEnd(cfg.Index.DocumentInterval, shardBits)
	require.Equal(t, time.Date(2094, 1, 19, 3, 14, 8, 0, time.UTC), windowEnd)

	lastShard := b.ing.postings.absCell(windowEnd.Add(-time.Nanosecond).UnixNano(), 1<<shardBits-1)
	tick, ok := b.ing.postings.tick(lastShard)
	require.True(t, ok)
	require.Equal(t, uint32(1<<32-1), tick)

	stream := &logproto.Stream{Labels: `{app="api"}`, Entries: []logproto.Entry{{Timestamp: windowEnd, Line: "boundary probe line"}}}
	defer func() {
		msg, _ := recover().(string)
		require.Contains(t, msg, "16s document_interval × 2^5 document shards")
	}()
	_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})
}
