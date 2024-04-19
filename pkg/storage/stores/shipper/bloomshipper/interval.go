package bloomshipper

import (
	"fmt"
	"hash"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/util/encoding"
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

// ParseBoundsFromAddr parses a fingerprint bounds from a string
// Does not support negative times (times prior to Unix epoch).
func ParseIntervalFromAddr(s string) (Interval, error) {
	parts := strings.Split(s, "-")
	return ParseIntervalFromParts(parts[0], parts[1])
}

// ParseIntervalFromParts parses a fingerprint bounds already separated strings
func ParseIntervalFromParts(a, b string) (Interval, error) {
	minTs, err := ParseTime(a)
	if err != nil {
		return Interval{}, fmt.Errorf("error parsing minTimestamp %s : %w", a, err)
	}
	maxTs, err := ParseTime(b)
	if err != nil {
		return Interval{}, fmt.Errorf("error parsing maxTimestamp %s : %w", b, err)
	}
	return NewInterval(minTs, maxTs), nil
}

// ParseFingerprint parses the input string into a model.Time.
func ParseTime(s string) (model.Time, error) {
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return model.Time(num), nil
}
