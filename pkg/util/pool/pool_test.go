package pool

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPoolNew(t *testing.T) {
	testPool := New(1, 8, 2, func(size int) interface{} {
		return make([]int, size)
	})
	cases := []struct {
		size        int
		expectedCap int
	}{
		{
			size:        -1,
			expectedCap: 1,
		},
		{
			size:        3,
			expectedCap: 4,
		},
		{
			size:        10,
			expectedCap: 10,
		},
	}
	for _, c := range cases {
		ret := testPool.Get(c.size)
		require.Equal(t, c.expectedCap, cap(ret.([]int)))
		testPool.Put(ret)
	}
}

func TestPoolNewWithSizes(t *testing.T) {
	testPool := NewWithSizes([]int{1, 2, 4, 8}, func(size int) interface{} {
		return make([]int, size)
	})
	cases := []struct {
		size        int
		expectedCap int
	}{
		{
			size:        -1,
			expectedCap: 1,
		},
		{
			size:        3,
			expectedCap: 4,
		},
		{
			size:        10,
			expectedCap: 10,
		},
	}
	for _, c := range cases {
		ret := testPool.Get(c.size)
		require.Equal(t, c.expectedCap, cap(ret.([]int)))
		testPool.Put(ret)
	}
}
