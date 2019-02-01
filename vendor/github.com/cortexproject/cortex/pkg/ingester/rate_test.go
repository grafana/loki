package ingester

import (
	"testing"
	"time"
)

func TestRate(t *testing.T) {
	ticks := []struct {
		events int
		want   float64
	}{
		{60, 1},
		{30, 0.9},
		{0, 0.72},
		{60, 0.776},
		{0, 0.6208},
		{0, 0.49664},
		{0, 0.397312},
		{0, 0.3178496},
		{0, 0.25427968},
		{0, 0.20342374400000002},
		{0, 0.16273899520000001},
	}
	r := newEWMARate(0.2, time.Minute)

	for i, tick := range ticks {
		for e := 0; e < tick.events; e++ {
			r.inc()
		}
		r.tick()
		if r.rate() != tick.want {
			t.Fatalf("%d. unexpected rate: want %v, got %v", i, tick.want, r.rate())
		}
	}
}
