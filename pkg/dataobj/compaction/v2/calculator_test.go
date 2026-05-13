package compactionv2

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// Suppress unused import warning; math/rand is used in the shuffle determinism tests.
var _ = rand.Float64

// sec is a small constructor for SectionRef test fixtures.
func sec(path string, idx int32, minKey, maxKey string) *compactionv2pb.SectionRef {
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
	require.Equal(t, "b", got[0].topMaxKey)
	require.Equal(t, 0, got[0].createdAt)
}

func TestPatienceSort_AllNonOverlapping(t *testing.T) {
	s1 := sec("o", 0, "a", "b")
	s2 := sec("o", 1, "c", "d")
	s3 := sec("o", 2, "e", "f")

	got := calculateRuns([]*compactionv2pb.SectionRef{s1, s2, s3})

	require.Len(t, got, 1, "all non-overlapping sections should form exactly one pile")
	require.Equal(t, []*compactionv2pb.SectionRef{s1, s2, s3}, got[0].sections)
	require.Equal(t, "f", got[0].topMaxKey)
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
	require.Equal(t, "20", got[1].topMaxKey)
	// pile0 should still hold only s1.
	require.Equal(t, []*compactionv2pb.SectionRef{s1}, got[0].sections)
	require.Equal(t, "05", got[0].topMaxKey)
}

func TestPatienceSort_TiebreakerOnCreationOrder(t *testing.T) {
	// Two piles will both end up with topMaxKey == "10":
	//   s1 = ("00","10") — creates pile0
	//   s2 = ("05","11") — overlaps s1 (MinKey "05" < pile0 topMaxKey "10"); creates pile1
	//   s3 = ("06","10") — overlaps both ("06" < both "10"); creates pile2
	// pile0 topMaxKey=10, pile1 topMaxKey=11, pile2 topMaxKey=10
	//   s4 = ("11","20") — eligible for pile0 (topMaxKey "10" < "11") AND pile2 (topMaxKey "10" < "11").
	//   pile1 topMaxKey "11" is NOT < "11"; not eligible.
	//   Per D2 tiebreak: pick the OLDEST eligible pile -> pile0 (createdAt=0).
	s1 := sec("o", 0, "00", "10")
	s2 := sec("o", 1, "05", "11")
	s3 := sec("o", 2, "06", "10")
	s4 := sec("o", 3, "11", "20")

	got := calculateRuns([]*compactionv2pb.SectionRef{s1, s2, s3, s4})

	require.Len(t, got, 3)
	// pile0 (createdAt=0) should have received s4.
	require.Equal(t, []*compactionv2pb.SectionRef{s1, s4}, got[0].sections,
		"D2: among piles with equal topMaxKey, append to the OLDEST")
	// pile2 unchanged.
	require.Equal(t, []*compactionv2pb.SectionRef{s3}, got[2].sections,
		"D2: newer pile is NOT chosen on tie")
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
