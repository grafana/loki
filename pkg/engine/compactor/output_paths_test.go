package compactor

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIndexMergePath_Build_Deterministic verifies the same inputs always
// produce the same path. This is what makes racing coordinators safe: they
// produce identical plans → identical paths → racing workers existence-check
// and dedupe.
func TestIndexMergePath_Build_Deterministic(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	ids := []string{"obj-a#0", "obj-b#3", "obj-a#1"}

	var b indexMergePath
	p1 := b.Build("tenant-1", window, 1, 0, ids)
	p2 := b.Build("tenant-1", window, 1, 0, ids)
	require.Equal(t, p1, p2, "identical inputs must produce identical paths")
}

// TestIndexMergePath_Build_ShuffledSectionIDsStable verifies the section-ID
// slice order does not affect the path. The planner emits piles in a
// deterministic order today, but cross-coordinator safety requires the path
// to be invariant under any permutation of the same section ID set.
func TestIndexMergePath_Build_ShuffledSectionIDsStable(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	idsA := []string{"obj-a#0", "obj-b#3", "obj-c#1"}
	idsB := []string{"obj-c#1", "obj-a#0", "obj-b#3"} // same set, shuffled

	var b indexMergePath
	pA := b.Build("tenant-1", window, 1, 0, idsA)
	pB := b.Build("tenant-1", window, 1, 0, idsB)
	require.Equal(t, pA, pB, "section ID order must not affect the path")
}

// TestIndexMergePath_Build_AnyInputChangeChangesPath verifies every input
// participates in the hash. If any of (tenant, window, planVersion, binIndex,
// sectionIDs) changes, the path must change. Otherwise distinct plans could
// alias to the same output and a stale-rewrite race becomes possible.
func TestIndexMergePath_Build_AnyInputChangeChangesPath(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)

	var b indexMergePath
	base := b.Build("tenant-1", window, 1, 0, []string{"a#0", "b#0"})

	cases := map[string]struct {
		tenant      string
		window      time.Time
		planVersion uint
		binIndex    int
		sectionIDs  []string
	}{
		"tenant":       {"tenant-2", window, 1, 0, []string{"a#0", "b#0"}},
		"window":       {"tenant-1", window.Add(time.Hour), 1, 0, []string{"a#0", "b#0"}},
		"plan version": {"tenant-1", window, 2, 0, []string{"a#0", "b#0"}},
		"bin index":    {"tenant-1", window, 1, 1, []string{"a#0", "b#0"}},
		"section ids":  {"tenant-1", window, 1, 0, []string{"a#0", "b#1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := b.Build(c.tenant, c.window, c.planVersion, c.binIndex, c.sectionIDs)
			require.NotEqual(t, base, got, "%s change must change the path", name)
		})
	}
}

// TestIndexMergePath_Build_ResultIndependentOfReuse pins the contract the
// coordinator's hot loop relies on: a path returned from an earlier Build
// call is safe to retain across subsequent Build calls on the same value.
// If the path string ever aliased indexMergePath.buf or .hexBuf, a
// subsequent call would corrupt the stored output.
func TestIndexMergePath_Build_ResultIndependentOfReuse(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)

	// Compute the reference value with a separate, fresh indexMergePath so
	// any aliasing in the reused-builder case would surface as a mismatch.
	var ref indexMergePath
	want := ref.Build("tenant-a", window, 1, 0, []string{"i0#0", "i1#0"})

	var reuser indexMergePath
	got := reuser.Build("tenant-a", window, 1, 0, []string{"i0#0", "i1#0"})
	require.Equal(t, want, got, "fresh and reused builders must agree on the same inputs")

	// Reuse the builder with very different inputs that grow buf and
	// rewrite hexBuf. The previously-returned string must remain byte-equal
	// to the reference.
	for i := 0; i < 8; i++ {
		reuser.Build("tenant-b", window, 7, i, []string{"x#0", "y#0", "z#0", "w#0"})
	}
	require.Equal(t, want, got,
		"path string must not be aliased to indexMergePath internal buffers")
}

// BenchmarkIndexMergePath_Build_FreshBuilder measures the cost of throwing
// away the builder after every call — the anti-pattern. Production hot loops
// (coordinator) must instead hold one indexMergePath across calls. See
// BenchmarkIndexMergePath_Build_ReusedBuilder for the intended cost.
func BenchmarkIndexMergePath_Build_FreshBuilder(b *testing.B) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	ids := []string{
		"indexes/aa/src-0#0", "indexes/bb/src-1#0", "indexes/cc/src-2#0",
		"indexes/dd/src-3#0", "indexes/ee/src-4#0", "indexes/ff/src-5#0",
		"indexes/gg/src-6#0", "indexes/hh/src-7#0",
	}
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		var bldr indexMergePath
		sink = bldr.Build("tenant-29", window, 1, i&0xff, ids)
	}
	_ = sink
}

// BenchmarkIndexMergePath_Build_ReusedBuilder measures the production-hot-loop
// cost: one indexMergePath, many Build calls.
func BenchmarkIndexMergePath_Build_ReusedBuilder(b *testing.B) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	ids := []string{
		"indexes/aa/src-0#0", "indexes/bb/src-1#0", "indexes/cc/src-2#0",
		"indexes/dd/src-3#0", "indexes/ee/src-4#0", "indexes/ff/src-5#0",
		"indexes/gg/src-6#0", "indexes/hh/src-7#0",
	}
	var bldr indexMergePath
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink = bldr.Build("tenant-29", window, 1, i&0xff, ids)
	}
	_ = sink
}

// BenchmarkIndexMergePath_Build_CycleBatch simulates a realistic per-cycle
// batch: a tenant with N tasks, all paths computed in sequence from one
// builder.
func BenchmarkIndexMergePath_Build_CycleBatch(b *testing.B) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	const tasksPerCycle = 16
	ids := []string{
		"indexes/aa/src-0#0", "indexes/bb/src-1#0", "indexes/cc/src-2#0",
		"indexes/dd/src-3#0", "indexes/ee/src-4#0", "indexes/ff/src-5#0",
		"indexes/gg/src-6#0", "indexes/hh/src-7#0",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var bldr indexMergePath
		var sink string
		for i := 0; i < tasksPerCycle; i++ {
			sink = bldr.Build(fmt.Sprintf("tenant-%d", n&0xff), window, 1, i, ids)
		}
		_ = sink
	}
}
