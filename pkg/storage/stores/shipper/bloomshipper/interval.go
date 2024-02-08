package bloomshipper

import (
	"fmt"
	"hash"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/util/encoding"
)

// Interval defines a time range with start end end time
// where the start is inclusive, the end is non-inclusive.
type Interval struct {
	Start, End model.Time
}

func NewInterval(start, end model.Time) Interval {
	return Interval{Start: start, End: end}
}

func (i Interval) Hash(h hash.Hash32) error {
	var enc encoding.Encbuf
	enc.PutBE64(uint64(i.Start))
	enc.PutBE64(uint64(i.End))
	_, err := h.Write(enc.Get())
	return errors.Wrap(err, "writing Interval")
}

func (i Interval) String() string {
	// 13 digits are enough until Sat Nov 20 2286 17:46:39 UTC
	return fmt.Sprintf("%013d-%013d", i.Start, i.End)
}

func (i Interval) Repr() string {
	return fmt.Sprintf("[%s, %s)", i.Start.Time().UTC(), i.End.Time().UTC())
}

// Cmp returns the position of a time relative to the interval
func (i Interval) Cmp(ts model.Time) v1.BoundsCheck {
	if ts.Before(i.Start) {
		return v1.Before
	} else if ts.After(i.End) || ts.Equal(i.End) {
		return v1.After
	}
	return v1.Overlap
}

// Overlaps returns whether the interval overlaps (partially) with the target interval
func (i Interval) Overlaps(target Interval) bool {
	return i.Cmp(target.Start) != v1.After && i.Cmp(target.End) != v1.Before
}

// Within returns whether the interval is fully within the target interval
func (i Interval) Within(target Interval) bool {
	return i.Start >= target.Start && i.End <= target.End
}
