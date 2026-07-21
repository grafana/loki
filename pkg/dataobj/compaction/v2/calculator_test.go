package compactionv2

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// Suppress unused import warning; math/rand is used in the shuffle determinism tests.
var _ = rand.Float64

// sec is a small constructor for SectionRef test fixtures with single-column
// MinKey/MaxKey values (the common case in existing tests).
func sec(path string, idx int64, minKey, maxKey string) *compactionv2pb.SectionRef {
	return &compactionv2pb.SectionRef{
		ObjectPath:   path,
		SectionIndex: idx,
		MinKey:       []string{minKey},
		MaxKey:       []string{maxKey},
	}
}

// secT is a constructor for SectionRef test fixtures with multi-column
// MinKey/MaxKey tuples (for tests that exercise multi-column sort_schema
// semantics).
func secT(path string, idx int64, minKey, maxKey []string) *compactionv2pb.SectionRef {
	return &compactionv2pb.SectionRef{
		ObjectPath:   path,
		SectionIndex: idx,
		MinKey:       minKey,
		MaxKey:       maxKey,
	}
}

func TestPatienceSort_Empty(t *testing.T) {
	require.Nil(t, calculateRuns(nil))
	require.Nil(t, calculateRuns([]*compactionv2pb.SectionRef{}))
}

func TestPatienceSort_SingleSection(t *testing.T) {
	s := sec("obj-1", 0, "a", "b")
	got := calculateRuns([]*compactionv2pb.SectionRef{s})

	require.Len(t, got, 1)
	require.Equal(t, []*compactionv2pb.SectionRef{s}, got[0].sections)
	require.Equal(t, []string{"b"}, got[0].topMaxKey)
}

func TestPatienceSort_AllNonOverlapping(t *testing.T) {
	s1 := sec("o", 0, "a", "b")
	s2 := sec("o", 1, "c", "d")
	s3 := sec("o", 2, "e", "f")

	got := calculateRuns([]*compactionv2pb.SectionRef{s1, s2, s3})

	require.Len(t, got, 1, "all non-overlapping sections should form exactly one pile")
	require.Equal(t, []*compactionv2pb.SectionRef{s1, s2, s3}, got[0].sections)
	require.Equal(t, []string{"f"}, got[0].topMaxKey)
}

func TestPatienceSort_AllOverlapping(t *testing.T) {
	// All three sections overlap pairwise; expect 3 piles each with 1 section.
	s1 := sec("o", 0, "a", "m")
	s2 := sec("o", 1, "b", "n")
	s3 := sec("o", 2, "c", "o")

	got := calculateRuns([]*compactionv2pb.SectionRef{s1, s2, s3})

	require.Len(t, got, 3)
	for _, p := range got {
		require.Len(t, p.sections, 1, "each pile should hold exactly one overlapping section")
	}
}

func TestPatienceSort_BestFit(t *testing.T) {
	// Seed two piles by feeding two overlapping sections first:
	//   pile0: topMaxKey "05"
	//   pile1: topMaxKey "10"
	// Then a third section with MinKey="12" — best-fit is pile1 (largest topMaxKey < "12").
	s1 := sec("o", 0, "00", "05") // creates pile0
	s2 := sec("o", 1, "01", "10") // overlaps s1; creates pile1
	s3 := sec("o", 2, "12", "20") // MinKey > both top_max_keys; pick pile1

	got := calculateRuns([]*compactionv2pb.SectionRef{s1, s2, s3})

	require.Len(t, got, 2, "expected 2 piles")
	// pile1 (created second) should now contain s2 and s3.
	require.Equal(t, []*compactionv2pb.SectionRef{s2, s3}, got[1].sections)
	require.Equal(t, []string{"20"}, got[1].topMaxKey)
	// pile0 should still hold only s1.
	require.Equal(t, []*compactionv2pb.SectionRef{s1}, got[0].sections)
	require.Equal(t, []string{"05"}, got[0].topMaxKey)
}

func TestPatienceSort_TiebreakerOnCreationOrder(t *testing.T) {
	// Build two piles that both end with topMaxKey == "10", then feed a section
	// whose MinKey ("20") is strictly greater than "10", so both piles are
	// unambiguously eligible regardless of the touching rule. Best-fit ties on
	// topMaxKey, so the OLDEST pile (lowest slice index) must win.
	//   s1 = ("00","10") — creates pile0 (topMaxKey "10")
	//   s2 = ("05","10") — overlaps s1 (pile0 topMaxKey "10" is not <= "05"),
	//                      so it creates pile1 (topMaxKey "10")
	//   s3 = ("20","30") — eligible for pile0 and pile1 (both topMaxKey "10" <=
	//                      "20"); tie on topMaxKey -> append to the oldest, pile0.
	s1 := sec("o", 0, "00", "10")
	s2 := sec("o", 1, "05", "10")
	s3 := sec("o", 2, "20", "30")

	got := calculateRuns([]*compactionv2pb.SectionRef{s1, s2, s3})

	require.Len(t, got, 2)
	require.Equal(t, []*compactionv2pb.SectionRef{s1, s3}, got[0].sections,
		"among piles with equal topMaxKey, append to the OLDEST")
	require.Equal(t, []*compactionv2pb.SectionRef{s2}, got[1].sections,
		"newer pile is NOT chosen on tie")
}

func TestPatienceSort_StableIDTiebreaker(t *testing.T) {
	// Two sections with identical (MinKey, MaxKey) — differentiated only by stable_id.
	// Per step-1 sort: (ObjectPath ASC, SectionIndex ASC) breaks the tie deterministically.
	// Both sections overlap each other (same range) so they end up in separate piles.
	// The pile created first must contain the section with the smaller stable_id.
	a := sec("aaa", 0, "k", "m")
	b := sec("aaa", 1, "k", "m")
	c := sec("bbb", 0, "k", "m")

	// Feed in deliberately scrambled order — output must still be deterministic
	// per the stable_id tiebreaker.
	got := calculateRuns([]*compactionv2pb.SectionRef{c, b, a})

	require.Len(t, got, 3)
	require.Equal(t, a, got[0].sections[0], "pile0 expected to hold stable_id-smallest section")
	require.Equal(t, b, got[1].sections[0])
	require.Equal(t, c, got[2].sections[0])
}

func TestPatienceSort_Determinism_Shuffled(t *testing.T) {
	// Build a non-trivial input — a few overlapping and non-overlapping sections.
	base := []*compactionv2pb.SectionRef{
		sec("o", 0, "01", "03"),
		sec("o", 1, "02", "04"),
		sec("o", 2, "05", "07"),
		sec("o", 3, "06", "10"),
		sec("o", 4, "11", "13"),
		sec("o", 5, "12", "14"),
		sec("o", 6, "15", "18"),
		sec("p", 0, "02", "06"),
	}

	// Reference result: feed in input order.
	want := calculateRuns(append([]*compactionv2pb.SectionRef(nil), base...))

	// Try 10 different deterministic shuffles. Use a seeded math/rand so the
	// test is reproducible.
	r := rand.New(rand.NewSource(42))
	for trial := 0; trial < 10; trial++ {
		shuffled := append([]*compactionv2pb.SectionRef(nil), base...)
		r.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got := calculateRuns(shuffled)

		require.Equal(t, len(want), len(got), "trial %d: pile count must match", trial)
		for i := range want {
			require.Equal(t, want[i].sections, got[i].sections, "trial %d: pile %d sections", trial, i)
			require.Equal(t, want[i].topMaxKey, got[i].topMaxKey, "trial %d: pile %d topMaxKey", trial, i)
		}
	}
}

// The following tests pin the multi-column (tuple) compare semantics for
// MinKey/MaxKey. They demonstrate cases that would fail under a naive
// single-string concatenation of column values:
//
// Sort schema = [service_name, env] (two columns). Sections carry MinKey/MaxKey
// as []string{service_name_value, env_value}. The calculator must compare these
// element-wise (column 0 first, column 1 only if column 0 is equal). A
// concatenated-string representation -- say, joined by some delimiter -- gets
// these cases wrong because the delimiter byte intermixes with column-content
// bytes at the boundary.

// TestCalculateRuns_MultiColumn_PrefixOrder demonstrates the prefix case:
// when one section's column-0 value is a strict prefix of another's, the
// prefix is less. Concatenation with any printable separator (e.g. '|' at
// 0x7C) reverses this whenever the next byte in the longer string is less
// than the separator -- e.g. "api|*" vs "apiv2|*" hits '|' vs 'v' (0x7C vs
// 0x76) and orders them backwards.
func TestCalculateRuns_MultiColumn_PrefixOrder(t *testing.T) {
	// Two sections, two columns each.
	//   a: MinKey=("api","zoo")    MaxKey=("api","zzz")
	//   b: MinKey=("apiv2","aaa")  MaxKey=("apiv2","aaa")
	//
	// Tuple compare on MinKey: column 0 "api" vs "apiv2" — prefix is less,
	// so a < b. Column 1 ("zoo" vs "aaa") never inspected.
	//
	// a.MaxKey = ("api","zzz") < b.MinKey = ("apiv2","aaa") at column 0
	// (same prefix logic), so the ranges do NOT overlap: one run with both
	// sections in order.
	a := secT("o", 0, []string{"api", "zoo"}, []string{"api", "zzz"})
	b := secT("o", 1, []string{"apiv2", "aaa"}, []string{"apiv2", "aaa"})

	got := calculateRuns([]*compactionv2pb.SectionRef{a, b})

	require.Len(t, got, 1, "non-overlapping sections should form one run")
	require.Equal(t, []*compactionv2pb.SectionRef{a, b}, got[0].sections,
		"prefix on column 0 must place 'api' before 'apiv2' regardless of column 1")
}

// TestCalculateRuns_MultiColumn_SecondColumnDecides verifies that when
// column 0 values tie, the comparison correctly proceeds to column 1.
// Tests sort ordering AND non-overlapping run packing on column 1.
func TestCalculateRuns_MultiColumn_SecondColumnDecides(t *testing.T) {
	// All three sections share service_name="api"; they only differ on env.
	//   a: ("api","prod")..("api","prod")     — env=prod
	//   b: ("api","qa")..("api","qa")          — env=qa
	//   c: ("api","staging")..("api","staging") — env=staging
	//
	// Lexicographic: prod < qa < staging. All three non-overlapping; one run.
	a := secT("o", 0, []string{"api", "prod"}, []string{"api", "prod"})
	b := secT("o", 1, []string{"api", "qa"}, []string{"api", "qa"})
	c := secT("o", 2, []string{"api", "staging"}, []string{"api", "staging"})

	// Feed in jumbled order; sort step must reorder by tuple compare.
	got := calculateRuns([]*compactionv2pb.SectionRef{c, a, b})

	require.Len(t, got, 1, "non-overlapping sections should form one run")
	require.Equal(t, []*compactionv2pb.SectionRef{a, b, c}, got[0].sections,
		"with equal column 0, column 1 must decide the order (prod < qa < staging)")
}

// TestCalculateRuns_MultiColumn_OverlapAcrossColumns demonstrates that
// overlap detection works correctly across columns. The same input under a
// single-concatenated-string MinKey/MaxKey could mistakenly treat these as
// non-overlapping (and pack them into one run) because the byte order would
// reverse at the column-0/column-1 boundary.
func TestCalculateRuns_MultiColumn_OverlapAcrossColumns(t *testing.T) {
	// a: MinKey=("api","prod")  MaxKey=("apiv2","aaa")
	// b: MinKey=("api","zoo")   MaxKey=("api","zzz")
	//
	// Tuple compare sorts a < b on MinKey (col 0 prefix: "api" < "api"? equal;
	// col 1 "prod" < "zoo"; so a.MinKey < b.MinKey).
	//
	// Does a contain b? a.MaxKey=("apiv2","aaa") vs b.MinKey=("api","zoo"):
	// col 0 "apiv2" vs "api" — "api" is a prefix of "apiv2" → "api" < "apiv2"
	// → a.MaxKey > b.MinKey. So b.MinKey falls INSIDE a's range. They overlap.
	// Expected: 2 runs.
	//
	// Under a naive "join columns with '|'" encoding, a.MaxKey = "apiv2|aaa"
	// vs b.MinKey = "api|zoo" — at position 3, 'v' (0x76) vs '|' (0x7C);
	// 'v' < '|', so "apiv2|aaa" < "api|zoo". The encoder would conclude
	// a.MaxKey < b.MinKey, treat them as non-overlapping, and pack them into
	// a single run. This test pins the correct overlap detection.
	a := secT("o", 0, []string{"api", "prod"}, []string{"apiv2", "aaa"})
	b := secT("o", 1, []string{"api", "zoo"}, []string{"api", "zzz"})

	got := calculateRuns([]*compactionv2pb.SectionRef{a, b})

	require.Len(t, got, 2, "overlapping sections must produce separate runs")
}

// secTS is a constructor for SectionRef test fixtures with explicit
// MinTimestamp/MaxTimestamp values (for tests that exercise the timestamp
// component of the composite sort key). MinKey/MaxKey are still []string
// tuples; pass single-element slices for the single-column case.
func secTS(path string, idx int64, minKey, maxKey []string, minTs, maxTs int64) *compactionv2pb.SectionRef {
	return &compactionv2pb.SectionRef{
		ObjectPath:   path,
		SectionIndex: idx,
		MinKey:       minKey,
		MaxKey:       maxKey,
		MinTimestamp: minTs,
		MaxTimestamp: maxTs,
	}
}

// TestCalculateRuns_Timestamp_NonOverlap demonstrates that when two sections
// share the same label tuple but have non-overlapping time slices, they order
// by timestamp and pack into a single run. The timestamp component of the
// composite key is what gives same-label sections a well-defined order.
func TestCalculateRuns_Timestamp_NonOverlap(t *testing.T) {
	// a: labels=["api"], ts=[100..200]
	// b: labels=["api"], ts=[300..400]
	//
	// Composite compare on (a.MaxKey, a.MaxTimestamp) vs (b.MinKey, b.MinTimestamp):
	// labels equal (["api"] == ["api"]); fall through to timestamp:
	// 200 < 300 -> a's upper bound < b's lower bound -> NO overlap -> one run.
	a := secTS("o", 0, []string{"api"}, []string{"api"}, 100, 200)
	b := secTS("o", 1, []string{"api"}, []string{"api"}, 300, 400)

	got := calculateRuns([]*compactionv2pb.SectionRef{b, a}) // jumbled

	require.Len(t, got, 1, "same-label, non-overlapping time should form one run")
	require.Equal(t, []*compactionv2pb.SectionRef{a, b}, got[0].sections,
		"composite sort key must order by timestamp when labels tie: a (ts=100) before b (ts=300)")
}

// TestCalculateRuns_Timestamp_Overlap is the dual: same labels with
// overlapping time slices must produce separate runs.
func TestCalculateRuns_Timestamp_Overlap(t *testing.T) {
	// a: labels=["api"], ts=[100..300]
	// b: labels=["api"], ts=[200..400]   -- overlaps a in time
	//
	// Composite compare on (a.MaxKey, a.MaxTimestamp) vs (b.MinKey, b.MinTimestamp):
	// labels equal; 300 > 200 -> a's upper bound > b's lower bound -> OVERLAP.
	// Two runs.
	a := secTS("o", 0, []string{"api"}, []string{"api"}, 100, 300)
	b := secTS("o", 1, []string{"api"}, []string{"api"}, 200, 400)

	got := calculateRuns([]*compactionv2pb.SectionRef{a, b})

	require.Len(t, got, 2, "same-label, time-overlapping sections must form separate runs")
}

// TestCalculateRuns_Timestamp_NumericOrder verifies that timestamps compare
// numerically as int64, not lexicographically as strings. A naive encoding
// of timestamps as Sprintf("%d", ts) would order ts=9 > ts=10 (because '9'
// (0x39) > '1' (0x31) under byte compare); the int64 field side-steps that
// entirely.
func TestCalculateRuns_Timestamp_NumericOrder(t *testing.T) {
	// Same labels; widely-separated timestamps where decimal-string compare
	// would give the wrong answer.
	//   a: labels=["api"], ts=[9..9]
	//   b: labels=["api"], ts=[10..10]
	// Numeric order: a < b (9 < 10). String "9" > "10" under byte compare.
	a := secTS("o", 0, []string{"api"}, []string{"api"}, 9, 9)
	b := secTS("o", 1, []string{"api"}, []string{"api"}, 10, 10)

	got := calculateRuns([]*compactionv2pb.SectionRef{b, a}) // jumbled

	require.Len(t, got, 1, "non-overlapping (one timestamp each) -> one run")
	require.Equal(t, []*compactionv2pb.SectionRef{a, b}, got[0].sections,
		"timestamps must compare numerically: ts=9 < ts=10")
}

// TestCalculateRuns_RunGrouping checks that touching sections form one run and
// overlapping sections split.
func TestCalculateRuns_RunGrouping(t *testing.T) {
	tests := []struct {
		name     string
		sections []*compactionv2pb.SectionRef
		wantRuns int
	}{
		{
			name:     "touching single-column key",
			sections: []*compactionv2pb.SectionRef{sec("o", 1, "m", "z"), sec("o", 0, "a", "m")},
			wantRuns: 1,
		},
		{
			name:     "overlapping single-column key splits",
			sections: []*compactionv2pb.SectionRef{sec("o", 0, "a", "m"), sec("o", 1, "k", "z")},
			wantRuns: 2,
		},
		{
			name: "touching timestamps, same label",
			sections: []*compactionv2pb.SectionRef{
				secTS("o", 1, []string{"api"}, []string{"api"}, 200, 300),
				secTS("o", 0, []string{"api"}, []string{"api"}, 100, 200),
			},
			wantRuns: 1,
		},
		{
			name: "disjoint timestamps, same label",
			sections: []*compactionv2pb.SectionRef{
				secTS("o", 0, []string{"auth"}, []string{"auth"}, 10, 20),
				secTS("o", 1, []string{"auth"}, []string{"auth"}, 30, 40),
			},
			wantRuns: 1,
		},
		{
			name: "overlapping timestamps, same label splits",
			sections: []*compactionv2pb.SectionRef{
				secTS("o", 0, []string{"auth"}, []string{"auth"}, 10, 30),
				secTS("o", 1, []string{"auth"}, []string{"auth"}, 20, 40),
			},
			wantRuns: 2,
		},
		{
			name: "touching multi-column tuple",
			sections: []*compactionv2pb.SectionRef{
				secT("o", 1, []string{"auth", "us"}, []string{"billing", "eu"}),
				secT("o", 0, []string{"auth", "eu"}, []string{"auth", "us"}),
			},
			wantRuns: 1,
		},
		{
			name: "touching full composite key",
			sections: []*compactionv2pb.SectionRef{
				secTS("o", 1, []string{"auth", "eu"}, []string{"auth", "eu"}, 40, 70),
				secTS("o", 0, []string{"auth", "eu"}, []string{"auth", "eu"}, 10, 40),
			},
			wantRuns: 1,
		},
		{
			name: "distinct multi-label tuples chain",
			sections: []*compactionv2pb.SectionRef{
				secTS("o", 0, []string{"auth", "eu"}, []string{"auth", "eu"}, 10, 40),
				secTS("o", 1, []string{"auth", "us"}, []string{"auth", "us"}, 20, 50),
				secTS("o", 2, []string{"billing", "eu"}, []string{"billing", "eu"}, 15, 45),
			},
			wantRuns: 1,
		},
		{
			name: "equal first key, second key disambiguates",
			sections: []*compactionv2pb.SectionRef{
				secTS("o", 0, []string{"auth", "eu"}, []string{"auth", "eu"}, 10, 40),
				secTS("o", 1, []string{"auth", "us"}, []string{"auth", "us"}, 10, 40),
			},
			wantRuns: 1,
		},
		{
			name: "identical multi-label tuple, overlapping times splits",
			sections: []*compactionv2pb.SectionRef{
				secTS("o", 0, []string{"auth", "eu"}, []string{"auth", "eu"}, 10, 30),
				secTS("o", 1, []string{"auth", "eu"}, []string{"auth", "eu"}, 20, 40),
			},
			wantRuns: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Len(t, calculateRuns(tc.sections), tc.wantRuns)
		})
	}
}

func runKey(v int) string { return fmt.Sprintf("%03d", v) }

// TestCalculateRuns_ValidRunSurvivesArbitrarySectionCuts checks that cutting one
// sorted run into many sections still yields exactly one run.
func TestCalculateRuns_ValidRunSurvivesArbitrarySectionCuts(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for trial := 0; trial < 50; trial++ {
		// Cut one sorted run into n sections; neighbours share a boundary key,
		// and a zero-width step makes a single-key section.
		n := 2 + r.Intn(12)
		bounds := make([]int, n+1)
		bounds[0] = r.Intn(3)
		for i := 1; i <= n; i++ {
			bounds[i] = bounds[i-1] + r.Intn(3)
		}

		sections := make([]*compactionv2pb.SectionRef, n)
		for i := 0; i < n; i++ {
			sections[i] = sec("o", int64(i), runKey(bounds[i]), runKey(bounds[i+1]))
		}

		// Run formation must be independent of input order.
		shuffled := append([]*compactionv2pb.SectionRef(nil), sections...)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		got := calculateRuns(shuffled)
		require.Lenf(t, got, 1, "trial %d: a cut-up valid run must re-derive as one run (bounds=%v)", trial, bounds)
		require.Equalf(t, sections, got[0].sections, "trial %d: sections must come back in sorted order", trial)
	}
}
