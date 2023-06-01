package sketch

import (
	"github.com/stretchr/testify/assert"
	"math"
	"math/rand"
	"strconv"
	"testing"
)

func TestLHHTopK(t *testing.T) {
	events := make([]event, 0)
	total := 0
	// lets insert 0-9 some amount of times between 1-5 times
	for i := 0; i < 10; i++ {
		n := rand.Intn(5) + 1
		events = append(events, event{name: strconv.Itoa(i), count: n})
		total = total + n
	}
	// then another set of things more than 5 times
	for i := 10; i < 13; i++ {
		n := rand.Intn(6) + 10
		events = append(events, event{name: strconv.Itoa(i), count: n})
		total = total + n

	}
	topk, err := NewLHHTopk(3, total)
	assert.NoError(t, err, "error creating topk")
	for _, e := range events {
		for i := 0; i < e.count; i++ {
			topk.Observe(e.name)
		}
	}
	prev := int64(math.MaxInt64)
	for _, e := range topk.TopK() {
		// there's a better way to do this check
		assert.Condition(t, func() bool { return e.Event == "10" || e.Event == "11" || e.Event == "12" }, "unexpected event in topk: %s", e.Event)
		assert.LessOrEqual(t, e.Count, prev, "topk was returned in unsorted order")
		prev = e.Count
	}
}

func TestLHHTopK_Merge(t *testing.T) {
	var topk1, topk2 LHHTopK
	var err error

	// each thing is going to take in at most 50 + 45 things so lets say total = 200
	total := 200
	// populate the first topk sketch
	{
		events := make([]event, 0)

		// lets insert 0-9 some amount of times between 1-5 times
		for i := 0; i < 10; i++ {
			n := rand.Intn(5) + 1
			events = append(events, event{name: strconv.Itoa(i), count: n})
		}
		// then another set of things more than 5 times
		for i := 10; i < 13; i++ {
			n := rand.Intn(6) + 10
			events = append(events, event{name: strconv.Itoa(i), count: n})
		}

		topk1, err = NewLHHTopk(3, total)
		assert.NoError(t, err, "error creating topk")
		for _, e := range events {
			for i := 0; i < e.count; i++ {
				topk1.Observe(e.name)
			}
		}
	}
	// populate the second topk sketch
	{
		events := make([]event, 0)

		// lets insert 0-9 some amount of times between 1-5 times
		for i := 0; i < 10; i++ {
			n := rand.Intn(5) + 1
			events = append(events, event{name: strconv.Itoa(i), count: n})
		}
		// then another set of things more than 5 times
		for i := 10; i < 13; i++ {
			n := rand.Intn(6) + 10
			events = append(events, event{name: strconv.Itoa(i), count: n})
		}

		// one final insert that has more than 10-12 will have
		events = append(events, event{name: "13", count: 40})

		topk2, err = NewLHHTopk(3, total)
		assert.NoError(t, err, "error creating topk")
		for _, e := range events {
			for i := 0; i < e.count; i++ {
				topk2.Observe(e.name)
			}
		}
	}

	assert.NoError(t, topk1.Merge(topk2))
	prev := int64(math.MaxInt64)
	for _, e := range topk1.TopK() {
		// there's a better way to do this check
		assert.Condition(t, func() bool { return e.Event == "10" || e.Event == "11" || e.Event == "12" || e.Event == "13" }, "unexpected event in topk", e.Event)
		assert.LessOrEqual(t, e.Count, prev, "topk was returned in unsorted order")
		prev = e.Count
	}
	// the highest thing in the merged topk should be 13
	assert.Equal(t, "13", topk1.TopK()[0].Event, "wrong result from merged sketch")
}

func BenchmarkLHHTopK(b *testing.B) {
	names := make([]string, 100000)
	for i := 0; i < len(names); i++ {
		names[i] = RandStringRunes(100)
	}
	for i := 0; i < b.N; i++ {
		total := (len(names)-3)*10 + (3 * 100)
		topk, err := NewLHHTopk(3, total)

		assert.NoError(b, err)
		for i := 0; i < len(names)-3; i++ {
			for j := 0; j <= 10; j++ {
				topk.Observe(strconv.Itoa(i))
			}
		}
		for i := len(names) - 3; i < len(names); i++ {
			for j := 0; j < 100; j++ {
				topk.Observe(strconv.Itoa(i))
			}
		}
		//b.ReportMetric(float64(size.Of(topk)), "struct_size")
	}
}
