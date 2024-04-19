package retention

import (
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
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

func (f fakeLimits) DefaultLimits() *validation.Limits {
	return f.defaultLimit.convertToValidationLimit()
}

func (f fakeLimits) AllByUserID() map[string]*validation.Limits {
	res := make(map[string]*validation.Limits)
	for userID, ret := range f.perTenant {
		res[userID] = ret.convertToValidationLimit()
	}
	return res
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}

func overridesTestConfig(defaultLimits validation.Limits, tenantLimits validation.TenantLimits) (*validation.Overrides, error) {
	return validation.NewOverrides(defaultLimits, tenantLimits)
}

type fakeOverrides struct {
	tenantLimits map[string]*validation.Limits
}

func (f fakeOverrides) TenantLimits(userID string) *validation.Limits {
	return f.tenantLimits[userID]
}

func (f fakeOverrides) AllByUserID() map[string]*validation.Limits {
	//TODO implement me
	panic("implement me")
}

func Test_expirationChecker_Expired(t *testing.T) {
	// Set a default retention of 0 which should disable it
	dur, _ := model.ParseDuration("0s")
	d := defaultLimitsTestConfig()
	d.RetentionPeriod = dur

	// Override tenant 1 and tenant 2
	t1 := defaultLimitsTestConfig()
	t1.RetentionPeriod = model.Duration(time.Hour)
	t1.StreamRetention = []validation.StreamRetention{
		{Period: model.Duration(2 * time.Hour), Priority: 10, Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}},
		{Period: model.Duration(2 * time.Hour), Priority: 1, Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.+")}},
	}
	t2 := defaultLimitsTestConfig()
	t2.RetentionPeriod = model.Duration(24 * time.Hour)
	t2.StreamRetention = []validation.StreamRetention{
		{Period: model.Duration(1 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}},
		{Period: model.Duration(2 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.")}},
	}

	f := fakeOverrides{
		tenantLimits: map[string]*validation.Limits{
			"1": &t1,
			"2": &t2,
		},
	}
	o, err := overridesTestConfig(d, f)
	require.NoError(t, err)

	e := NewExpirationChecker(o)
	tests := []struct {
		name string
		ref  ChunkEntry
		want bool
	}{
		{"expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-2*time.Hour)), true},
		{"just expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-1*time.Hour+(10*time.Millisecond))), false},
		{"not expired tenant", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-30*time.Minute)), false},
		{"not expired tenant by far", newChunkEntry("2", `{foo="buzz"}`, model.Now().Add(-72*time.Hour), model.Now().Add(-3*time.Hour)), false},
		{"expired stream override", newChunkEntry("2", `{foo="bar"}`, model.Now().Add(-12*time.Hour), model.Now().Add(-10*time.Hour)), true},
		{"non expired stream override", newChunkEntry("1", `{foo="bar"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-90*time.Minute)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, nonDeletedIntervalFilters := e.Expired(tt.ref, model.Now())
			require.Equal(t, tt.want, actual)
			require.Nil(t, nonDeletedIntervalFilters)
		})
	}
}

func Test_expirationChecker_Expired_zeroValue(t *testing.T) {

	// Default retention should be zero
	d := defaultLimitsTestConfig()

	// Override tenant 2 to have 24 hour retention
	tl := defaultLimitsTestConfig()
	dur, _ := model.ParseDuration("24h")
	tl.RetentionPeriod = dur
	f := fakeOverrides{
		tenantLimits: map[string]*validation.Limits{
			"2": &tl,
		},
	}
	o, err := overridesTestConfig(d, f)
	require.NoError(t, err)
	e := NewExpirationChecker(o)
	tests := []struct {
		name string
		ref  ChunkEntry
		want bool
	}{
		{"tenant with no override should not delete", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-2*time.Hour)), false},
		{"tenant with no override, REALLY old chunk should not delete", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-10000*time.Hour+(1*time.Hour)), model.Now().Add(-10000*time.Hour)), false},
		{"tenant with override chunk less than retention should not delete", newChunkEntry("2", `{foo="buzz"}`, model.Now().Add(-3*time.Hour), model.Now().Add(-2*time.Hour)), false},
		{"tenant with override should delete", newChunkEntry("2", `{foo="buzz"}`, model.Now().Add(-31*time.Hour), model.Now().Add(-30*time.Hour)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, nonDeletedIntervalFilters := e.Expired(tt.ref, model.Now())
			require.Equal(t, tt.want, actual)
			require.Nil(t, nonDeletedIntervalFilters)
		})
	}
}

func Test_expirationChecker_Expired_zeroValueOverride(t *testing.T) {

	// Set a default retention of 24h
	dur, _ := model.ParseDuration("24h")
	d := defaultLimitsTestConfig()
	d.RetentionPeriod = dur

	// Override tenant 2 to have zero value for retention from default
	t2 := defaultLimitsTestConfig()
	t2.RetentionPeriod = 0

	// Override tenant 3 to have zero value without unit
	t3 := defaultLimitsTestConfig()
	dur, _ = model.ParseDuration("0")
	t3.RetentionPeriod = dur

	f := fakeOverrides{
		tenantLimits: map[string]*validation.Limits{
			"2": &t2,
			"3": &t3,
		},
	}

	o, err := overridesTestConfig(d, f)
	require.NoError(t, err)

	e := NewExpirationChecker(o)
	tests := []struct {
		name string
		ref  ChunkEntry
		want bool
	}{
		{"tenant with no override should delete", newChunkEntry("1", `{foo="buzz"}`, model.Now().Add(-31*time.Hour), model.Now().Add(-30*time.Hour)), true},
		{"tenant with override should not delete", newChunkEntry("2", `{foo="buzz"}`, model.Now().Add(-31*time.Hour), model.Now().Add(-30*time.Hour)), false},
		{"tenant with zero value without unit should not delete", newChunkEntry("3", `{foo="buzz"}`, model.Now().Add(-31*time.Hour), model.Now().Add(-30*time.Hour)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, nonDeletedIntervalFilters := e.Expired(tt.ref, model.Now())
			require.Equal(t, tt.want, actual)
			require.Nil(t, nonDeletedIntervalFilters)
		})
	}
}

func Test_expirationChecker_DropFromIndex_zeroValue(t *testing.T) {
	// Default retention should be zero
	d := defaultLimitsTestConfig()

	// Override tenant 2 to have 24 hour retention
	tl := defaultLimitsTestConfig()
	dur, _ := model.ParseDuration("24h")
	tl.RetentionPeriod = dur
	f := fakeOverrides{
		tenantLimits: map[string]*validation.Limits{
			"2": &tl,
		},
	}
	o, err := overridesTestConfig(d, f)
	require.NoError(t, err)
	e := NewExpirationChecker(o)

	chunkFrom := model.Now().Add(-3 * time.Hour)
	chunkThrough := model.Now().Add(-2 * time.Hour)
	tests := []struct {
		name         string
		ref          ChunkEntry
		tableEndTime model.Time
		want         bool
	}{
		{"tenant with no override should not delete", newChunkEntry("1", `{foo="buzz"}`, chunkFrom, chunkThrough), model.Now().Add(-48 * time.Hour), false},
		{"tenant with override tableEndTime within retention period should not delete", newChunkEntry("2", `{foo="buzz"}`, chunkFrom, chunkThrough), model.Now().Add(-1 * time.Hour), false},
		{"tenant with override should delete", newChunkEntry("2", `{foo="buzz"}`, chunkFrom, chunkThrough), model.Now().Add(-48 * time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := e.DropFromIndex(tt.ref, tt.tableEndTime, model.Now())
			require.Equal(t, tt.want, actual)
		})
	}
}

func TestFindLatestRetentionStartTime(t *testing.T) {
	const dayDuration = 24 * time.Hour
	now := model.Now()
	for _, tc := range []struct {
		name                             string
		limit                            fakeLimits
		expectedLatestRetentionStartTime latestRetentionStartTime
	}{
		{
			name: "only default retention set",
			limit: fakeLimits{
				defaultLimit: retentionLimit{
					retentionPeriod: 7 * dayDuration,
				},
			},
			expectedLatestRetentionStartTime: latestRetentionStartTime{
				overall:  now.Add(-7 * dayDuration),
				defaults: now.Add(-7 * dayDuration),
				byUser:   map[string]model.Time{},
			},
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
			expectedLatestRetentionStartTime: latestRetentionStartTime{
				overall:  now.Add(-7 * dayDuration),
				defaults: now.Add(-7 * dayDuration),
				byUser: map[string]model.Time{
					"0": now.Add(-12 * dayDuration),
					"1": now.Add(-15 * dayDuration),
				},
			},
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
			expectedLatestRetentionStartTime: latestRetentionStartTime{
				overall:  now.Add(-3 * dayDuration),
				defaults: now.Add(-3 * dayDuration),
				byUser: map[string]model.Time{
					"0": now.Add(-7 * dayDuration),
					"1": now.Add(-5 * dayDuration),
				},
			},
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
			expectedLatestRetentionStartTime: latestRetentionStartTime{
				overall:  now.Add(-5 * dayDuration),
				defaults: now.Add(-7 * dayDuration),
				byUser: map[string]model.Time{
					"0": now.Add(-10 * dayDuration),
					"1": now.Add(-5 * dayDuration),
				},
			},
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
			expectedLatestRetentionStartTime: latestRetentionStartTime{
				overall:  now.Add(-2 * dayDuration),
				defaults: now.Add(-7 * dayDuration),
				byUser: map[string]model.Time{
					"0": now.Add(-10 * dayDuration),
					"1": now.Add(-2 * dayDuration),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			latestRetentionStartTime := findLatestRetentionStartTime(now, tc.limit)
			require.Equal(t, tc.expectedLatestRetentionStartTime, latestRetentionStartTime)
		})
	}
}

func TestExpirationChecker_IntervalMayHaveExpiredChunks(t *testing.T) {
	now := model.Now()
	expirationChecker := expirationChecker{
		latestRetentionStartTime: latestRetentionStartTime{
			overall:  now.Add(-24 * time.Hour),
			defaults: now.Add(-48 * time.Hour),
			byUser: map[string]model.Time{
				"user0": now.Add(-72 * time.Hour),
				"user1": now.Add(-24 * time.Hour),
			},
		},
	}

	for _, tc := range []struct {
		name             string
		userID           string
		interval         model.Interval
		hasExpiredChunks bool
	}{
		// common index using overallLatestRetentionStartTime
		{
			name: "common index - not expired",
			interval: model.Interval{
				Start: now.Add(-23 * time.Hour),
				End:   now,
			},
		},
		{
			name: "common index - partially expired",
			interval: model.Interval{
				Start: now.Add(-25 * time.Hour),
				End:   now.Add(-22 * time.Hour),
			},
			hasExpiredChunks: true,
		},
		{
			name: "common index - fully expired",
			interval: model.Interval{
				Start: now.Add(-26 * time.Hour),
				End:   now.Add(-25 * time.Hour),
			},
			hasExpiredChunks: true,
		},

		// user0 having custom retention
		{
			name:   "user0 index - not expired",
			userID: "user0",
			interval: model.Interval{
				Start: now.Add(-71 + time.Hour),
				End:   now,
			},
		},
		{
			name:   "user0 index - partially expired",
			userID: "user0",
			interval: model.Interval{
				Start: now.Add(-73 * time.Hour),
				End:   now.Add(-71 * time.Hour),
			},
			hasExpiredChunks: true,
		},
		{
			name:   "user0 index - fully expired",
			userID: "user0",
			interval: model.Interval{
				Start: now.Add(-74 * time.Hour),
				End:   now.Add(-73 * time.Hour),
			},
			hasExpiredChunks: true,
		},

		// user3 not having custom retention so using defaultLatestRetentionStartTime
		{
			name:   "user3 index - not expired",
			userID: "user3",
			interval: model.Interval{
				Start: now.Add(-47 * time.Hour),
				End:   now,
			},
		},
		{
			name:   "user3 index - partially expired",
			userID: "user3",
			interval: model.Interval{
				Start: now.Add(-49 * time.Hour),
				End:   now.Add(-47 * time.Hour),
			},
			hasExpiredChunks: true,
		},
		{
			name:   "user3 index - fully expired",
			userID: "user3",
			interval: model.Interval{
				Start: now.Add(-50 * time.Hour),
				End:   now.Add(-49 * time.Hour),
			},
			hasExpiredChunks: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.hasExpiredChunks, expirationChecker.IntervalMayHaveExpiredChunks(tc.interval, tc.userID))
		})
	}
}
