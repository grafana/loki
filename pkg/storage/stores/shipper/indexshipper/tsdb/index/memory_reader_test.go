package index

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// newUnboundedInMemoryOptions returns options whose budget is large enough
// that every test fixture is held in memory.
func newUnboundedInMemoryOptions() InMemoryOptions {
	return InMemoryOptions{
		Budget:   NewMemoryBudget(1<<40, nil),
		Fallback: MmapOptions{},
	}
}

// openInMemory opens path with an unbounded in-memory reader and registers a
// cleanup that closes it.
func openInMemory(t testing.TB, path string) Reader {
	t.Helper()
	r, err := newUnboundedInMemoryOptions().OpenReader(path)
	require.NoError(t, err)
	require.Equal(t, TierMemory, r.Tier())
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return r
}

func openStream(t testing.TB, path string) Reader {
	t.Helper()
	r, err := NewStreamFileReader(path, DefaultStreamOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return r
}

func fileSize(t testing.TB, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	return fi.Size()
}

func TestInMemoryReader_CrossCheck(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			path := writeCrossCheckFixture(t, format)
			memory, stream := openInMemory(t, path), openStream(t, path)

			require.Equal(t, stream.Version(), memory.Version())
			require.Equal(t, stream.Checksum(), memory.Checksum())
			require.Equal(t, stream.Size(), memory.Size())

			streamMin, streamMax := stream.Bounds()
			memoryMin, memoryMax := memory.Bounds()
			require.Equal(t, streamMin, memoryMin)
			require.Equal(t, streamMax, memoryMax)

			requireLabelsEqual(t, stream, memory)
			requirePostingsSeriesEqual(t, stream, memory)
		})
	}
}

func TestInMemoryReader_SeriesMatchesStream(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			path, through := writeManyChunksFixture(t, format)
			memory, stream := openInMemory(t, path), openStream(t, path)
			streamScan, memoryScan := scanBoth(t, stream, memory)

			refs := allSeriesRefs(t, stream)
			require.NotEmpty(t, refs)
			require.Equal(t, refs, allSeriesRefs(t, memory))

			for _, w := range []struct{ from, through int64 }{
				{0, math.MaxInt64},
				{20*chunkSpan + 50, 40*chunkSpan + 50},
				{through, through + chunkSpan},
			} {
				for _, ref := range refs {
					requireStreamSeriesEqual(t, streamScan, memoryScan, ref, w.from, w.through)
				}
			}
		})
	}
}

func TestInMemoryOptions_BudgetAccounting(t *testing.T) {
	first := writeCrossCheckFixture(t, FormatV3)
	second := writeCrossCheckFixture(t, FormatV3)
	size := fileSize(t, first)
	require.Equal(t, size, fileSize(t, second))

	budget := NewMemoryBudget(size, nil)
	opts := InMemoryOptions{Budget: budget, Placement: PlaceAll, Fallback: MmapOptions{}}

	requireGauges := func(t *testing.T, bytes, memoryFiles, diskFiles, budgetRefusals float64) {
		t.Helper()
		require.Equal(t, bytes, testutil.ToFloat64(budget.usedBytes), "in-memory bytes")
		require.Equal(t, memoryFiles, testutil.ToFloat64(budget.files.WithLabelValues(string(TierMemory))), "memory files")
		require.Equal(t, diskFiles, testutil.ToFloat64(budget.files.WithLabelValues(string(TierDisk))), "disk files")
		require.Equal(t, budgetRefusals, testutil.ToFloat64(budget.refusals.WithLabelValues(refusalReasonBudget)), "budget refusals")
	}
	requireGauges(t, 0, 0, 0, 0)

	// The first file uses the whole budget.
	r1, err := opts.OpenReader(first)
	require.NoError(t, err)
	require.Equal(t, TierMemory, r1.Tier())
	requireGauges(t, float64(size), 1, 0, 0)

	// The second does not fit, so it falls back to disk.
	r2, err := opts.OpenReader(second)
	require.NoError(t, err)
	require.Equal(t, TierDisk, r2.Tier())
	requireGauges(t, float64(size), 1, 1, 1)
	requireLabelsEqual(t, r1, r2)

	// Closing the first releases its bytes, and closing it again releases nothing.
	require.NoError(t, r1.Close())
	requireGauges(t, 0, 0, 1, 1)
	require.NoError(t, r1.Close())
	requireGauges(t, 0, 0, 1, 1)

	// With room in the budget again, a reopen goes back in memory.
	r3, err := opts.OpenReader(second)
	require.NoError(t, err)
	require.Equal(t, TierMemory, r3.Tier())
	requireGauges(t, float64(size), 1, 1, 1)

	require.NoError(t, r2.Close())
	requireGauges(t, float64(size), 1, 0, 1)
	require.NoError(t, r3.Close())
	requireGauges(t, 0, 0, 0, 1)
}

func TestInMemoryOptions_PlacementRefusalFallsBack(t *testing.T) {
	path := writeCrossCheckFixture(t, FormatV3)
	budget := NewMemoryBudget(1<<40, nil)
	opts := InMemoryOptions{
		Budget:    budget,
		Placement: func(string) bool { return false },
		Fallback:  DefaultStreamOptions(),
	}

	r, err := opts.OpenReader(path)
	require.NoError(t, err)
	require.Equal(t, TierDisk, r.Tier())
	require.Equal(t, float64(1), testutil.ToFloat64(budget.refusals.WithLabelValues(refusalReasonPlacement)))
	require.Equal(t, float64(0), testutil.ToFloat64(budget.usedBytes))
	require.Equal(t, float64(1), testutil.ToFloat64(budget.files.WithLabelValues(string(TierDisk))))

	requireLabelsEqual(t, openStream(t, path), r)

	require.NoError(t, r.Close())
	require.Equal(t, float64(0), testutil.ToFloat64(budget.files.WithLabelValues(string(TierDisk))))
}

func TestInMemoryOptions_ErrorsDoNotLeakReservation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		path    func(t *testing.T) string
		wantErr string
	}{
		{
			name:    "missing file",
			path:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
			wantErr: "no such file",
		},
		{
			name: "corrupt file",
			path: func(t *testing.T) string {
				path := writeCrossCheckFixture(t, FormatV3)
				corruptFileBytes(t, path, func(b []byte) { b[0] ^= 0xFF })
				return path
			},
			wantErr: "invalid magic number",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget := NewMemoryBudget(1<<40, nil)
			opts := InMemoryOptions{Budget: budget, Placement: PlaceAll, Fallback: MmapOptions{}}

			_, err := opts.OpenReader(tc.path(t))
			require.ErrorContains(t, err, tc.wantErr)
			require.Equal(t, float64(0), testutil.ToFloat64(budget.usedBytes))
			require.Equal(t, float64(0), testutil.ToFloat64(budget.files.WithLabelValues(string(TierMemory))))
			// Read and decode errors are returned, not treated as refusals.
			require.Equal(t, float64(0), testutil.ToFloat64(budget.refusals.WithLabelValues(refusalReasonBudget)))
		})
	}
}

func TestMemoryBudget_TryReserve(t *testing.T) {
	b := NewMemoryBudget(10, nil)
	require.True(t, b.TryReserve(4))
	require.True(t, b.TryReserve(6))
	require.False(t, b.TryReserve(1))
	b.Release(6)
	require.False(t, b.TryReserve(7))
	require.True(t, b.TryReserve(6))
	require.False(t, b.TryReserve(-1))
	require.Equal(t, float64(10), testutil.ToFloat64(b.usedBytes))
}
