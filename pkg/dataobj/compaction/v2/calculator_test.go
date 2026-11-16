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

// testSection returns a section of the object at path, which is a unit of its
// own.
func testSection(path string, index int64, minKey, maxKey testKey) Section[testKey] {
	return Section[testKey]{
		Ref:          &compactionv2pb.SectionRef{ObjectPath: path, SectionIndex: index},
		Min:          minKey,
		Max:          maxKey,
		PlanningUnit: path,
	}
}

// unitSection returns a section of the object at path in the unit named unit.
func unitSection(unit, path string, index int64, minKey, maxKey testKey) Section[testKey] {
	section := testSection(path, index, minKey, maxKey)
	section.PlanningUnit = unit
	return section
}

func key(label string, timestamp int64) testKey {
	return testKey{labels: []string{label}, timestamp: timestamp}
}

func TestGroupSectionsByUnit(t *testing.T) {
	t.Run("groups the sections of each object in section index order", func(t *testing.T) {
		sections := []Section[testKey]{
			testSection("b", 1, key("h", 0), key("i", 0)),
			testSection("a", 2, key("e", 0), key("f", 0)),
			testSection("a", 0, key("c", 0), key("d", 0)),
			testSection("b", 0, key("g", 0), key("h", 0)),
			testSection("a", 1, key("a", 0), key("z", 0)),
		}
		original := append([]Section[testKey](nil), sections...)

		got := groupSectionsByUnit(sections, compareTestKey)

		require.Equal(t, []unit[testKey]{
			{
				name:     "a",
				sections: []*compactionv2pb.SectionRef{sections[2].Ref, sections[4].Ref, sections[1].Ref},
				min:      key("a", 0),
				max:      key("z", 0),
			},
			{
				name:     "b",
				sections: []*compactionv2pb.SectionRef{sections[3].Ref, sections[0].Ref},
				min:      key("g", 0),
				max:      key("i", 0),
			},
		}, got)
		require.Equal(t, original, sections, "grouping must not mutate the input")
	})

	t.Run("orders units with equal lower bounds by upper bound, then by name", func(t *testing.T) {
		sections := []Section[testKey]{
			testSection("z", 0, key("a", 0), key("c", 0)),
			testSection("b", 0, key("a", 0), key("b", 0)),
			testSection("a", 0, key("a", 0), key("b", 0)),
		}

		got := groupSectionsByUnit(sections, compareTestKey)

		require.Equal(t, []string{"a", "b", "z"}, []string{got[0].name, got[1].name, got[2].name})
	})

	t.Run("returns no units for no sections", func(t *testing.T) {
		require.Empty(t, groupSectionsByUnit([]Section[int](nil), cmp.Compare[int]))
	})

	t.Run("panics when a section has no reference", func(t *testing.T) {
		require.PanicsWithValue(t, "nil section reference", func() {
			groupSectionsByUnit([]Section[int]{{Min: 1, Max: 2}}, cmp.Compare[int])
		})
	})

	t.Run("panics when a section has no unit", func(t *testing.T) {
		require.PanicsWithValue(t, "section has no unit", func() {
			groupSectionsByUnit([]Section[int]{{Ref: &compactionv2pb.SectionRef{ObjectPath: "a"}, Min: 1, Max: 2}}, cmp.Compare[int])
		})
	})

	t.Run("groups the objects of one unit into one unit in key order", func(t *testing.T) {
		sections := []Section[testKey]{
			unitSection("run", "run/index-1", 0, key("m", 0), key("z", 0)),
			testSection("plain", 0, key("c", 0), key("d", 0)),
			unitSection("run", "run/index-0", 1, key("e", 0), key("f", 0)),
			unitSection("run", "run/index-0", 0, key("a", 0), key("b", 0)),
		}

		got := groupSectionsByUnit(sections, compareTestKey)

		require.Equal(t, []unit[testKey]{
			{
				name:     "run",
				sections: []*compactionv2pb.SectionRef{sections[3].Ref, sections[2].Ref, sections[0].Ref},
				min:      key("a", 0),
				max:      key("z", 0),
			},
			{
				name:     "plain",
				sections: []*compactionv2pb.SectionRef{sections[1].Ref},
				min:      key("c", 0),
				max:      key("d", 0),
			},
		}, got)
	})
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
				testSection("a", 2, key("e", 0), key("f", 0)),
				testSection("b", 0, key("a", 0), key("b", 0)),
				testSection("c", 1, key("c", 0), key("d", 0)),
			},
			want: [][]int64{{0, 1, 2}},
		},
		{
			name: "touching",
			sections: []Section[testKey]{
				testSection("a", 0, key("a", 0), key("b", 0)),
				testSection("b", 1, key("b", 0), key("c", 0)),
			},
			want: [][]int64{{0}, {1}},
		},
		{
			name: "overlapping",
			sections: []Section[testKey]{
				testSection("a", 0, key("a", 0), key("d", 0)),
				testSection("b", 1, key("b", 0), key("e", 0)),
				testSection("c", 2, key("c", 0), key("f", 0)),
			},
			want: [][]int64{{0}, {1}, {2}},
		},
		{
			name: "timestamp breaks label tie",
			sections: []Section[testKey]{
				testSection("b", 1, key("api", 30), key("api", 40)),
				testSection("a", 0, key("api", 10), key("api", 20)),
			},
			want: [][]int64{{0, 1}},
		},
		{
			name: "keeps the objects of one unit in one run when another object lies between them",
			sections: []Section[testKey]{
				unitSection("run", "run/index-0", 0, key("a", 0), key("b", 0)),
				unitSection("run", "run/index-1", 1, key("y", 0), key("z", 0)),
				testSection("plain", 2, key("m", 0), key("n", 0)),
			},
			want: [][]int64{{0, 1}, {2}},
		},
		{
			name: "best fit",
			sections: []Section[testKey]{
				testSection("a", 0, key("00", 0), key("05", 0)),
				testSection("b", 1, key("01", 0), key("10", 0)),
				testSection("c", 2, key("12", 0), key("20", 0)),
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
	require.Equal(t, [][]string{{"a#0", "a#1"}, {"b#0"}}, runRefs(runs))
}

func TestObjectEnvelopeControlsRunsAndConvergence(t *testing.T) {
	sections := []Section[testKey]{
		testSection("b", 0, key("c", 0), key("f", 0)),
		testSection("a", 1, key("d", 0), key("e", 0)),
		testSection("a", 0, key("a", 0), key("b", 0)),
	}

	runs := CalculateRuns(sections, compareTestKey)

	require.Equal(t, [][]string{{"a#0", "a#1"}, {"b#0"}}, runRefs(runs))
	require.False(t, IsConverged(sections, compareTestKey))
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
				testSection("a", 0, key("a", 0), key("b", 0)),
				testSection("b", 0, key("c", 0), key("d", 0)),
			},
			converged: true,
		},
		{
			name: "touching only",
			sections: []Section[testKey]{
				testSection("a", 0, key("a", 0), key("b", 0)),
				testSection("b", 0, key("b", 0), key("c", 0)),
			},
			converged: true,
		},
		{
			name: "reports converged for one unit whose objects do not overlap",
			sections: []Section[testKey]{
				unitSection("run", "run/index-0", 0, key("a", 0), key("m", 0)),
				unitSection("run", "run/index-1", 0, key("n", 0), key("z", 0)),
			},
			converged: true,
		},
		{
			name: "reports not converged when an object lies inside the bounds of a unit",
			sections: []Section[testKey]{
				unitSection("run", "run/index-0", 0, key("a", 0), key("b", 0)),
				unitSection("run", "run/index-1", 0, key("y", 0), key("z", 0)),
				testSection("plain", 0, key("m", 0), key("n", 0)),
			},
		},
		{
			name: "reports converged for one unit whose objects overlap, because planning sees only its bounds",
			sections: []Section[testKey]{
				unitSection("run", "run/index-0", 0, key("a", 0), key("n", 0)),
				unitSection("run", "run/index-1", 0, key("m", 0), key("z", 0)),
			},
			converged: true,
		},
		{
			name: "true overlap",
			sections: []Section[testKey]{
				testSection("a", 0, key("a", 0), key("c", 0)),
				testSection("b", 0, key("b", 0), key("d", 0)),
			},
		},
		{
			// This test breaks the assumption that sections within an object are sorted.
			// It's included to show that the algorithm does not check for overlap within an object.
			name: "overlap within one object is not checked",
			sections: []Section[testKey]{
				testSection("a", 0, key("a", 0), key("c", 0)),
				testSection("a", 1, key("b", 0), key("d", 0)),
			},
			converged: true,
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
