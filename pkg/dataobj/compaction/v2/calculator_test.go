package compactionv2

import (
	"cmp"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

type testKey struct {
	labels    []string
	timestamp int64
}

func compareTestKey(a, b testKey) int {
	if n := slices.Compare(a.labels, b.labels); n != 0 {
		return n
	}
	return cmp.Compare(a.timestamp, b.timestamp)
}

func testSection(path string, index int64, minKey, maxKey testKey) Section[testKey] {
	return Section[testKey]{
		Ref: &compactionv2pb.SectionRef{ObjectPath: path, SectionIndex: index},
		Min: minKey,
		Max: maxKey,
	}
}

func key(label string, timestamp int64) testKey {
	return testKey{labels: []string{label}, timestamp: timestamp}
}

func TestCalculateRuns(t *testing.T) {
	tests := []struct {
		name     string
		sections []Section[testKey]
		want     [][]int64
	}{
		{
			name: "empty",
		},
		{
			name: "non-overlapping",
			sections: []Section[testKey]{
				testSection("o", 2, key("e", 0), key("f", 0)),
				testSection("o", 0, key("a", 0), key("b", 0)),
				testSection("o", 1, key("c", 0), key("d", 0)),
			},
			want: [][]int64{{0, 1, 2}},
		},
		{
			name: "touching",
			sections: []Section[testKey]{
				testSection("o", 0, key("a", 0), key("b", 0)),
				testSection("o", 1, key("b", 0), key("c", 0)),
			},
			want: [][]int64{{0}, {1}},
		},
		{
			name: "overlapping",
			sections: []Section[testKey]{
				testSection("o", 0, key("a", 0), key("d", 0)),
				testSection("o", 1, key("b", 0), key("e", 0)),
				testSection("o", 2, key("c", 0), key("f", 0)),
			},
			want: [][]int64{{0}, {1}, {2}},
		},
		{
			name: "timestamp breaks label tie",
			sections: []Section[testKey]{
				testSection("o", 1, key("api", 30), key("api", 40)),
				testSection("o", 0, key("api", 10), key("api", 20)),
			},
			want: [][]int64{{0, 1}},
		},
		{
			name: "best fit",
			sections: []Section[testKey]{
				testSection("o", 0, key("00", 0), key("05", 0)),
				testSection("o", 1, key("01", 0), key("10", 0)),
				testSection("o", 2, key("12", 0), key("20", 0)),
			},
			want: [][]int64{{0}, {1, 2}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runs := CalculateRuns(test.sections, compareTestKey)
			var got [][]int64
			for _, run := range runs {
				var indexes []int64
				for _, section := range run.Sections() {
					indexes = append(indexes, section.SectionIndex)
				}
				got = append(got, indexes)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func TestCalculateRuns_NilRefPanics(t *testing.T) {
	require.PanicsWithValue(t, "nil section reference", func() {
		CalculateRuns([]Section[int]{{Min: 1, Max: 2}}, cmp.Compare[int])
	})
}

func TestCalculateRuns_Deterministic(t *testing.T) {
	sections := []Section[testKey]{
		testSection("a", 0, key("a", 0), key("d", 0)),
		testSection("b", 0, key("b", 0), key("e", 0)),
		testSection("c", 0, key("f", 0), key("g", 0)),
		testSection("d", 0, key("c", 0), key("h", 0)),
	}
	want := CalculateRuns(append([]Section[testKey](nil), sections...), compareTestKey)

	random := rand.New(rand.NewSource(42))
	for range 10 {
		shuffled := append([]Section[testKey](nil), sections...)
		random.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got := CalculateRuns(shuffled, compareTestKey)
		require.Equal(t, runRefs(want), runRefs(got))
	}
}

func TestCalculateRuns_TiedBoundsUseReferenceOrder(t *testing.T) {
	sections := []Section[testKey]{
		testSection("b", 0, key("a", 0), key("b", 0)),
		testSection("a", 1, key("a", 0), key("b", 0)),
		testSection("a", 0, key("a", 0), key("b", 0)),
	}

	runs := CalculateRuns(sections, compareTestKey)
	require.Equal(t, [][]string{{"a#0"}, {"a#1"}, {"b#0"}}, runRefs(runs))
}

func TestIsConverged(t *testing.T) {
	tests := []struct {
		name      string
		sections  []Section[testKey]
		converged bool
	}{
		{name: "empty", converged: true},
		{
			name: "strict run",
			sections: []Section[testKey]{
				testSection("o", 0, key("a", 0), key("b", 0)),
				testSection("o", 1, key("c", 0), key("d", 0)),
			},
			converged: true,
		},
		{
			name: "touching only",
			sections: []Section[testKey]{
				testSection("o", 0, key("a", 0), key("b", 0)),
				testSection("o", 1, key("b", 0), key("c", 0)),
			},
			converged: true,
		},
		{
			name: "true overlap",
			sections: []Section[testKey]{
				testSection("o", 0, key("a", 0), key("c", 0)),
				testSection("o", 1, key("b", 0), key("d", 0)),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := append([]Section[testKey](nil), test.sections...)
			require.Equal(t, test.converged, IsConverged(test.sections, compareTestKey))
			require.Equal(t, original, test.sections)
		})
	}
}

func runRefs(runs []Run) [][]string {
	refs := make([][]string, len(runs))
	for i, run := range runs {
		for _, section := range run.Sections() {
			refs[i] = append(refs[i], fmt.Sprintf("%s#%d", section.ObjectPath, section.SectionIndex))
		}
	}
	return refs
}
