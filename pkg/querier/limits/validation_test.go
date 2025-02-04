package limits

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeTimeLimits struct {
	maxQueryLookback time.Duration
	maxQueryLength   time.Duration
}

func (f fakeTimeLimits) MaxQueryLookback(_ context.Context, _ string) time.Duration {
	return f.maxQueryLookback
}

func (f fakeTimeLimits) MaxQueryLength(_ context.Context, _ string) time.Duration {
	return f.maxQueryLength
}

func Test_validateQueryTimeRangeLimits(t *testing.T) {
	now := time.Now()
	nowFunc = func() time.Time { return now }
	tests := []struct {
		name        string
		limits      TimeRangeLimits
		from        time.Time
		through     time.Time
		wantFrom    time.Time
		wantThrough time.Time
		wantErr     bool
	}{
		{"no change", fakeTimeLimits{1000 * time.Hour, 1000 * time.Hour}, now, now.Add(24 * time.Hour), now, now.Add(24 * time.Hour), false},
		{"clamped to 24h", fakeTimeLimits{24 * time.Hour, 1000 * time.Hour}, now.Add(-48 * time.Hour), now, now.Add(-24 * time.Hour), now, false},
		{"end before start", fakeTimeLimits{}, now, now.Add(-48 * time.Hour), time.Time{}, time.Time{}, true},
		{"query too long", fakeTimeLimits{maxQueryLength: 24 * time.Hour}, now.Add(-48 * time.Hour), now, time.Time{}, time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, through, err := ValidateQueryTimeRangeLimits(context.Background(), "foo", tt.limits, tt.from, tt.through)
			if tt.wantErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
			require.Equal(t, tt.wantFrom, from, "wanted (%s) got (%s)", tt.wantFrom, from)
			require.Equal(t, tt.wantThrough, through)
		})
	}
}
