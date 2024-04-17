package series

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

// Refer to https://github.com/prometheus/prometheus/issues/2651.
func TestFindSetMatches(t *testing.T) {
	cases := []struct {
		pattern string
		exp     []string
	}{
		// Simple sets.
		{
			pattern: "foo|bar|baz",
			exp: []string{
				"foo",
				"bar",
				"baz",
			},
		},
		// Simple sets containing escaped characters.
		{
			pattern: "fo\\.o|bar\\?|\\^baz",
			exp: []string{
				"fo.o",
				"bar?",
				"^baz",
			},
		},
		// Simple sets containing special characters without escaping.
		{
			pattern: "fo.o|bar?|^baz",
			exp:     nil,
		},
		{
			pattern: "foo\\|bar\\|baz",
			exp: []string{
				"foo|bar|baz",
			},
		},
	}

	for _, c := range cases {
		matches := FindSetMatches(c.pattern)
		require.Equal(t, c.exp, matches)
	}
}

func benchmarkParseIndexEntries(i int64, regex string, b *testing.B) {
	b.ReportAllocs()
	b.StopTimer()
	ctx := context.Background()
	entries := generateIndexEntries(i)
	matcher, err := labels.NewMatcher(labels.MatchRegexp, "", regex)
	if err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
	for n := 0; n < b.N; n++ {
		keys, err := parseIndexEntries(ctx, entries, matcher)
		if err != nil {
			b.Fatal(err)
		}
		if regex == ".*" && len(keys) != len(entries)/2 {
			b.Fatalf("expected keys:%d got:%d", len(entries)/2, len(keys))
		}
	}
}

func BenchmarkParseIndexEntries500(b *testing.B)   { benchmarkParseIndexEntries(500, ".*", b) }
func BenchmarkParseIndexEntries2500(b *testing.B)  { benchmarkParseIndexEntries(2500, ".*", b) }
func BenchmarkParseIndexEntries10000(b *testing.B) { benchmarkParseIndexEntries(10000, ".*", b) }
func BenchmarkParseIndexEntries50000(b *testing.B) { benchmarkParseIndexEntries(50000, ".*", b) }

func BenchmarkParseIndexEntriesRegexSet500(b *testing.B) {
	benchmarkParseIndexEntries(500, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func BenchmarkParseIndexEntriesRegexSet2500(b *testing.B) {
	benchmarkParseIndexEntries(2500, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func BenchmarkParseIndexEntriesRegexSet10000(b *testing.B) {
	benchmarkParseIndexEntries(10000, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func BenchmarkParseIndexEntriesRegexSet50000(b *testing.B) {
	benchmarkParseIndexEntries(50000, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func generateIndexEntries(n int64) []index.Entry {
	res := make([]index.Entry, 0, n)
	for i := n - 1; i >= 0; i-- {
		labelValue := fmt.Sprintf("labelvalue%d", i%(n/2))
		chunkID := fmt.Sprintf("chunkid%d", i%(n/2))
		rangeValue := []byte{}
		rangeValue = append(rangeValue, []byte("component1")...)
		rangeValue = append(rangeValue, 0)
		rangeValue = append(rangeValue, []byte(labelValue)...)
		rangeValue = append(rangeValue, 0)
		rangeValue = append(rangeValue, []byte(chunkID)...)
		rangeValue = append(rangeValue, 0)
		res = append(res, index.Entry{
			RangeValue: rangeValue,
		})
	}
	return res
}
