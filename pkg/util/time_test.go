package util

import (
	"fmt"
	"testing"
	"time"

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
