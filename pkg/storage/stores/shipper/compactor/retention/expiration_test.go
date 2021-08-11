package retention

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/validation"
)

type retentionLimit struct {
	retentionPeriod time.Duration
	streamRetention []validation.StreamRetention
}

func (r retentionLimit) convertToValidationLimit() *validation.Limits {
	return &validation.Limits{
		RetentionPeriod: model.Duration(r.retentionPeriod),
		StreamRetention: r.streamRetention,
	}
}

type fakeLimits struct {
	defaultLimit retentionLimit
	perTenant    map[string]retentionLimit
}

func (f fakeLimits) RetentionPeriod(userID string) time.Duration {
	return f.perTenant[userID].retentionPeriod
}

func (f fakeLimits) StreamRetention(userID string) []validation.StreamRetention {
	return f.perTenant[userID].streamRetention
}

func (f fakeLimits) ForEachTenantLimit(callback validation.ForEachTenantLimitCallback) {
	for userID, limit := range f.perTenant {
		callback(userID, limit.convertToValidationLimit())
	}
}

func (f fakeLimits) DefaultLimits() *validation.Limits {
	return f.defaultLimit.convertToValidationLimit()
}

func Test_expirationChecker_Expired(t *testing.T) {
	e := NewExpirationChecker(&fakeLimits{
		perTenant: map[string]retentionLimit{
			"1": {
				retentionPeriod: time.Hour,
				streamRetention: []validation.StreamRetention{
					{Period: model.Duration(2 * time.Hour), Priority: 10, Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					{Period: model.Duration(2 * time.Hour), Priority: 1, Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.+")}},
				},
			},
			"2": {
				retentionPeriod: 24 * time.Hour,
				streamRetention: []validation.StreamRetention{
					{Period: model.Duration(1 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					{Period: model.Duration(2 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.")}},
				},
			},
		},
	})
	tests := []struct {
		name string
		ref  ChunkEntry
		want bool
	}{
		{"expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-2*time.Hour)), true},
		{"just expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-1*time.Hour+(10*time.Microsecond))), false},
		{"not expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-30*time.Minute)), false},
		{"not expired tenant by far", newChunkEntry("2", `{foo="buzz"}`, model.Now().Add(-72*time.Hour), model.Now().Add(-3*time.Hour)), false},
		{"expired stream override", newChunkEntry("2", `{foo="bar"}`, model.Now().Add(-12*time.Hour), model.Now().Add(-10*time.Hour)), true},
		{"non expired stream override", newChunkEntry("1", `{foo="bar"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-90*time.Minute)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, nonDeletedIntervals := e.Expired(tt.ref, model.Now())
			require.Equal(t, tt.want, actual)
			require.Nil(t, nonDeletedIntervals)
		})
	}
}

func TestFindLatestRetentionStartTime(t *testing.T) {
	const dayDuration = 24 * time.Hour
	for _, tc := range []struct {
		name                               string
		limit                              fakeLimits
		expectedEarliestRetentionStartTime time.Duration
	}{
		{
			name: "only default retention set",
			limit: fakeLimits{
				defaultLimit: retentionLimit{
					retentionPeriod: 7 * dayDuration,
				},
			},
			expectedEarliestRetentionStartTime: 7 * dayDuration,
		},
		{
			name: "default retention period smallest",
			limit: fakeLimits{
				defaultLimit: retentionLimit{
					retentionPeriod: 7 * dayDuration,
					streamRetention: []validation.StreamRetention{
						{
							Period: model.Duration(10 * dayDuration),
						},
					},
				},
				perTenant: map[string]retentionLimit{
					"0": {retentionPeriod: 12 * dayDuration},
					"1": {retentionPeriod: 15 * dayDuration},
				},
			},
			expectedEarliestRetentionStartTime: 7 * dayDuration,
		},
		{
			name: "default stream retention period smallest",
			limit: fakeLimits{
				defaultLimit: retentionLimit{
					retentionPeriod: 7 * dayDuration,
					streamRetention: []validation.StreamRetention{
						{
							Period: model.Duration(3 * dayDuration),
						},
					},
				},
				perTenant: map[string]retentionLimit{
					"0": {retentionPeriod: 7 * dayDuration},
					"1": {retentionPeriod: 5 * dayDuration},
				},
			},
			expectedEarliestRetentionStartTime: 3 * dayDuration,
		},
		{
			name: "user retention retention period smallest",
			limit: fakeLimits{
				defaultLimit: retentionLimit{
					retentionPeriod: 7 * dayDuration,
					streamRetention: []validation.StreamRetention{
						{
							Period: model.Duration(10 * dayDuration),
						},
					},
				},
				perTenant: map[string]retentionLimit{
					"0": {
						retentionPeriod: 20 * dayDuration,
						streamRetention: []validation.StreamRetention{
							{
								Period: model.Duration(10 * dayDuration),
							},
						},
					},
					"1": {
						retentionPeriod: 5 * dayDuration,
						streamRetention: []validation.StreamRetention{
							{
								Period: model.Duration(15 * dayDuration),
							},
						},
					},
				},
			},
			expectedEarliestRetentionStartTime: 5 * dayDuration,
		},
		{
			name: "user stream retention period smallest",
			limit: fakeLimits{
				defaultLimit: retentionLimit{
					retentionPeriod: 7 * dayDuration,
					streamRetention: []validation.StreamRetention{
						{
							Period: model.Duration(10 * dayDuration),
						},
					},
				},
				perTenant: map[string]retentionLimit{
					"0": {
						retentionPeriod: 20 * dayDuration,
						streamRetention: []validation.StreamRetention{
							{
								Period: model.Duration(10 * dayDuration),
							},
						},
					},
					"1": {
						retentionPeriod: 15 * dayDuration,
						streamRetention: []validation.StreamRetention{
							{
								Period: model.Duration(2 * dayDuration),
							},
						},
					},
				},
			},
			expectedEarliestRetentionStartTime: 2 * dayDuration,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedEarliestRetentionStartTime, findSmallestRetentionPeriod(tc.limit))
		})
	}
}

func TestExpirationChecker_IntervalHasExpiredChunks(t *testing.T) {
	for _, tc := range []struct {
		name              string
		expirationChecker expirationChecker
		interval          model.Interval
		hasExpiredChunks  bool
	}{
		{
			name: "not expired",
			expirationChecker: expirationChecker{
				latestRetentionStartTime: model.Now().Add(-24 * time.Hour),
			},
			interval: model.Interval{
				Start: model.Now().Add(-time.Hour),
				End:   model.Now(),
			},
		},
		{
			name: "partially expired",
			expirationChecker: expirationChecker{
				latestRetentionStartTime: model.Now().Add(-24 * time.Hour),
			},
			interval: model.Interval{
				Start: model.Now().Add(-25 * time.Hour),
				End:   model.Now().Add(-22 * time.Hour),
			},
			hasExpiredChunks: true,
		},
		{
			name: "fully expired",
			expirationChecker: expirationChecker{
				latestRetentionStartTime: model.Now().Add(-24 * time.Hour),
			},
			interval: model.Interval{
				Start: model.Now().Add(-26 * time.Hour),
				End:   model.Now().Add(-25 * time.Hour),
			},
			hasExpiredChunks: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.hasExpiredChunks, tc.expirationChecker.IntervalHasExpiredChunks(tc.interval))
		})
	}
}
