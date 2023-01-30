package vector

import (
	"math"

	"github.com/prometheus/prometheus/promql"
)

type HeapByMaxValue promql.Vector

func (s HeapByMaxValue) Len() int {
	return len(s)
}

func (s HeapByMaxValue) Less(i, j int) bool {
	if math.IsNaN(s[i].V) {
		return true
	}
	return s[i].V < s[j].V
}

func (s HeapByMaxValue) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s *HeapByMaxValue) Push(x interface{}) {
	*s = append(*s, *(x.(*promql.Sample)))
}

func (s *HeapByMaxValue) Pop() interface{} {
	old := *s
	n := len(old)
	el := old[n-1]
	*s = old[0 : n-1]
	return el
}

type HeapByMinValue promql.Vector

func (s HeapByMinValue) Len() int {
	return len(s)
}

func (s HeapByMinValue) Less(i, j int) bool {
	if math.IsNaN(s[i].V) {
		return true
	}
	return s[i].V > s[j].V
}

func (s HeapByMinValue) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s *HeapByMinValue) Push(x interface{}) {
	*s = append(*s, *(x.(*promql.Sample)))
}

func (s *HeapByMinValue) Pop() interface{} {
	old := *s
	n := len(old)
	el := old[n-1]
	*s = old[0 : n-1]
	return el
}
