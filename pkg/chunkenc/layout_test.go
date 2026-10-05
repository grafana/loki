package chunkenc

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var layoutPieces = []string{"abc", "e5e650", "ab", "a", "b", "0", "{", `"`, ":", "=", " ", "  ", "xyz", "é", "\x00", "\xff", "_", "-", "session", "\n"}

func randomLayoutLine(rnd *rand.Rand) string {
	var sb strings.Builder
	for range rnd.Intn(12) {
		sb.WriteString(layoutPieces[rnd.Intn(len(layoutPieces))])
	}
	return sb.String()
}

func randomLayoutEntries(rnd *rand.Rand, n int) []logproto.Entry {
	entries := make([]logproto.Entry, 0, n)
	ts := int64(1)
	for range n {
		ts += int64(rnd.Intn(3))
		e := logproto.Entry{Timestamp: time.Unix(0, ts), Line: randomLayoutLine(rnd)}
		switch rnd.Intn(3) {
		case 1:
			e.StructuredMetadata = logproto.FromLabelsToLabelAdapters(labels.FromStrings("trace_id", strconv.Itoa(rnd.Intn(5))))
		case 2:
			e.StructuredMetadata = logproto.FromLabelsToLabelAdapters(labels.FromStrings("a", "1", "b", randomLayoutLine(rnd)))
		}
		entries = append(entries, e)
	}
	return entries
}

// layoutChunks returns the same entries as V4, V5 and V6 chunks read back from bytes.
func layoutChunks(t *testing.T, entries []logproto.Entry) map[byte]*MemChunk {
	t.Helper()
	c := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, 300, 0)
	for i := range entries {
		_, err := c.Append(&entries[i])
		require.NoError(t, err)
	}
	require.NoError(t, c.Close())
	v4Bytes, err := c.Bytes()
	require.NoError(t, err)

	out := map[byte]*MemChunk{}
	for _, format := range []byte{ChunkFormatV4, ChunkFormatV5, ChunkFormatV6} {
		mc, err := NewByteChunk(v4Bytes, 0, 0)
		require.NoError(t, err)
		if format != ChunkFormatV4 {
			require.NoError(t, ReencodeLayout(mc, format))
			b, err := mc.Bytes()
			require.NoError(t, err)
			mc, err = NewByteChunk(b, 0, 0)
			require.NoError(t, err)
			require.Equal(t, format, mc.format)
		}
		out[format] = mc
	}
	return out
}

func layoutQuery(t *testing.T, c *MemChunk, query string) []string {
	t.Helper()
	expr, err := syntax.ParseLogSelector(query, true)
	require.NoError(t, err)
	p, err := expr.Pipeline()
	require.NoError(t, err)
	it, err := c.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), logproto.FORWARD, p.ForStream(labels.FromStrings("app", "test")))
	require.NoError(t, err)
	var got []string
	for it.Next() {
		e := it.At()
		got = append(got, fmt.Sprintf("%d %q %v %v %s", e.Timestamp.UnixNano(), e.Line, e.StructuredMetadata, e.Parsed, it.Labels()))
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
	return got
}

func TestLayoutMatchesV4(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		rnd := rand.New(rand.NewSource(seed))
		entries := randomLayoutEntries(rnd, 200)
		chunks := layoutChunks(t, entries)
		require.Greater(t, len(chunks[ChunkFormatV4].blocks), 3)

		queries := []string{`{app="test"}`, `{app="test"} |= ""`, `{app="test"} |~ "(?i)ABC"`, `{app="test"} | trace_id="1"`}
		for range 60 {
			line := entries[rnd.Intn(len(entries))].Line
			if line == "" {
				continue
			}
			start := rnd.Intn(len(line))
			lit := line[start : start+1+rnd.Intn(len(line)-start)]
			queries = append(queries, `{app="test"} |= `+strconv.Quote(lit))
		}
		// Literals that only occur across two neighbouring lines.
		for i := 0; i+1 < len(entries); i += 17 {
			a, b := entries[i].Line, entries[i+1].Line
			if a == "" || b == "" {
				continue
			}
			lit := a[rnd.Intn(len(a)):] + b[:1+rnd.Intn(len(b))]
			queries = append(queries, `{app="test"} |= `+strconv.Quote(lit))
		}
		queries = append(queries,
			`{app="test"} |= "zz-not-there"`,
			`{app="test"} |= "e5e650" |= "abc"`,
			`{app="test"} |= "session" != "abc"`,
			`{app="test"} |= "a" | trace_id="2"`,
			`{app="test"} |= "\"abc"`,
			`{app="test"} |= "b:"`,
			`{app="test"} |= "\n"`,
		)
		for _, q := range queries {
			want := layoutQuery(t, chunks[ChunkFormatV4], q)
			for _, format := range []byte{ChunkFormatV5, ChunkFormatV6} {
				require.Equal(t, want, layoutQuery(t, chunks[format], q), "seed %d format %d query %s", seed, format, q)
			}
		}
	}
}

func TestLayoutSamplesMatchV4(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	chunks := layoutChunks(t, randomLayoutEntries(rnd, 300))
	count := func(c *MemChunk) []logproto.Sample {
		ex, err := getStreamExtractor(`count_over_time({app="test"} |= "ab" [5m])`, labels.FromStrings("app", "test"))
		require.NoError(t, err)
		it := c.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), ex)
		var got []logproto.Sample
		for it.Next() {
			got = append(got, it.At())
		}
		require.NoError(t, it.Close())
		return got
	}
	want := count(chunks[ChunkFormatV4])
	require.NotEmpty(t, want)
	require.Equal(t, want, count(chunks[ChunkFormatV5]))
	require.Equal(t, want, count(chunks[ChunkFormatV6]))
}

func TestLayoutSamplePushdownMatchesV4(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	chunks := layoutChunks(t, randomLayoutEntries(rnd, 300))
	lbls := labels.FromStrings("app", "test")
	needles := []string{"session", "e5e650", "zzz_absent"}
	for _, needle := range needles {
		quoted := strconv.Quote(needle)
		queries := []string{
			`count_over_time({app="test"} |= ` + quoted + ` [1m])`,
			`sum(count_over_time({app="test"} |= ` + quoted + ` [1m]))`,
			`sum by (trace_id) (count_over_time({app="test"} |= ` + quoted + ` [1m]))`,
			`sum_over_time({app="test"} |= ` + quoted + ` | unwrap trace_id [1m])`,
		}
		for _, query := range queries {
			t.Run(query, func(t *testing.T) {
				ex, err := getStreamExtractor(query, lbls)
				require.NoError(t, err)
				require.NotNil(t, log.RequiredSampleLiteral(ex))

				want := layoutSamples(t, chunks[ChunkFormatV4], query)
				ResetLayoutStats()
				got := layoutSamples(t, chunks[ChunkFormatV6], query)
				require.Equal(t, want, got, query)
				if needle == "zzz_absent" {
					require.Zero(t, LayoutStats.BlocksDecompressed.Load())
				}
			})
		}
	}

	// |~ "session" is rewritten to a case-sensitive contains filter, so the literal is kept.
	simplified := `count_over_time({app="test"} |~ "session" [1m])`
	ex, err := getStreamExtractor(simplified, lbls)
	require.NoError(t, err)
	require.Equal(t, []byte("session"), log.RequiredSampleLiteral(ex))
	require.Equal(t, layoutSamples(t, chunks[ChunkFormatV4], simplified), layoutSamples(t, chunks[ChunkFormatV6], simplified))

	// A pattern the regexp simplifier cannot rewrite has no literal and still matches V4.
	regex := `count_over_time({app="test"} |~ "se.*ion" [1m])`
	ex, err = getStreamExtractor(regex, lbls)
	require.NoError(t, err)
	require.Nil(t, log.RequiredSampleLiteral(ex))
	require.Equal(t, layoutSamples(t, chunks[ChunkFormatV4], regex), layoutSamples(t, chunks[ChunkFormatV6], regex))
}

type layoutSample struct {
	ts     int64
	value  float64
	labels string
}

func layoutSamples(t *testing.T, c *MemChunk, query string) []layoutSample {
	t.Helper()
	ex, err := getStreamExtractor(query, labels.FromStrings("app", "test"))
	require.NoError(t, err)
	it := c.SampleIterator(context.Background(), time.Unix(0, 0), time.Unix(0, math.MaxInt64), ex)
	var got []layoutSample
	for it.Next() {
		s := it.At()
		got = append(got, layoutSample{ts: s.Timestamp, value: s.Value, labels: it.Labels()})
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
	return got
}

func TestLiteralPlanCandidates(t *testing.T) {
	lines := []string{"xab", "cab", "", "abab", "a", "bx", "ab"}
	var region []byte
	offs := []int32{0}
	for _, l := range lines {
		region = append(region, l...)
		offs = append(offs, int32(len(region)))
	}
	p := newLiteralPlan([]byte("ab"), ChunkFormatV5)
	// "a" + "bx" is a match across a line boundary.
	require.Equal(t, []int32{0, 1, 3, 6}, p.candidates(region, offs, nil))
	require.Empty(t, newLiteralPlan([]byte("bc"), ChunkFormatV5).candidates(region, offs, nil))
}

func TestDictMayContainHasNoFalseNegatives(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	for range 300 {
		entries := make([]layoutEntry, 1+rnd.Intn(20))
		for i := range entries {
			entries[i] = layoutEntry{line: []byte(randomLayoutLine(rnd)), metadata: appendMetadataSection(nil, nil)}
		}
		block, _, err := encodeV6Block(entries, compression.GetWriterPool(compression.Snappy))
		require.NoError(t, err)
		d := layoutBuf{b: block}
		d.size()
		d.size()
		tokenLens := d.bytes(d.size())
		dict := d.bytes(d.size())
		require.False(t, d.bad)

		var region []byte
		for _, e := range entries {
			region = append(region, e.line...)
		}
		if len(region) == 0 {
			continue
		}
		for range 50 {
			start := rnd.Intn(len(region))
			lit := region[start : start+1+rnd.Intn(min(len(region)-start, 30))]
			p := newLiteralPlan(lit, ChunkFormatV6)
			require.True(t, p.dictMayContain(dict, tokenLens), "literal %q region %q", lit, region)
		}
		require.False(t, newLiteralPlan([]byte("qqq"), ChunkFormatV6).dictMayContain(dict, tokenLens))
	}
}

func TestRequiredLiteralPushdown(t *testing.T) {
	for query, want := range map[string]string{
		`{app="test"} |= "abc"`:                   "abc",
		`{app="test"} |= "ab" |= "abcd"`:          "abcd",
		`{app="test"} |= "abc" != "x"`:            "abc",
		`{app="test"} != "abc"`:                   "",
		`{app="test"} |~ "(?i)abc"`:               "",
		`{app="test"} |= ""`:                      "",
		`{app="test"} | line_format "x" |= "abc"`: "",
		`{app="test"} |= "abc" | line_format "x"`: "abc",
		`{app="test"} |~ "abc|def"`:               "",
	} {
		expr, err := syntax.ParseLogSelector(query, true)
		require.NoError(t, err)
		p, err := expr.Pipeline()
		require.NoError(t, err)
		require.Equal(t, want, string(log.RequiredLiteral(p.ForStream(labels.FromStrings("app", "test")))), query)
	}
}

func TestCutWritesLayoutFormat(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	entries := randomLayoutEntries(rnd, 80)
	v4 := NewMemChunk(ChunkFormatV4, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, 256, 0)
	for i := range entries {
		_, err := v4.Append(&entries[i])
		require.NoError(t, err)
	}
	require.NoError(t, v4.Close())

	for _, format := range []byte{ChunkFormatV5, ChunkFormatV6} {
		c := NewMemChunk(format, compression.Snappy, UnorderedWithStructuredMetadataHeadBlockFmt, 256, 0)
		for i := range entries {
			_, err := c.Append(&entries[i])
			require.NoError(t, err)
		}
		require.NoError(t, c.Close())
		require.Equal(t, format, c.format)
		require.NotEmpty(t, c.blocks)

		b, err := c.Bytes()
		require.NoError(t, err)
		decoded, err := NewByteChunk(b, 0, 0)
		require.NoError(t, err)
		require.Equal(t, format, decoded.format)
		require.Equal(t, layoutQuery(t, v4, `{app="test"} |= "session"`), layoutQuery(t, decoded, `{app="test"} |= "session"`))
	}
}

func uvarints(ns ...int) []byte {
	var b []byte
	for _, n := range ns {
		b = binary.AppendUvarint(b, uint64(n))
	}
	return b
}

func TestRebuildLines(t *testing.T) {
	longTok := []byte(strings.Repeat("L", 20))
	exactTok := []byte(strings.Repeat("E", 16))
	shortTok := []byte("hi")
	dict := append(append(append([]byte{}, longTok...), exactTok...), shortTok...)
	tokenLens := uvarints(len(longTok), len(exactTok), len(shortTok))

	t.Run("mixed token lengths", func(t *testing.T) {
		ids := uvarints(2, 0, 1, 2)
		want := append(append(append(append([]byte{}, shortTok...), longTok...), exactTok...), shortTok...)
		var s layoutScratch
		got, err := s.rebuildLines(dict, tokenLens, 3, ids, len(want))
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, len(want), cap(got))
		// A second rebuild reuses the padded buffers.
		got, err = s.rebuildLines(dict, tokenLens, 3, ids, len(want))
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("last token ends at linesLen", func(t *testing.T) {
		var s layoutScratch
		got, err := s.rebuildLines(longTok, uvarints(len(longTok)), 1, uvarints(0), len(longTok))
		require.NoError(t, err)
		require.Equal(t, longTok, got)
	})

	t.Run("multibyte id", func(t *testing.T) {
		// Id 128 is a two-byte uvarint. The earlier tokens are empty.
		const id = 128
		lens := make([]int, id+1)
		lens[id] = len(shortTok)
		var s layoutScratch
		got, err := s.rebuildLines(shortTok, uvarints(lens...), id+1, uvarints(id), len(shortTok))
		require.NoError(t, err)
		require.Equal(t, shortTok, got)
	})

	one := []byte("ab")
	two := []byte("cd")
	pair := append(append([]byte{}, one...), two...)
	pairLens := uvarints(len(one), len(two))
	malformed := []struct {
		name      string
		dict      []byte
		tokenLens []byte
		numTokens int
		ids       []byte
		linesLen  int
	}{
		{name: "id at numTokens", dict: pair, tokenLens: pairLens, numTokens: 2, ids: uvarints(2), linesLen: len(pair)},
		{name: "more than linesLen", dict: pair, tokenLens: pairLens, numTokens: 2, ids: uvarints(0, 1), linesLen: len(one)},
		{name: "fewer than linesLen", dict: one, tokenLens: uvarints(len(one)), numTokens: 1, ids: uvarints(0), linesLen: len(one) + 1},
		{name: "truncated varint", dict: one, tokenLens: uvarints(len(one)), numTokens: 1, ids: []byte{0x80}, linesLen: len(one)},
	}
	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			var s layoutScratch
			got, err := s.rebuildLines(tc.dict, tc.tokenLens, tc.numTokens, tc.ids, tc.linesLen)
			require.ErrorIs(t, err, errLayoutBlock)
			require.Nil(t, got)
		})
	}
}
