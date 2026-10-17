package log

import (
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestLines(lines ...string) Lines {
	var (
		data    []byte
		offsets = []int32{0}
	)
	for _, line := range lines {
		data = append(data, line...)
		offsets = append(offsets, int32(len(data)))
	}
	return Lines{Data: data, Offsets: offsets}
}

func filterEachLine(f Filterer, lines Lines) []int32 {
	var out []int32
	for i := range lines.count() {
		if f.Filter(lines.line(int32(i))) {
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

func contain(match string) MatcherFilterer {
	return newContainsFilter([]byte(match), false)
}

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
			name:   "returns the lines that hold the literal at the start, in the middle and at the end",
			filter: contain("error"),
			lines:  []string{"error first", "a error b", "last error", "fine"},
			want:   []int32{0, 1, 2},
		},
		{
			name:   "returns a line once when the literal appears twice in it",
			filter: contain("error"),
			lines:  []string{"error error", "fine", "error"},
			want:   []int32{0, 2},
		},
		{
			name:   "returns no line when the literal is longer than every line",
			filter: contain("abcd"),
			lines:  []string{"ab", "abc", ""},
			want:   nil,
		},
		{
			name:   "skips a literal that spans two adjacent lines",
			filter: contain("error"),
			lines:  []string{"xxer", "rorxx"},
			want:   nil,
		},
		{
			name:   "finds a literal in a line after one that spans two adjacent lines",
			filter: contain("error"),
			lines:  []string{"xxer", "rorxx error"},
			want:   []int32{1},
		},
		{
			name:   "finds a literal that starts at the second byte of one that spans two adjacent lines",
			filter: contain("aaa"),
			lines:  []string{"xa", "aaa"},
			want:   []int32{1},
		},
		{
			name:   "treats an empty line as holding no literal",
			filter: contain("error"),
			lines:  []string{"", "error", ""},
			want:   []int32{1},
		},
		{
			name:   "passes an empty line through a negated literal",
			filter: mustNewFilter(t, "error", LineMatchNotEqual),
			lines:  []string{"", "error", "fine"},
			want:   []int32{0, 2},
		},
		{
			name:   "returns every line for an empty literal",
			filter: &containsFilter{},
			lines:  []string{"", "a", "bc"},
			want:   []int32{0, 1, 2},
		},
		{
			name:   "returns every line for a filter that keeps every line",
			filter: TrueFilter,
			lines:  []string{"", "a", "bc"},
			want:   []int32{0, 1, 2},
		},
		{
			name:   "returns no line for an empty batch",
			filter: contain("error"),
			lines:  nil,
			want:   nil,
		},
		{
			name:   "tests each line for a case-insensitive literal",
			filter: newContainsFilter([]byte("error"), true),
			lines:  []string{"ERROR", "error", "Err", "xErRoR", "fine"},
			want:   []int32{0, 1, 3},
		},
		{
			name:   "returns the lines that hold every literal of an and",
			filter: NewAndFilters([]Filterer{contain("a"), contain("b"), contain("c")}),
			lines:  []string{"abc", "ab", "cba", "c", "bca"},
			want:   []int32{0, 2, 4},
		},
		{
			name:   "returns the lines of an and of a literal and a regexp",
			filter: NewAndFilters([]Filterer{contain("status"), mustNewFilter(t, "status=[45][0-9]+", LineMatchRegexp)}),
			lines:  []string{"status=200", "status=404", "code=404", "status=500 status=200"},
			want:   []int32{1, 3},
		},
		{
			name:   "returns the lines of an and of two filters",
			filter: NewAndFilter(contain("a"), NewNotFilter(contain("b"))),
			lines:  []string{"a", "ab", "b", "ca", ""},
			want:   []int32{0, 3},
		},
		{
			name:   "returns the lines of an or",
			filter: newOrFilter(contain("a"), contain("b")),
			lines:  []string{"a", "b", "ab", "c", ""},
			want:   []int32{0, 1, 2},
		},
		{
			name:   "returns the lines of a negated or",
			filter: NewNotFilter(newOrFilter(contain("a"), contain("b"))),
			lines:  []string{"a", "b", "ab", "c", ""},
			want:   []int32{3, 4},
		},
		{
			name:   "returns the lines of an or inside an and",
			filter: NewAndFilter(newOrFilter(contain("a"), contain("b")), contain("x")),
			lines:  []string{"ax", "bx", "x", "ab", "abx", "cx"},
			want:   []int32{0, 1, 4},
		},
		{
			name:   "returns the lines of an and inside an or",
			filter: newOrFilter(NewAndFilter(contain("a"), contain("b")), contain("x")),
			lines:  []string{"ab", "a", "x", "b", "axb"},
			want:   []int32{0, 2, 4},
		},
		{
			name:   "returns the lines of a regexp filter",
			filter: mustNewFilter(t, "e.*r+o", LineMatchRegexp),
			lines:  []string{"error", "eaaro", "fine"},
			want:   []int32{0, 1},
		},
		{
			name:   "returns the lines of a pattern filter",
			filter: mustNewFilter(t, "level=<_> msg", LineMatchPattern),
			lines:  []string{"level=info msg", "level= msg", "msg"},
			want:   []int32{0},
		},
		{
			name:   "returns the lines of an ip filter",
			filter: ipFilter,
			lines:  []string{"from 192.168.1.5", "from 10.0.0.1", "none"},
			want:   []int32{0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := newTestLines(tc.lines...)
			require.Equal(t, tc.want, filterEachLine(tc.filter, lines), "the expected lines must agree with Filter")

			got := FilterBatch(tc.filter, nil, lines)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("finds the lines of a batch that does not start at offset zero", func(t *testing.T) {
		lines := Lines{
			Data:    []byte("errorokerrorokerror"),
			Offsets: []int32{5, 7, 12, 14},
		}
		require.Equal(t, []int32{1}, FilterBatch(contain("error"), nil, lines))
	})

	t.Run("finds the lines of a batch that is longer than the shared list of indexes", func(t *testing.T) {
		var (
			rows     = make([]string, 3*len(allLines)+7)
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
		lines := newTestLines(rows...)

		require.Equal(t, want, FilterBatch(contain("needle"), nil, lines))
		require.Equal(t, wantBoth, FilterBatch(NewAndFilter(contain("needle"), contain("even")), nil, lines))
	})

	t.Run("appends to the given dst", func(t *testing.T) {
		got := FilterBatch(contain("a"), []int32{99}, newTestLines("a", "b", "ab"))
		require.Equal(t, []int32{99, 0, 2}, got)

		got = FilterBatch(NewAndFilter(contain("a"), contain("b")), []int32{99}, newTestLines("a", "b", "ab"))
		require.Equal(t, []int32{99, 2}, got)
	})

	t.Run("allocates nothing for a literal and an and in steady state", func(t *testing.T) {
		lines := newTestLines("error a", "b", "error error", "c", "a")
		filters := map[string]Filterer{
			"literal": contain("error"),
			"and":     NewAndFilter(contain("error"), contain("a")),
		}
		for name, f := range filters {
			dst := FilterBatch(f, nil, lines)
			allocs := testing.AllocsPerRun(100, func() {
				dst = FilterBatch(f, dst[:0], lines)
			})
			require.Zero(t, allocs, name)
		}
	})

	t.Run("matches Filter on random lines for every filter shape", func(t *testing.T) {
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
			return newContainsFilter([]byte(s), rnd.Intn(5) == 0)
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
			lines := newTestLines(rows...)

			// Drop the first offsets, so Offsets does not start at 0 and Data holds bytes before the
			// batch.
			if len(rows) > 2 {
				first := rnd.Intn(len(rows) / 2)
				lines.Offsets = lines.Offsets[first:]
			}

			f := randomFilter()
			want := filterEachLine(f, lines)
			got := FilterBatch(f, []int32{99, 98}, lines)
			require.Equal(t, []int32{99, 98}, got[:2], "trial %d: the prefix of dst changed", trial)
			require.True(t, slices.Equal(want, got[2:]), "trial %d, rows %q, want %v, got %v", trial, rows, want, got[2:])
		}
	})
}
