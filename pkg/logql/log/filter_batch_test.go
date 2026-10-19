package log

import (
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterBatch(t *testing.T) {
	ipFilter, err := NewIPLineFilter("192.168.0.0/16", LineMatchEqual)
	require.NoError(t, err)

	cases := []struct {
		name   string
		filter Filterer
		lines  []string
		want   []int32
	}{
		{
			name:   "containsFilter should return the lines that hold the literal at the start, in the middle and at the end",
			filter: mustNewMatcherFilter(t, "error", LineMatchEqual),
			lines:  []string{"error first", "a error b", "last error", "fine"},
			want:   []int32{0, 1, 2},
		},
		{
			name:   "containsFilter should return a line once when the literal appears twice in it",
			filter: mustNewMatcherFilter(t, "error", LineMatchEqual),
			lines:  []string{"error error", "fine", "error"},
			want:   []int32{0, 2},
		},
		{
			name:   "containsFilter should return no line when the literal is longer than every line",
			filter: mustNewMatcherFilter(t, "abcd", LineMatchEqual),
			lines:  []string{"ab", "abc", ""},
			want:   nil,
		},
		{
			name:   "containsFilter should skip a literal that spans two adjacent lines",
			filter: mustNewMatcherFilter(t, "error", LineMatchEqual),
			lines:  []string{"xxer", "rorxx"},
			want:   nil,
		},
		{
			name:   "containsFilter should find a literal in a line after one that spans two adjacent lines",
			filter: mustNewMatcherFilter(t, "error", LineMatchEqual),
			lines:  []string{"xxer", "rorxx error"},
			want:   []int32{1},
		},
		{
			name:   "containsFilter should find a literal that starts at the second byte of one that spans two adjacent lines",
			filter: mustNewMatcherFilter(t, "aaa", LineMatchEqual),
			lines:  []string{"xa", "aaa"},
			want:   []int32{1},
		},
		{
			name:   "containsFilter should treat an empty line as holding no literal",
			filter: mustNewMatcherFilter(t, "error", LineMatchEqual),
			lines:  []string{"", "error", ""},
			want:   []int32{1},
		},
		{
			name:   "notFilter should pass an empty line through a negated literal",
			filter: mustNewFilter(t, "error", LineMatchNotEqual),
			lines:  []string{"", "error", "fine"},
			want:   []int32{0, 2},
		},
		{
			name:   "containsFilter should return every line for an empty literal",
			filter: &containsFilter{},
			lines:  []string{"", "a", "bc"},
			want:   []int32{0, 1, 2},
		},
		{
			name:   "trueFilter should return every line",
			filter: TrueFilter,
			lines:  []string{"", "a", "bc"},
			want:   []int32{0, 1, 2},
		},
		{
			name:   "containsFilter should return no line for an empty batch",
			filter: mustNewMatcherFilter(t, "error", LineMatchEqual),
			lines:  nil,
			want:   nil,
		},
		{
			name:   "containsFilter should test each line for a case-insensitive literal",
			filter: mustNewFilter(t, "(?i)error", LineMatchRegexp),
			lines:  []string{"ERROR", "error", "Err", "xErRoR", "fine"},
			want:   []int32{0, 1, 3},
		},
		{
			name: "containsAllFilter should return the lines that hold every literal",
			filter: NewAndFilters([]Filterer{
				mustNewMatcherFilter(t, "a", LineMatchEqual),
				mustNewMatcherFilter(t, "b", LineMatchEqual),
				mustNewMatcherFilter(t, "c", LineMatchEqual),
			}),
			lines: []string{"abc", "ab", "cba", "c", "bca"},
			want:  []int32{0, 2, 4},
		},
		{
			name: "andFilters should return the lines that pass a literal and a regexp",
			filter: NewAndFilters([]Filterer{
				mustNewMatcherFilter(t, "status", LineMatchEqual),
				mustNewFilter(t, "status=[45][0-9]+", LineMatchRegexp),
			}),
			lines: []string{"status=200", "status=404", "code=404", "status=500 status=200"},
			want:  []int32{1, 3},
		},
		{
			name: "andFilter should return the lines that pass a literal and a negated literal",
			filter: NewAndFilter(
				mustNewMatcherFilter(t, "a", LineMatchEqual),
				NewNotFilter(mustNewMatcherFilter(t, "b", LineMatchEqual)),
			),
			lines: []string{"a", "ab", "b", "ca", ""},
			want:  []int32{0, 3},
		},
		{
			name: "orFilter should return the lines that pass either literal",
			filter: newOrFilter(
				mustNewMatcherFilter(t, "a", LineMatchEqual),
				mustNewMatcherFilter(t, "b", LineMatchEqual),
			),
			lines: []string{"a", "b", "ab", "c", ""},
			want:  []int32{0, 1, 2},
		},
		{
			name: "andFilter should return the lines of a negated or",
			filter: NewNotFilter(newOrFilter(
				mustNewMatcherFilter(t, "a", LineMatchEqual),
				mustNewMatcherFilter(t, "b", LineMatchEqual),
			)),
			lines: []string{"a", "b", "ab", "c", ""},
			want:  []int32{3, 4},
		},
		{
			name: "andFilter should return the lines of an or inside an and",
			filter: NewAndFilter(
				newOrFilter(
					mustNewMatcherFilter(t, "a", LineMatchEqual),
					mustNewMatcherFilter(t, "b", LineMatchEqual),
				),
				mustNewMatcherFilter(t, "x", LineMatchEqual),
			),
			lines: []string{"ax", "bx", "x", "ab", "abx", "cx"},
			want:  []int32{0, 1, 4},
		},
		{
			name: "orFilter should return the lines of an and inside an or",
			filter: newOrFilter(
				NewAndFilter(
					mustNewMatcherFilter(t, "a", LineMatchEqual),
					mustNewMatcherFilter(t, "b", LineMatchEqual),
				),
				mustNewMatcherFilter(t, "x", LineMatchEqual),
			),
			lines: []string{"ab", "a", "x", "b", "axb"},
			want:  []int32{0, 2, 4},
		},
		{
			name:   "regexpFilter should return the lines that match the regexp",
			filter: mustNewFilter(t, "e.*r+o", LineMatchRegexp),
			lines:  []string{"error", "eaaro", "fine"},
			want:   []int32{0, 1},
		},
		{
			name:   "patternFilter should return the lines that match the pattern",
			filter: mustNewFilter(t, "level=<_> msg", LineMatchPattern),
			lines:  []string{"level=info msg", "level= msg", "msg"},
			want:   []int32{0},
		},
		{
			name:   "IPLineFilter should return the lines that hold an address in the range",
			filter: ipFilter,
			lines:  []string{"from 192.168.1.5", "from 10.0.0.1", "none"},
			want:   []int32{0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batch := newTestLinesBatch(tc.lines...)
			require.Equal(t, tc.want, filterEachLine(tc.filter, batch), "the expected lines must agree with Filter")

			got := FilterBatch(tc.filter, batch, nil)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("FilterBatch should find the lines of a batch that does not start at offset zero", func(t *testing.T) {
		batch := LinesBatch{
			Data:    []byte("errorokerrorokerror"),
			Offsets: []int32{5, 7, 12, 14},
		}
		filter := mustNewMatcherFilter(t, "error", LineMatchEqual)
		require.Equal(t, []int32{1}, FilterBatch(filter, batch, nil))
	})

	t.Run("FilterBatch should find the lines of a batch that is longer than the shared list of indexes", func(t *testing.T) {
		var (
			rows     = make([]string, 3*len(defaultSequentialIndexes)+7)
			want     []int32
			wantBoth []int32
		)
		for i := range rows {
			rows[i] = "plain"
			if i%7 == 0 {
				rows[i] = "needle"
				want = append(want, int32(i))
			}
			if i%7 == 0 && i%2 == 0 {
				rows[i] = "needle even"
				wantBoth = append(wantBoth, int32(i))
			}
		}
		batch := newTestLinesBatch(rows...)

		require.Equal(t, want, FilterBatch(mustNewMatcherFilter(t, "needle", LineMatchEqual), batch, nil))
		both := NewAndFilter(
			mustNewMatcherFilter(t, "needle", LineMatchEqual),
			mustNewMatcherFilter(t, "even", LineMatchEqual),
		)
		require.Equal(t, wantBoth, FilterBatch(both, batch, nil))
	})

	t.Run("FilterBatch should append to the given dstIndexes", func(t *testing.T) {
		batch := newTestLinesBatch("a", "b", "ab")

		got := FilterBatch(mustNewMatcherFilter(t, "a", LineMatchEqual), batch, []int32{99})
		require.Equal(t, []int32{99, 0, 2}, got)

		both := NewAndFilter(
			mustNewMatcherFilter(t, "a", LineMatchEqual),
			mustNewMatcherFilter(t, "b", LineMatchEqual),
		)
		got = FilterBatch(both, batch, []int32{99})
		require.Equal(t, []int32{99, 2}, got)
	})

	t.Run("FilterBatch should allocate nothing for a literal and an and in steady state", func(t *testing.T) {
		batch := newTestLinesBatch("error a", "b", "error error", "c", "a")
		filters := map[string]Filterer{
			"literal": mustNewMatcherFilter(t, "error", LineMatchEqual),
			"and": NewAndFilter(
				mustNewMatcherFilter(t, "error", LineMatchEqual),
				mustNewMatcherFilter(t, "a", LineMatchEqual),
			),
		}
		for name, f := range filters {
			dst := FilterBatch(f, batch, nil)
			allocs := testing.AllocsPerRun(100, func() {
				dst = FilterBatch(f, batch, dst[:0])
			})
			require.Zero(t, allocs, name)
		}
	})

	t.Run("FilterBatch should match Filter on random lines for every filter shape", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		const alphabet = "abA "

		randomString := func(maxLen int) string {
			var sb strings.Builder
			for range rnd.Intn(maxLen + 1) {
				sb.WriteByte(alphabet[rnd.Intn(len(alphabet))])
			}
			return sb.String()
		}
		randomLiteral := func() MatcherFilterer {
			s := randomString(3)
			if s == "" {
				s = "a"
			}
			if rnd.Intn(5) == 0 {
				return mustNewMatcherFilter(t, "(?i)"+s, LineMatchRegexp)
			}
			return mustNewMatcherFilter(t, s, LineMatchEqual)
		}
		regexp := mustNewFilter(t, "a.b", LineMatchRegexp)
		randomFilter := func() Filterer {
			switch rnd.Intn(10) {
			case 0:
				return randomLiteral()
			case 1:
				return NewAndFilters([]Filterer{randomLiteral(), randomLiteral(), randomLiteral()})
			case 2:
				return NewAndFilter(randomLiteral(), randomLiteral())
			case 3:
				return newOrFilter(randomLiteral(), randomLiteral())
			case 4:
				return NewNotFilter(randomLiteral())
			case 5:
				return NewAndFilter(newOrFilter(randomLiteral(), NewNotFilter(randomLiteral())), randomLiteral())
			case 6:
				return NewAndFilters([]Filterer{randomLiteral(), newOrFilter(randomLiteral(), randomLiteral()), NewNotFilter(randomLiteral())})
			case 7:
				return NewAndFilter(randomLiteral(), newOrFilter(randomLiteral(), NewNotFilter(randomLiteral())))
			case 8:
				return NewAndFilters([]Filterer{randomLiteral(), regexp, NewNotFilter(randomLiteral())})
			default:
				return newOrFilter(NewAndFilter(randomLiteral(), randomLiteral()), NewNotFilter(randomLiteral()))
			}
		}

		for trial := range 5000 {
			rows := make([]string, rnd.Intn(40))
			for i := range rows {
				rows[i] = randomString(8)
			}
			batch := newTestLinesBatch(rows...)

			// Drop the first offsets, so Offsets does not start at 0 and Data holds bytes before the
			// batch.
			if len(rows) > 2 {
				first := rnd.Intn(len(rows) / 2)
				batch.Offsets = batch.Offsets[first:]
			}

			f := randomFilter()
			want := filterEachLine(f, batch)
			got := FilterBatch(f, batch, []int32{99, 98})
			require.Equal(t, []int32{99, 98}, got[:2], "trial %d: the prefix of dst changed", trial)
			require.True(t, slices.Equal(want, got[2:]), "trial %d, rows %q, want %v, got %v", trial, rows, want, got[2:])
		}
	})
}

func newTestLinesBatch(lines ...string) LinesBatch {
	var (
		data    []byte
		offsets = []int32{0}
	)
	for _, line := range lines {
		data = append(data, line...)
		offsets = append(offsets, int32(len(data)))
	}
	return LinesBatch{Data: data, Offsets: offsets}
}

func filterEachLine(f Filterer, batch LinesBatch) []int32 {
	var out []int32
	for i := range batch.count() {
		if f.Filter(batch.line(int32(i))) {
			out = append(out, int32(i))
		}
	}
	return out
}

func mustNewFilter(t *testing.T, match string, mt LineMatchType) Filterer {
	t.Helper()
	f, err := NewFilter(match, mt)
	require.NoError(t, err)
	return f
}

// mustNewMatcherFilter is mustNewFilter for the filters that the combinators accept.
func mustNewMatcherFilter(t *testing.T, match string, mt LineMatchType) MatcherFilterer {
	t.Helper()
	f := mustNewFilter(t, match, mt)
	m, ok := f.(MatcherFilterer)
	require.True(t, ok, "%T is not a MatcherFilterer", f)
	return m
}
