package sketch

import (
	"math/rand"
	"strconv"
	"testing"
)

func TestCMS(_ *testing.T) {
	type event struct {
		name  string
		count int
	}

	cms, _ := NewCountMinSketch(10, 4)

	k := 1
	numStreams := 10
	maxPerStream := 100
	events := make([]event, 0)
	max := int64(0)

	for j := 0; j < numStreams-k; j++ {
		num := int64(maxPerStream)
		n := rand.Int63n(num) + 1
		if n > max {
			max = n
		}
		for z := 0; z < int(n); z++ {
			events = append(events, event{name: strconv.Itoa(j), count: 1})
		}
	}
	// then another set of things more than the max of the previous entries
	for z := numStreams - k; z < numStreams; z++ {
		n := rand.Int63n(int64(maxPerStream)) + 1 + max
		for x := 0; x < int(n); x++ {
			events = append(events, event{name: strconv.Itoa(z), count: 1})
		}
	}

	r := rand.New(rand.NewSource(42))
	r.Shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })

	for _, e := range events {
		for i := 0; i < e.count; i++ {
			cms.ConservativeIncrement(unsafeGetBytes(e.name))
		}
	}
}
