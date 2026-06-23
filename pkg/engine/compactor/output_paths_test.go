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

// TestIndexMergePath_Build_ResultIndependentOfReuse tests that a path returned
// from an earlier Build call is safe to retain across subsequent Build calls on
// the same value.
func TestIndexMergePath_Build_ResultIndependentOfReuse(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)

	// Compute the reference value with a separate, fresh indexMergePath so
	// any aliasing in the reused-builder case would surface as a mismatch.
	var ref indexMergePath
	want := ref.Build("tenant-a", window, 1, 0, []string{"i0#0", "i1#0"})

	var reuser indexMergePath
	got := reuser.Build("tenant-a", window, 1, 0, []string{"i0#0", "i1#0"})
	require.Equal(t, want, got, "fresh and reused builders must agree on the same inputs")

	// Reuse the builder with different inputs. The previously-returned string
	// must remain equal to the reference.
	for i := range 8 {
		reused := reuser.Build("tenant-b", window, 7, i, []string{"x#0", "y#0", "z#0", "w#0"})
		require.NotEqual(t, got, reused)
	}
	require.Equal(t, want, got,
		"path string must not be aliased to indexMergePath internal buffers")
}

// BenchmarkIndexMergePath_Build illustrates the benefit of reusing an
// indexMergePath across Build calls. The three subcases share inputs:
//
//   - fresh_per_call: construct a new indexMergePath for every Build call.
//     Each call grows its internal scratch from zero and lets it be GC'd.
//   - reused_builder: declare the indexMergePath once, call Build many
//     times. The coordinator's hot loop pattern — scratch buffers reach
//     their high-water mark on the first call and stay there.
//   - per_cycle_batch_16: realistic per-coordinator-cycle shape — a fresh
//     indexMergePath per cycle, reused across 16 tasks within that cycle.
func BenchmarkIndexMergePath_Build(b *testing.B) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	ids := []string{
		"indexes/aa/src-0#0", "indexes/bb/src-1#0", "indexes/cc/src-2#0",
		"indexes/dd/src-3#0", "indexes/ee/src-4#0", "indexes/ff/src-5#0",
		"indexes/gg/src-6#0", "indexes/hh/src-7#0",
	}

	b.Run("fresh_per_call", func(b *testing.B) {
		b.ReportAllocs()
		var sink string
		for i := 0; b.Loop(); i++ {
			var bldr indexMergePath
			sink = bldr.Build("tenant-29", window, 1, i&0xff, ids)
		}
		_ = sink
	})

	b.Run("reused_builder", func(b *testing.B) {
		var bldr indexMergePath
		b.ReportAllocs()
		var sink string
		for i := 0; b.Loop(); i++ {
			sink = bldr.Build("tenant-29", window, 1, i&0xff, ids)
		}
		_ = sink
	})

	b.Run("per_cycle_batch_16", func(b *testing.B) {
		const tasksPerCycle = 16
		b.ReportAllocs()
		var sink string
		for n := 0; b.Loop(); n++ {
			var bldr indexMergePath
			for i := 0; i < tasksPerCycle; i++ {
				sink = bldr.Build(fmt.Sprintf("tenant-%d", n&0xff), window, 1, i, ids)
			}
		}
		_ = sink
	})
}
