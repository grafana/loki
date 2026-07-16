package loser_test

import (
	"math"
	"testing"

	"github.com/grafana/loki/v3/pkg/util/loser"
	"github.com/stretchr/testify/require"
)

type List struct {
	list []uint64
	cur  uint64
}

func NewList(list ...uint64) *List {
	return &List{list: list}
}

func (it *List) At() uint64 {
	return it.cur
}

func (it *List) Next() bool {
	if len(it.list) > 0 {
		it.cur = it.list[0]
		it.list = it.list[1:]
		return true
	}
	it.cur = 0
	return false
}

func (it *List) Seek(val uint64) bool {
	for it.cur < val && len(it.list) > 0 {
		it.cur = it.list[0]
		it.list = it.list[1:]
	}
	return len(it.list) > 0
}

func checkIterablesEqual[E any, S1 loser.Sequence, S2 loser.Sequence](t *testing.T, a S1, b S2, at1 func(S1) E, at2 func(S2) E, less func(E, E) bool) {
	t.Helper()
	count := 0
	for a.Next() {
		count++
		if !b.Next() {
			t.Fatalf("b ended before a after %d elements", count)
		}
		if less(at1(a), at2(b)) || less(at2(b), at1(a)) {
			t.Fatalf("position %d: %v != %v", count, at1(a), at2(b))
		}
	}
	if b.Next() {
		t.Fatalf("a ended before b after %d elements", count)
	}
}

var testCases = []struct {
	name string
	args []*List
	want *List
}{
	{
		name: "empty input",
		want: NewList(),
	},
	{
		name: "one list",
		args: []*List{NewList(1, 2, 3, 4)},
		want: NewList(1, 2, 3, 4),
	},
	{
		name: "two lists",
		args: []*List{NewList(3, 4, 5), NewList(1, 2)},
		want: NewList(1, 2, 3, 4, 5),
	},
	{
		name: "two lists, first empty",
		args: []*List{NewList(), NewList(1, 2)},
		want: NewList(1, 2),
	},
	{
		name: "two lists, second empty",
		args: []*List{NewList(1, 2), NewList()},
		want: NewList(1, 2),
	},
	{
		name: "two lists b",
		args: []*List{NewList(1, 2), NewList(3, 4, 5)},
		want: NewList(1, 2, 3, 4, 5),
	},
	{
		name: "two lists c",
		args: []*List{NewList(1, 3), NewList(2, 4, 5)},
		want: NewList(1, 2, 3, 4, 5),
	},
	{
		name: "two lists, largest value in first list equal to maximum",
		args: []*List{NewList(1, math.MaxUint64), NewList(2, 3)},
		want: NewList(1, 2, 3, math.MaxUint64),
	},
	{
		name: "two lists, first straddles second and has maxval",
		args: []*List{NewList(1, 3, math.MaxUint64), NewList(2)},
		want: NewList(1, 2, 3, math.MaxUint64),
	},
	{
		name: "two lists, largest value in second list equal to maximum",
		args: []*List{NewList(1, 3), NewList(2, math.MaxUint64)},
		want: NewList(1, 2, 3, math.MaxUint64),
	},
	{
		name: "two lists, second straddles first and has maxval",
		args: []*List{NewList(2), NewList(1, 3, math.MaxUint64)},
		want: NewList(1, 2, 3, math.MaxUint64),
	},
	{
		name: "two lists, largest value in both lists equal to maximum",
		args: []*List{NewList(1, math.MaxUint64), NewList(2, math.MaxUint64)},
		want: NewList(1, 2, math.MaxUint64, math.MaxUint64),
	},
	{
		name: "three lists",
		args: []*List{NewList(1, 3), NewList(2, 4), NewList(5)},
		want: NewList(1, 2, 3, 4, 5),
	},
}

// Helpers for tests
func listAt(s *List) uint64                      { return s.At() }
func lessUint64(a, b uint64) bool                { return a < b }
func treeAt(s *loser.Tree[uint64, *List]) uint64 { return s.Winner().At() }

func TestMerge(t *testing.T) {
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			numCloses := 0
			closeFn := func(_ *List) {
				numCloses++
			}
			lt := loser.New(tt.args, math.MaxUint64, listAt, lessUint64, closeFn)
			checkIterablesEqual(t, tt.want, lt, listAt, treeAt, lessUint64)
			if numCloses != len(tt.args) {
				t.Errorf("Expected %d closes, got %d", len(tt.args), numCloses)
			}
		})
	}
}

func TestPush(t *testing.T) {
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			numCloses := 0
			closeFn := func(_ *List) {
				numCloses++
			}
			lt := loser.New(nil, math.MaxUint64, listAt, lessUint64, closeFn)
			for _, s := range tt.args {
				lt.Push(s)
			}
			checkIterablesEqual(t, tt.want, lt, listAt, treeAt, lessUint64)
			if numCloses != len(tt.args) {
				t.Errorf("Expected %d closes, got %d", len(tt.args), numCloses)
			}
		})
	}
}

func TestPushDuringIteration(t *testing.T) {
	// Start with {1, 2} then push {5, 6}, {3}, {4}, interleaved with some Next() calls.
	lt := loser.New([]*List{NewList(1, 2)}, math.MaxUint64, func(s *List) uint64 { return s.At() }, lessUint64, func(*List) {})

	require.True(t, lt.Next())
	require.Equal(t, uint64(1), lt.Winner().At())

	lt.Push(NewList(5, 6))

	require.True(t, lt.Next())
	require.Equal(t, uint64(2), lt.Winner().At())

	lt.Push(NewList(3))
	lt.Push(NewList(4))

	want := NewList(3, 4, 5, 6)
	checkIterablesEqual(t, want, lt, listAt, treeAt, lessUint64)
}
