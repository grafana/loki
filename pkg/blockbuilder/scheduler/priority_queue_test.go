package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCircularBuffer_Range(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		input    []int
		want     []int
	}{
		{
			name:     "empty buffer",
			capacity: 3,
			input:    []int{},
			want:     []int{},
		},
		{
			name:     "partially filled buffer",
			capacity: 3,
			input:    []int{1, 2},
			want:     []int{1, 2},
		},
		{
			name:     "full buffer",
			capacity: 3,
			input:    []int{1, 2, 3},
			want:     []int{1, 2, 3},
		},
		{
			name:     "buffer with eviction",
			capacity: 3,
			input:    []int{1, 2, 3, 4, 5},
			want:     []int{3, 4, 5}, // oldest elements (1,2) were evicted
		},
		{
			name:     "buffer with multiple evictions",
			capacity: 2,
			input:    []int{1, 2, 3, 4, 5},
			want:     []int{4, 5}, // only newest elements remain
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create and fill buffer
			buf := NewCircularBuffer[int](tt.capacity)
			for _, v := range tt.input {
				buf.Push(v)
			}

			// Use Range to collect elements
			got := make([]int, 0)
			buf.Range(func(v int) bool {
				got = append(got, v)
				return true
			})

			require.Equal(t, tt.want, got, "Range should iterate in order from oldest to newest")
		})
	}
}

func TestCircularBuffer_Range_EarlyStop(t *testing.T) {
	buf := NewCircularBuffer[int](5)
	for i := 1; i <= 5; i++ {
		buf.Push(i)
	}

	var got []int
	buf.Range(func(v int) bool {
		got = append(got, v)
		return v != 3 // stop after seeing 3
	})

	require.Equal(t, []int{1, 2, 3}, got, "Range should stop when function returns false")
}
