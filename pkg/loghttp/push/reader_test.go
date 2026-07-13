package push

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedLimitReader_ReadsFullyWhenBudgetAmple(t *testing.T) {
	const content = "hello world, this is a small body"
	var budget atomic.Int64
	budget.Store(1 << 20) // 1MB, far more than needed
	start := budget.Load()

	r := NewSharedLimitReader(strings.NewReader(content), &budget)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, content, string(got))
	require.False(t, r.Truncated())

	// While open, at least one block is reserved from the budget.
	require.Less(t, budget.Load(), start)

	// Close returns the entire reservation to the budget.
	require.NoError(t, r.Close())
	require.Equal(t, start, budget.Load())
}

func TestSharedLimitReader_TruncatesWhenBudgetExhausted(t *testing.T) {
	// Body larger than the budget.
	body := strings.Repeat("a", 5*allocBlockSize)

	for _, tc := range []struct {
		name   string
		budget int64
	}{
		{name: "exact multiple of block", budget: 2 * allocBlockSize},
		{name: "non-multiple of block", budget: 2*allocBlockSize + 1234},
		{name: "smaller than a block", budget: allocBlockSize - 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var budget atomic.Int64
			budget.Store(tc.budget)

			r := NewSharedLimitReader(strings.NewReader(body), &budget)
			got, err := io.ReadAll(r)
			require.NoError(t, err) // io.ReadAll treats io.EOF as success
			require.True(t, r.Truncated())

			// We only ever deliver whole reserved blocks, so the delivered
			// amount is the largest multiple of allocBlockSize that fits the
			// budget, and never more than the budget.
			require.LessOrEqual(t, int64(len(got)), tc.budget)
			if tc.budget >= allocBlockSize {
				require.Greater(t, int64(len(got)), tc.budget-allocBlockSize)
			} else {
				require.Empty(t, got)
			}

			// Close restores the reservation.
			require.NoError(t, r.Close())
			require.Equal(t, tc.budget, budget.Load())
		})
	}
}

func TestSharedLimitReader_ReservesInBlocks(t *testing.T) {
	body := strings.Repeat("a", 3*allocBlockSize)
	var budget atomic.Int64
	budget.Store(10 * allocBlockSize)
	start := budget.Load()

	r := NewSharedLimitReader(bytes.NewReader([]byte(body)), &budget)

	// Read a single byte: this should reserve exactly one block from the budget.
	buf := make([]byte, 1)
	n, err := r.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, start-allocBlockSize, budget.Load())

	require.NoError(t, r.Close())
	require.Equal(t, start, budget.Load())
}

func TestSharedLimitReader_ConcurrentReadersShareBudget(t *testing.T) {
	const readers = 32
	body := strings.Repeat("a", 4*allocBlockSize)

	var budget atomic.Int64
	// Only enough budget for a couple of readers to complete at a time.
	budget.Store(8 * allocBlockSize)
	start := budget.Load()

	var minObserved atomic.Int64
	minObserved.Store(start)

	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := NewSharedLimitReader(strings.NewReader(body), &budget)
			_, _ = io.ReadAll(r)
			if v := budget.Load(); v < minObserved.Load() {
				minObserved.Store(v)
			}
			_ = r.Close()
		}()
	}
	wg.Wait()

	// The budget must never be driven below zero and must be fully restored
	// once every reader has closed.
	require.GreaterOrEqual(t, minObserved.Load(), int64(0))
	require.Equal(t, start, budget.Load())
}
