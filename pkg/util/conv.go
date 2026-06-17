package util //nolint:revive

import (
	"time"

	"github.com/prometheus/common/model"
)

// RoundToMilliseconds returns milliseconds precision time from nanoseconds.
// from will be rounded down to the nearest milliseconds while through is rounded up.
func RoundToMilliseconds(from, through time.Time) (model.Time, model.Time) {
	fromMs := from.UnixNano() / int64(time.Millisecond)
	throughMs := through.UnixNano() / int64(time.Millisecond)

	// add a millisecond to the through time if the nanosecond offset within the second is not a multiple of milliseconds
	if int64(through.Nanosecond())%int64(time.Millisecond) != 0 {
		throughMs++
	}

	return model.Time(fromMs), model.Time(throughMs)
}
