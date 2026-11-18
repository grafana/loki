package logql

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.uber.org/atomic"
)

// errIntermediateSeriesLimit is returned by Reserve when a reservation would take the counter
// over its limit.
var errIntermediateSeriesLimit = errors.New("intermediate series limit exceeded")

// intermediateSeriesCounter counts the label sets the range iterators of one job hold while
// they evaluate. Every iterator in the job's evaluator tree shares the same counter, so both
// legs of a binary operation add up. A nil counter counts nothing and never fails.
type intermediateSeriesCounter struct {
	// limit is zero when the counter is only measuring.
	limit   int64
	current atomic.Int64
	peak    atomic.Int64

	// thresholds are the ascending counts that onCrossing is called for, once each. Both are set
	// before the counter is shared.
	thresholds []int64
	onCrossing func(threshold, current int64)
	// crossed is the number of thresholds onCrossing has been called for.
	crossed atomic.Int32
}

// newIntermediateSeriesCounter returns a counter that fails a reservation taking it over limit.
// A limit of zero means no limit.
func newIntermediateSeriesCounter(limit int) *intermediateSeriesCounter {
	return &intermediateSeriesCounter{limit: int64(limit)}
}

// notifyOnCrossing makes the counter call fn the first time the count reaches each of thresholds,
// and returns the counter. A threshold given more than once counts as one. It must be called
// before the counter is shared. The count falling and rising again does not call fn a second time.
func (c *intermediateSeriesCounter) notifyOnCrossing(thresholds []int, fn func(threshold, current int64)) *intermediateSeriesCounter {
	c.thresholds = make([]int64, 0, len(thresholds))
	for _, t := range thresholds {
		if t > 0 {
			c.thresholds = append(c.thresholds, int64(t))
		}
	}
	slices.Sort(c.thresholds)
	c.thresholds = slices.Compact(c.thresholds)
	c.onCrossing = fn
	return c
}

// notify calls onCrossing for every threshold that cur has reached and that has not been called yet.
func (c *intermediateSeriesCounter) notify(cur int64) {
	for {
		i := c.crossed.Load()
		if int(i) >= len(c.thresholds) || cur < c.thresholds[i] {
			return
		}
		if c.crossed.CompareAndSwap(i, i+1) {
			c.onCrossing(c.thresholds[i], cur)
		}
	}
}

// Reserve adds n label sets to the count and returns errIntermediateSeriesLimit, leaving the
// count unchanged, when that would take it over the limit. The limit is checked before the count
// moves, so a failed reservation is never visible to other goroutines. Callers reserve only once
// the label set is built, so a failed parse never raises the peak.
func (c *intermediateSeriesCounter) Reserve(n int) error {
	if c == nil {
		return nil
	}
	var cur int64
	for {
		prev := c.current.Load()
		cur = prev + int64(n)
		if c.limit > 0 && cur > c.limit {
			return errIntermediateSeriesLimit
		}
		if c.current.CompareAndSwap(prev, cur) {
			break
		}
	}
	if c.onCrossing != nil {
		c.notify(cur)
	}
	for {
		peak := c.peak.Load()
		if cur <= peak || c.peak.CompareAndSwap(peak, cur) {
			return nil
		}
	}
}

// Release removes n label sets from the count, because the iterator that held them discarded them.
func (c *intermediateSeriesCounter) Release(n int) {
	if c == nil {
		return
	}
	c.current.Add(-int64(n))
}

// Current returns the number of label sets held now.
func (c *intermediateSeriesCounter) Current() int64 {
	if c == nil {
		return 0
	}
	return c.current.Load()
}

// Peak returns the largest count Current has had.
func (c *intermediateSeriesCounter) Peak() int64 {
	if c == nil {
		return 0
	}
	return c.peak.Load()
}

type intermediateSeriesCounterKey struct{}

// withIntermediateSeriesCounter returns a context that carries c, so every iterator built for
// the job finds the same counter.
func withIntermediateSeriesCounter(ctx context.Context, c *intermediateSeriesCounter) context.Context {
	return context.WithValue(ctx, intermediateSeriesCounterKey{}, c)
}

// intermediateSeriesCounterFromContext returns the counter of the job ctx belongs to, or nil
// when it has none.
func intermediateSeriesCounterFromContext(ctx context.Context) *intermediateSeriesCounter {
	c, _ := ctx.Value(intermediateSeriesCounterKey{}).(*intermediateSeriesCounter)
	return c
}

// IntermediateSeriesBreadcrumbThresholds is a list of intermediate series counts, set as a
// comma-separated string. A job logs a line the first time its count reaches each of them.
type IntermediateSeriesBreadcrumbThresholds []int

// String implements flag.Value.
func (t IntermediateSeriesBreadcrumbThresholds) String() string {
	s := make([]string, 0, len(t))
	for _, v := range t {
		s = append(s, strconv.Itoa(v))
	}
	return strings.Join(s, ",")
}

// Set implements flag.Value.
func (t *IntermediateSeriesBreadcrumbThresholds) Set(s string) error {
	*t = nil
	if s == "" {
		return nil
	}
	for _, part := range strings.Split(s, ",") {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return fmt.Errorf("invalid intermediate series breadcrumb threshold %q: %w", part, err)
		}
		if v <= 0 {
			return fmt.Errorf("intermediate series breadcrumb threshold must be positive, got %d", v)
		}
		*t = append(*t, v)
	}
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (t *IntermediateSeriesBreadcrumbThresholds) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	return t.Set(s)
}

// MarshalYAML implements yaml.Marshaler.
func (t IntermediateSeriesBreadcrumbThresholds) MarshalYAML() (interface{}, error) {
	return t.String(), nil
}
