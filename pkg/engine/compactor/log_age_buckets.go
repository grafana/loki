package compactor

import (
	"strconv"
	"time"

	"github.com/grafana/loki/v4/pkg/dataobj/metastore"
)

// logAgeOverflowLabel is the age label for data older than the span of the
// compacted windows.
const logAgeOverflowLabel = "+Inf"

// logAgeBuckets groups pending log bytes by the age of their oldest data.
//
// The bounds start at one hour and double until they reach the span of the
// compacted windows. The last bound is the span itself. A bucket holds the
// ages above the previous bound, up to and including its own bound, so the
// buckets do not overlap. Ages above the span go to logAgeOverflowLabel.
type logAgeBuckets struct {
	buckets []logAgeBucket
}

// logAgeBucket is one bounded bucket of logAgeBuckets.
type logAgeBucket struct {
	bound time.Duration
	label string
}

// newLogAgeBuckets returns the buckets for a coordinator that compacts the
// current window and windowLookback older windows.
func newLogAgeBuckets(windowLookback int) logAgeBuckets {
	span := time.Duration(windowLookback+1) * metastore.MetastoreWindowSize
	var b logAgeBuckets
	for bound := time.Hour; ; bound *= 2 {
		bound = min(bound, span)
		b.buckets = append(b.buckets, logAgeBucket{
			bound: bound,
			label: strconv.FormatInt(int64(bound/time.Hour), 10) + "h",
		})
		if bound == span {
			break
		}
	}
	return b
}

// label returns the age label of data whose oldest timestamp is oldest, at
// now. Data with a timestamp after now goes to the first bucket.
func (b logAgeBuckets) label(oldest, now time.Time) string {
	age := now.Sub(oldest)
	for _, bucket := range b.buckets {
		if age <= bucket.bound {
			return bucket.label
		}
	}
	return logAgeOverflowLabel
}

// labels returns every age label, from the youngest bucket to
// logAgeOverflowLabel.
func (b logAgeBuckets) labels() []string {
	labels := make([]string, 0, len(b.buckets)+1)
	for _, bucket := range b.buckets {
		labels = append(labels, bucket.label)
	}
	return append(labels, logAgeOverflowLabel)
}
