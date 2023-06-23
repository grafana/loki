package logql

import (
	"math"

	"github.com/prometheus/prometheus/promql"
)

type vectorByValueHeap promql.Vector

func (s vectorByValueHeap) Len() int {
	return len(s)
}

func (s vectorByValueHeap) Less(i, j int) bool {
	if math.IsNaN(s[i].F) {
		return true
	}
	return s[i].F < s[j].F
}

func (s vectorByValueHeap) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s *vectorByValueHeap) Push(x interface{}) {
	*s = append(*s, *(x.(*promql.Sample)))
}

func (s *vectorByValueHeap) Pop() interface{} {
	old := *s
	n := len(old)
	el := old[n-1]
	*s = old[0 : n-1]
	return el
}

type vectorByReverseValueHeap promql.Vector

func (s vectorByReverseValueHeap) Len() int {
	return len(s)
}

func (s vectorByReverseValueHeap) Less(i, j int) bool {
	if math.IsNaN(s[i].F) {
		return true
	}
	return s[i].F > s[j].F
}

func (s vectorByReverseValueHeap) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s *vectorByReverseValueHeap) Push(x interface{}) {
	*s = append(*s, *(x.(*promql.Sample)))
}

func (s *vectorByReverseValueHeap) Pop() interface{} {
	old := *s
	n := len(old)
	el := old[n-1]
	*s = old[0 : n-1]
	return el
}
