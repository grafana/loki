package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPriorityQueue(t *testing.T) {
	t.Run("operations", func(t *testing.T) {
		tests := []struct {
			name     string
			input    []int
			wantPops []int
		}{
			{
				name:     "empty queue",
				input:    []int{},
				wantPops: []int{},
			},
			{
				name:     "single element",
				input:    []int{1},
				wantPops: []int{1},
			},
			{
				name:     "multiple elements in order",
				input:    []int{1, 2, 3},
				wantPops: []int{1, 2, 3},
			},
			{
				name:     "multiple elements out of order",
				input:    []int{3, 1, 2},
				wantPops: []int{1, 2, 3},
			},
			{
				name:     "duplicate elements",
				input:    []int{2, 1, 2, 1},
				wantPops: []int{1, 1, 2, 2},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				pq := NewPriorityQueue[int](func(a, b int) bool { return a < b })
				require.Equal(t, 0, pq.Len())

				// Push all elements
				for _, v := range tt.input {
					pq.Push(v)
				}
				require.Equal(t, len(tt.input), pq.Len())

				// Pop all elements and verify order
				got := make([]int, 0, len(tt.input))
				for range tt.input {
					v, ok := pq.Pop()
					require.True(t, ok)
					got = append(got, v)
				}
				require.Equal(t, tt.wantPops, got)

				// Verify empty queue behavior
				v, ok := pq.Pop()
				require.False(t, ok)
				require.Zero(t, v)
				require.Equal(t, 0, pq.Len())
			})
		}
	})

	t.Run("custom type", func(t *testing.T) {
		type Job struct {
			ID       string
			Priority int
		}

		pq := NewPriorityQueue[Job](func(a, b Job) bool {
			return a.Priority < b.Priority
		})

		jobs := []Job{
			{ID: "high", Priority: 3},
			{ID: "low", Priority: 1},
			{ID: "medium", Priority: 2},
		}

		// Push all jobs
		for _, j := range jobs {
			pq.Push(j)
		}

		// Verify they come out in priority order
		want := []string{"low", "medium", "high"}
		got := make([]string, 0, len(jobs))
		for range jobs {
			j, ok := pq.Pop()
			require.True(t, ok)
			got = append(got, j.ID)
		}
		require.Equal(t, want, got)
	})

	t.Run("mixed operations", func(t *testing.T) {
		pq := NewPriorityQueue[int](func(a, b int) bool { return a < b })

		// Push some elements
		pq.Push(3)
		pq.Push(1)
		require.Equal(t, 2, pq.Len())

		// Pop lowest
		v, ok := pq.Pop()
		require.True(t, ok)
		require.Equal(t, 1, v)

		// Push more elements
		pq.Push(2)
		pq.Push(4)

		// Verify remaining elements come out in order
		want := []int{2, 3, 4}
		got := make([]int, 0, 3)
		for range want {
			v, ok := pq.Pop()
			require.True(t, ok)
			got = append(got, v)
		}
		require.Equal(t, want, got)
	})
}

func TestCircularBuffer(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		input    []int
		wantPops []int
	}{
		{
			name:     "empty buffer",
			capacity: 5,
			input:    []int{},
			wantPops: []int{},
		},
		{
			name:     "partial fill",
			capacity: 5,
			input:    []int{1, 2, 3},
			wantPops: []int{1, 2, 3},
		},
		{
			name:     "full buffer",
			capacity: 3,
			input:    []int{1, 2, 3},
			wantPops: []int{1, 2, 3},
		},
		{
			name:     "overflow buffer",
			capacity: 3,
			input:    []int{1, 2, 3, 4, 5},
			wantPops: []int{3, 4, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircularBuffer[int](tt.capacity)
			require.Equal(t, 0, cb.Len())

			// Push all elements
			for _, v := range tt.input {
				cb.Push(v)
			}
			require.Equal(t, min(tt.capacity, len(tt.input)), cb.Len())

			// Pop all elements and verify order
			got := make([]int, 0, cb.Len())
			for cb.Len() > 0 {
				v, ok := cb.Pop()
				require.True(t, ok)
				got = append(got, v)
			}
			require.Equal(t, tt.wantPops, got)

			// Verify empty buffer behavior
			v, ok := cb.Pop()
			require.False(t, ok)
			require.Zero(t, v)
			require.Equal(t, 0, cb.Len())
		})
	}
}
