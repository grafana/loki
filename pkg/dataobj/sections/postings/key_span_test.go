package postings_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

var (
	keyAppAPI = postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "api"}
	keyAppWeb = postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "web"}
	keyEnvDev = postings.Key{Kind: postings.KindLabel, ColumnName: "env", LabelValue: "dev"}
)

func mustKeySpan(t *testing.T, start, end *postings.Key) postings.KeySpan {
	t.Helper()
	span, err := postings.NewKeySpan(start, end)
	require.NoError(t, err)
	return span
}

func TestNewKeySpan(t *testing.T) {
	t.Run("returns a span that holds every key when both bounds are nil", func(t *testing.T) {
		require.True(t, mustKeySpan(t, nil, nil).IsAll())
	})

	t.Run("returns a span with its bounds when start sorts below end", func(t *testing.T) {
		span := mustKeySpan(t, &keyAppAPI, &keyEnvDev)
		start, ok := span.Start()
		require.True(t, ok)
		require.Equal(t, keyAppAPI, start)
		end, ok := span.End()
		require.True(t, ok)
		require.Equal(t, keyEnvDev, end)
		require.False(t, span.IsAll())
	})

	t.Run("returns an error when start equals end", func(t *testing.T) {
		_, err := postings.NewKeySpan(&keyAppAPI, &keyAppAPI)
		require.Error(t, err)
	})

	t.Run("returns an error when start sorts above end", func(t *testing.T) {
		_, err := postings.NewKeySpan(&keyEnvDev, &keyAppAPI)
		require.Error(t, err)
	})

	t.Run("returns an error when a kind does not fit in one byte", func(t *testing.T) {
		_, err := postings.NewKeySpan(nil, &postings.Key{Kind: 256})
		require.Error(t, err)
	})
}

func TestDecodeKeySpan(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end *postings.Key
	}{
		{name: "round trips a span with both bounds", start: &keyAppAPI, end: &keyEnvDev},
		{name: "round trips a span without a start", end: &keyAppWeb},
		{name: "round trips a span without an end", start: &keyAppWeb},
		{name: "round trips a span that holds every key"},
		{name: "round trips bounds with empty and binary strings", start: &postings.Key{Kind: postings.KindLabel}, end: &postings.Key{Kind: postings.KindLabel, ColumnName: "a", LabelValue: "\x00\xff"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			span := mustKeySpan(t, tc.start, tc.end)
			decoded, err := postings.DecodeKeySpan(span.Encode())
			require.NoError(t, err)
			require.Equal(t, span, decoded)
		})
	}

	t.Run("returns an error when an encoding is malformed", func(t *testing.T) {
		_, err := postings.DecodeKeySpan([]byte{0xff}, nil)
		require.Error(t, err)
	})
}

func TestKeySpan_Contains(t *testing.T) {
	span := mustKeySpan(t, &keyAppWeb, &keyEnvDev)
	for _, tc := range []struct {
		name string
		span postings.KeySpan
		key  postings.Key
		want bool
	}{
		{name: "holds its start", span: span, key: keyAppWeb, want: true},
		{name: "holds a key between its bounds", span: span, key: postings.Key{Kind: postings.KindLabel, ColumnName: "b"}, want: true},
		{name: "excludes a key below its start", span: span, key: keyAppAPI},
		{name: "excludes its end", span: span, key: keyEnvDev},
		{name: "holds every key below its end when it has no start", span: mustKeySpan(t, nil, &keyAppWeb), key: postings.Key{Kind: postings.KindBloom}, want: true},
		{name: "holds every key from its start when it has no end", span: mustKeySpan(t, &keyAppWeb, nil), key: postings.Key{Kind: 255}, want: true},
		{name: "holds any key when it has no bounds", key: keyAppAPI, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.span.Contains(tc.key))
		})
	}
}

func TestKeySpan_Overlaps(t *testing.T) {
	span := mustKeySpan(t, &keyAppWeb, &keyEnvDev)
	keyBloom := postings.Key{Kind: postings.KindBloom}
	keyZone := postings.Key{Kind: postings.KindLabel, ColumnName: "zone"}
	for _, tc := range []struct {
		name           string
		span           postings.KeySpan
		minKey, maxKey postings.Key
		want           bool
	}{
		{name: "overlaps a range that ends at its start", span: span, minKey: keyAppAPI, maxKey: keyAppWeb, want: true},
		{name: "overlaps a range that holds the whole span", span: span, minKey: keyBloom, maxKey: keyZone, want: true},
		{name: "overlaps a range inside it", span: span, minKey: keyAppWeb, maxKey: keyAppWeb, want: true},
		{name: "does not overlap a range below its start", span: span, minKey: keyBloom, maxKey: keyAppAPI},
		{name: "does not overlap a range that starts at its end", span: span, minKey: keyEnvDev, maxKey: keyZone},
		{name: "overlaps any range below its end when it has no start", span: mustKeySpan(t, nil, &keyEnvDev), minKey: keyBloom, maxKey: keyBloom, want: true},
		{name: "overlaps any range from its start when it has no end", span: mustKeySpan(t, &keyAppWeb, nil), minKey: keyZone, maxKey: postings.Key{Kind: 255}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.span.Overlaps(tc.minKey, tc.maxKey))
		})
	}
}

func TestKeySpan_HoldsRange(t *testing.T) {
	keyRange := func(t *testing.T, minKey, maxKey postings.Key) postings.KeyRange {
		t.Helper()
		r, err := postings.NewKeyRange(minKey, maxKey)
		require.NoError(t, err)
		return r
	}
	span := mustKeySpan(t, &keyAppAPI, &keyEnvDev)

	t.Run("holds a range inside the span", func(t *testing.T) {
		require.True(t, span.HoldsRange(keyRange(t, keyAppAPI, keyAppWeb)))
	})

	t.Run("does not hold a range that starts below the span", func(t *testing.T) {
		require.False(t, span.HoldsRange(keyRange(t, postings.Key{Kind: postings.KindBloom, ColumnName: "trace_id"}, keyAppWeb)))
	})

	t.Run("does not hold a range that reaches the end of the span", func(t *testing.T) {
		require.False(t, span.HoldsRange(keyRange(t, keyAppWeb, keyEnvDev)))
	})

	t.Run("does not hold the zero range when the span has bounds", func(t *testing.T) {
		require.False(t, span.HoldsRange(postings.KeyRange{}))
	})

	t.Run("holds the zero range when the span holds every key", func(t *testing.T) {
		require.True(t, postings.KeySpan{}.HoldsRange(postings.KeyRange{}))
	})
}

func TestKeySpan_Predicate(t *testing.T) {
	ctx := context.Background()
	ts := time.Unix(0, 0).UTC()

	// The sections hold every pair of these column names and label values,
	// and bloom rows, so each bound of a span falls between, on, and next to
	// rows of each level of the key. Pages of two rows let a reader skip
	// pages by their value ranges.
	build := func(pageMaxRowCount int) []*postings.Section {
		b := postings.NewBuilder(nil, 0, pageMaxRowCount, 1<<20)
		var stream int64
		for _, name := range []string{"", "a", "app", "b", "env"} {
			for _, value := range []string{"", "api", "dev", "web", "zz"} {
				stream++
				b.ObserveLabelPosting(postings.LabelObservation{
					ObjectPath: "/obj", ColumnName: name, LabelValue: value,
					StreamID: stream, Time: postings.At(ts), UncompressedSize: 1,
				})
			}
		}
		b.PrepareBloomColumn("/obj", 0, "trace_id", 1000, 0)
		require.NoError(t, b.ObserveBloomPosting(postings.BloomObservation{
			ObjectPath: "/obj", ColumnName: "trace_id", StreamID: 1, Timestamp: ts, UncompressedSize: 1,
		}))
		sections, closeSections := openPostingsSections(ctx, t, b)
		t.Cleanup(closeSections)
		return sections
	}
	sections := append(build(0), build(2)...)

	readKeys := func(t *testing.T, section *postings.Section, span postings.KeySpan) []postings.Key {
		t.Helper()
		predicate, err := span.Predicate(section)
		require.NoError(t, err)
		opts := postings.ReaderOptions{Columns: section.Columns()}
		if predicate != nil {
			opts.Predicates = []postings.Predicate{predicate}
		}
		reader := postings.NewReader(opts)
		require.NoError(t, reader.Open(ctx))
		rows := postings.NewRowReader(ctx, reader)
		defer func() { require.NoError(t, rows.Close()) }()
		var keys []postings.Key
		for rows.Next() {
			row := rows.At()
			keys = append(keys, postings.Key{Kind: row.Kind, ColumnName: row.ColumnName, LabelValue: row.LabelValue})
		}
		require.NoError(t, rows.Err())
		return keys
	}

	keyAppEmpty := postings.Key{Kind: postings.KindLabel, ColumnName: "app"}
	keyBetween := postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "b"}
	keyBloom := postings.Key{Kind: postings.KindBloom, ColumnName: "trace_id"}
	for _, tc := range []struct {
		name       string
		start, end *postings.Key
	}{
		{name: "keeps exactly the keys between two row keys", start: &keyAppAPI, end: &keyEnvDev},
		{name: "keeps exactly the keys from a bound between row keys", start: &keyBetween},
		{name: "keeps exactly the keys below a bound with an empty label value", end: &keyAppEmpty},
		{name: "keeps exactly the keys of one column name", start: &keyAppEmpty, end: &postings.Key{Kind: postings.KindLabel, ColumnName: "b"}},
		{name: "keeps exactly the keys from a bound of a lower kind", start: &keyBloom, end: &keyAppWeb},
		{name: "keeps exactly the keys below a bound of a higher kind", end: &postings.Key{Kind: 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			span := mustKeySpan(t, tc.start, tc.end)
			for _, section := range sections {
				var want []postings.Key
				for _, key := range readKeys(t, section, postings.KeySpan{}) {
					if span.Contains(key) {
						want = append(want, key)
					}
				}
				require.Equal(t, want, readKeys(t, section, span))
			}
		})
	}

	t.Run("returns no predicate when the span holds every key", func(t *testing.T) {
		require.NotEmpty(t, sections)
		predicate, err := postings.KeySpan{}.Predicate(sections[0])
		require.NoError(t, err)
		require.Nil(t, predicate)
	})
}
