package util

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeFromMillis(t *testing.T) {
	var testExpr = []struct {
		input    int64
		expected time.Time
	}{
		{input: 1000, expected: time.Unix(1, 0)},
		{input: 1500, expected: time.Unix(1, 500*nanosecondsInMillisecond)},
	}

	for i, c := range testExpr {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			res := TimeFromMillis(c.input)
			require.Equal(t, c.expected, res)
		})
	}
}

func TestDurationWithJitter(t *testing.T) {
	const numRuns = 1000

	for i := 0; i < numRuns; i++ {
		actual := DurationWithJitter(time.Minute, 0.5)
		assert.GreaterOrEqual(t, int64(actual), int64(30*time.Second))
		assert.LessOrEqual(t, int64(actual), int64(90*time.Second))
	}
}

func TestDurationWithJitter_ZeroInputDuration(t *testing.T) {
	assert.Equal(t, time.Duration(0), DurationWithJitter(time.Duration(0), 0.5))
}

func TestDurationWithPositiveJitter(t *testing.T) {
	const numRuns = 1000

	for i := 0; i < numRuns; i++ {
		actual := DurationWithPositiveJitter(time.Minute, 0.5)
		assert.GreaterOrEqual(t, int64(actual), int64(60*time.Second))
		assert.LessOrEqual(t, int64(actual), int64(90*time.Second))
	}
}

func TestDurationWithPositiveJitter_ZeroInputDuration(t *testing.T) {
	assert.Equal(t, time.Duration(0), DurationWithPositiveJitter(time.Duration(0), 0.5))
}

func TestParseTime(t *testing.T) {
	var tests = []struct {
		input  string
		fail   bool
		result time.Time
	}{
		{
			input: "",
			fail:  true,
		}, {
			input: "abc",
			fail:  true,
		}, {
			input: "30s",
			fail:  true,
		}, {
			input:  "123",
			result: time.Unix(123, 0),
		}, {
			input:  "123.123",
			result: time.Unix(123, 123000000),
		}, {
			input:  "2015-06-03T13:21:58.555Z",
			result: time.Unix(1433337718, 555*time.Millisecond.Nanoseconds()),
		}, {
			input:  "2015-06-03T14:21:58.555+01:00",
			result: time.Unix(1433337718, 555*time.Millisecond.Nanoseconds()),
		}, {
			// Test nanosecond rounding.
			input:  "2015-06-03T13:21:58.56789Z",
			result: time.Unix(1433337718, 567*1e6),
		}, {
			// Test float rounding.
			input:  "1543578564.705",
			result: time.Unix(1543578564, 705*1e6),
		},
	}

	for _, test := range tests {
		ts, err := ParseTime(test.input)
		if test.fail {
			require.Error(t, err)
			continue
		}

		require.NoError(t, err)
		assert.Equal(t, TimeToMillis(test.result), ts)
	}
}

func TestNewDisableableTicker_Enabled(t *testing.T) {
	stop, ch := NewDisableableTicker(10 * time.Millisecond)
	defer stop()

	time.Sleep(100 * time.Millisecond)

	select {
	case <-ch:
		break
	default:
		t.Error("ticker should have ticked when enabled")
	}
}

func TestNewDisableableTicker_Disabled(t *testing.T) {
	stop, ch := NewDisableableTicker(0)
	defer stop()

	time.Sleep(100 * time.Millisecond)

	select {
	case <-ch:
		t.Error("ticker should not have ticked when disabled")
	default:
		break
	}
}

type timeInterval struct {
	from, through time.Time
}

func TestForInterval(t *testing.T) {
	splitInterval := 10 * time.Second
	for _, tc := range []struct {
		name              string
		inp               timeInterval
		expectedIntervals []timeInterval
		endTimeInclusive  bool
	}{
		{
			name: "range smaller than split interval",
			inp: timeInterval{
				from:    time.Unix(5, 0),
				through: time.Unix(8, 0),
			},
			expectedIntervals: []timeInterval{
				{
					from:    time.Unix(5, 0),
					through: time.Unix(8, 0),
				},
			},
		},
		{
			name: "range exactly equal and aligned to split interval",
			inp: timeInterval{
				from:    time.Unix(10, 0),
				through: time.Unix(20, 0),
			},
			expectedIntervals: []timeInterval{
				{
					from:    time.Unix(10, 0),
					through: time.Unix(20, 0),
				},
			},
		},
		{
			name: "multiple splits with end time not inclusive",
			inp: timeInterval{
				from:    time.Unix(5, 0),
				through: time.Unix(28, 0),
			},
			expectedIntervals: []timeInterval{
				{
					from:    time.Unix(5, 0),
					through: time.Unix(10, 0),
				},
				{
					from:    time.Unix(10, 0),
					through: time.Unix(20, 0),
				},
				{
					from:    time.Unix(20, 0),
					through: time.Unix(28, 0),
				},
			},
		},
		{
			name: "multiple splits with end time inclusive",
			inp: timeInterval{
				from:    time.Unix(5, 0),
				through: time.Unix(28, 0),
			},
			endTimeInclusive: true,
			expectedIntervals: []timeInterval{
				{
					from:    time.Unix(5, 0),
					through: time.Unix(10, 0).Add(-time.Millisecond),
				},
				{
					from:    time.Unix(10, 0),
					through: time.Unix(20, 0).Add(-time.Millisecond),
				},
				{
					from:    time.Unix(20, 0),
					through: time.Unix(28, 0),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var actualIntervals []timeInterval
			ForInterval(splitInterval, tc.inp.from, tc.inp.through, tc.endTimeInclusive, func(start, end time.Time) {
				actualIntervals = append(actualIntervals, timeInterval{
					from:    start,
					through: end,
				})
			})

			require.Equal(t, tc.expectedIntervals, actualIntervals)
		})
	}
}

func TestForInterval_OfZero(t *testing.T) {
	var actualIntervals []timeInterval

	from := time.Unix(5, 0)
	through := time.Unix(8, 0)
	endTimeInclusive := true
	ForInterval(0, from, through, endTimeInclusive, func(start, end time.Time) {
		actualIntervals = append(actualIntervals, timeInterval{
			from:    start,
			through: end,
		})
	})

	require.Equal(t, []timeInterval{
		{
			from:    time.Unix(5, 0),
			through: time.Unix(8, 0),
		},
	}, actualIntervals)
}

func TestGetFactorOfTime(t *testing.T) {
	for _, tc := range []struct {
		desc                                string
		from, through, extentMin, extentMax int64
		exp                                 float64
	}{
		{
			desc: "equal",
			from: 10, through: 20,
			extentMin: 10, extentMax: 20,
			exp: 1,
		},
		{
			desc: "50% overlap on left",
			from: 10, through: 20,
			extentMin: 5, extentMax: 15,
			exp: 0.5,
		},
		{
			desc: "50% overlap on right",
			from: 10, through: 20,
			extentMin: 15, extentMax: 25,
			exp: 0.5,
		},
		{
			desc: "10% overlap on right",
			from: 15, through: 16,
			extentMin: 15, extentMax: 25,
			exp: 0.1,
		},
		{
			desc: "no overlap",
			from: 10, through: 20,
			extentMin: 25, extentMax: 35,
			exp: 0,
		},
		{
			desc: "no overlap, through=extentMin",
			from: 10, through: 20,
			extentMin: 20, extentMax: 35,
			exp: 0,
		},
		{
			desc: "factor would be NaN",
			from: 1685655637000000000, through: 1685656237000000000,
			extentMin: 1685656107442496000, extentMax: 1685656107442496000,
			exp: 1,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			factor := GetFactorOfTime(tc.from, tc.through, tc.extentMin, tc.extentMax)
			require.Equal(t, tc.exp, factor)
		})
	}
}

func TestRetentionHours(t *testing.T) {
	require.Equal(t, "24", RetentionHours(24*time.Hour))
	sevenDays, err := model.ParseDuration("7d")
	require.NoError(t, err)
	require.Equal(t, "168", RetentionHours(time.Duration(sevenDays)))
}
