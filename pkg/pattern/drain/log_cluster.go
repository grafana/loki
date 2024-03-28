package drain

import (
	"sort"
	"strings"

	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/pattern/iter"
)

type LogCluster struct {
	id       int
	Size     int
	Tokens   []string
	Stringer func([]string) string
	Volume   Volume
}

func (c *LogCluster) String() string {
	if c.Stringer != nil {
		return c.Stringer(c.Tokens)
	}
	return strings.Join(c.Tokens, " ")
}

func (c *LogCluster) append(ts model.Time) {
	c.Volume.Add(ts)
}

func (c *LogCluster) Iterator(from, through model.Time) iter.Iterator {
	return iter.NewSlice(c.String(), c.Volume.ForRange(from, through).Values)
}

func truncateTimestamp(ts model.Time) model.Time { return ts - ts%timeResolution }

type Volume struct {
	Values []logproto.PatternSample
}

func initVolume(ts model.Time) Volume {
	v := Volume{Values: make([]logproto.PatternSample, 1, defaultVolumeSize)}
	v.Values[0] = logproto.PatternSample{
		Timestamp: ts,
		Value:     1,
	}
	return v
}

// ForRange returns a new Volume with only the values
// in the given range [start:end).
// start and end are in milliseconds since epoch.
func (x *Volume) ForRange(start, end model.Time) *Volume {
	if len(x.Values) == 0 {
		// Should not be the case.
		return new(Volume)
	}
	first := x.Values[0].Timestamp
	last := x.Values[len(x.Values)-1].Timestamp
	if start >= end || first >= end || last < start {
		return new(Volume)
	}
	var lo int
	if start > first {
		lo = sort.Search(len(x.Values), func(i int) bool {
			return x.Values[i].Timestamp >= start
		})
	}
	hi := len(x.Values)
	if end < last {
		hi = sort.Search(len(x.Values), func(i int) bool {
			return x.Values[i].Timestamp >= end
		})
	}
	return &Volume{
		Values: x.Values[lo:hi],
	}
}

func (x *Volume) Matches() int64 {
	var m int64
	for i := range x.Values {
		m += int64(x.Values[i].Value)
	}
	return m
}

func (x *Volume) Add(ts model.Time) {
	t := truncateTimestamp(ts)
	first := x.Values[0].Timestamp // can't be empty
	last := x.Values[len(x.Values)-1].Timestamp
	switch {
	case last == t:
		// Should be the most common case.
		x.Values[len(x.Values)-1].Value++
	case first > t:
		// Prepend.
		x.Values = slices.Grow(x.Values, 1)
		copy(x.Values[1:], x.Values)
		x.Values[0] = logproto.PatternSample{Timestamp: t, Value: 1}
	case last < t:
		// Append.
		x.Values = append(x.Values, logproto.PatternSample{Timestamp: t, Value: 1})
	default:
		// Find with binary search and update.
		index := sort.Search(len(x.Values), func(i int) bool {
			return x.Values[i].Timestamp >= t
		})
		if index < len(x.Values) && x.Values[index].Timestamp == t {
			x.Values[index].Value++
		} else {
			x.Values = slices.Insert(x.Values, index, logproto.PatternSample{Timestamp: t, Value: 1})
		}
	}
}
