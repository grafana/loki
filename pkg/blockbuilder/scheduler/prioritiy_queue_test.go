package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPriorityQueue(t *testing.T) {
	t.Run("basic operations", func(t *testing.T) {
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
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				pq := NewPriorityQueue[int, int](
					func(a, b int) bool { return a < b },
					func(v int) int { return v },
				)
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

	t.Run("key operations", func(t *testing.T) {
		type Job struct {
			ID       string
			Priority int
		}

		pq := NewPriorityQueue[string, Job](
			func(a, b Job) bool { return a.Priority < b.Priority },
			func(j Job) string { return j.ID },
		)

		// Test Push with duplicate key
		job1 := Job{ID: "job1", Priority: 1}
		job1Updated := Job{ID: "job1", Priority: 3}
		job2 := Job{ID: "job2", Priority: 2}

		pq.Push(job1)
		require.Equal(t, 1, pq.Len())

		// Push with same key should update
		pq.Push(job1Updated)
		require.Equal(t, 1, pq.Len())

		// Verify updated priority
		v, ok := pq.Lookup("job1")
		require.True(t, ok)
		require.Equal(t, job1Updated, v)

		// Test Remove
		pq.Push(job2)
		v, ok = pq.Remove("job1")
		require.True(t, ok)
		require.Equal(t, job1Updated, v)
		require.Equal(t, 1, pq.Len())

		// Test UpdatePriority
		newJob2 := Job{ID: "job2", Priority: 4}
		ok = pq.UpdatePriority("job2", newJob2)
		require.True(t, ok)

		v, ok = pq.Lookup("job2")
		require.True(t, ok)
		require.Equal(t, newJob2, v)

		// Test non-existent key operations
		v, ok = pq.Lookup("nonexistent")
		require.False(t, ok)
		require.Zero(t, v)

		v, ok = pq.Remove("nonexistent")
		require.False(t, ok)
		require.Zero(t, v)

		ok = pq.UpdatePriority("nonexistent", Job{})
		require.False(t, ok)
	})

	t.Run("custom type", func(t *testing.T) {
		type Job struct {
			ID       string
			Priority int
		}

		pq := NewPriorityQueue[string, Job](
			func(a, b Job) bool { return a.Priority < b.Priority },
			func(j Job) string { return j.ID },
		)

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
		pq := NewPriorityQueue[int, int](
			func(a, b int) bool { return a < b },
			func(v int) int { return v },
		)

		// Push some elements
		pq.Push(3)
		pq.Push(1)
		pq.Push(4)

		// Pop an element
		v, ok := pq.Pop()
		require.True(t, ok)
		require.Equal(t, 1, v)

		// Push more elements
		pq.Push(2)
		pq.Push(5)

		// Pop remaining elements and verify order
		want := []int{2, 3, 4, 5}
		got := make([]int, 0, len(want))
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

func TestCircularBufferLookup(t *testing.T) {
	t.Run("empty buffer", func(t *testing.T) {
		cb := NewCircularBuffer[int](5)
		_, ok := cb.Lookup(func(i int) bool { return i == 1 })
		require.False(t, ok)
	})

	t.Run("single element", func(t *testing.T) {
		cb := NewCircularBuffer[int](5)
		cb.Push(1)
		v, ok := cb.Lookup(func(i int) bool { return i == 1 })
		require.True(t, ok)
		require.Equal(t, 1, v)
	})

	t.Run("multiple elements", func(t *testing.T) {
		cb := NewCircularBuffer[int](5)
		for i := 1; i <= 3; i++ {
			cb.Push(i)
		}
		v, ok := cb.Lookup(func(i int) bool { return i == 2 })
		require.True(t, ok)
		require.Equal(t, 2, v)
	})

	t.Run("wrapped buffer", func(t *testing.T) {
		cb := NewCircularBuffer[int](3)
		// Push 5 elements into a buffer of size 3, causing wrap-around
		for i := 1; i <= 5; i++ {
			cb.Push(i)
		}
		// Buffer should now contain [4,5,3] with head at index 2
		v, ok := cb.Lookup(func(i int) bool { return i == 4 })
		require.True(t, ok)
		require.Equal(t, 4, v)

		// Element that was evicted should not be found
		_, ok = cb.Lookup(func(i int) bool { return i == 1 })
		require.False(t, ok)
	})

	t.Run("no match", func(t *testing.T) {
		cb := NewCircularBuffer[int](5)
		for i := 1; i <= 3; i++ {
			cb.Push(i)
		}
		_, ok := cb.Lookup(func(i int) bool { return i == 99 })
		require.False(t, ok)
	})
}
