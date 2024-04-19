package bloomshipper

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

func Test_Interval_String(t *testing.T) {
	start := model.Time(0)
	end := model.TimeFromUnix(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	interval := NewInterval(start, end)
	assert.Equal(t, "0000000000000-1704067200000", interval.String())
	assert.Equal(t, "[1970-01-01 00:00:00 +0000 UTC, 2024-01-01 00:00:00 +0000 UTC)", interval.Repr())
}

func Test_Interval_Cmp(t *testing.T) {
	interval := NewInterval(10, 20)
	assert.Equal(t, v1.Before, interval.Cmp(0))
	assert.Equal(t, v1.Overlap, interval.Cmp(10))
	assert.Equal(t, v1.Overlap, interval.Cmp(15))
	assert.Equal(t, v1.After, interval.Cmp(20)) // End is not inclusive
	assert.Equal(t, v1.After, interval.Cmp(21))
}

func Test_Interval_Overlap(t *testing.T) {
	interval := NewInterval(10, 20)
	assert.True(t, interval.Overlaps(Interval{Start: 5, End: 15}))
	assert.True(t, interval.Overlaps(Interval{Start: 15, End: 25}))
	assert.True(t, interval.Overlaps(Interval{Start: 10, End: 20}))
	assert.True(t, interval.Overlaps(Interval{Start: 5, End: 25}))
	assert.False(t, interval.Overlaps(Interval{Start: 1, End: 9}))
	assert.False(t, interval.Overlaps(Interval{Start: 20, End: 30})) // End is not inclusive
	assert.False(t, interval.Overlaps(Interval{Start: 25, End: 30}))
}

func Test_Interval_Within(t *testing.T) {
	target := NewInterval(10, 20)
	assert.False(t, NewInterval(1, 9).Within(target))
	assert.False(t, NewInterval(21, 30).Within(target))
	assert.True(t, NewInterval(10, 20).Within(target))
	assert.True(t, NewInterval(14, 15).Within(target))
	assert.False(t, NewInterval(5, 15).Within(target))
	assert.False(t, NewInterval(15, 25).Within(target))
	assert.False(t, NewInterval(5, 25).Within(target))
}
