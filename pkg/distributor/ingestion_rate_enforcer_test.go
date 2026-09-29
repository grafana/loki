package distributor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	throttler "github.com/spiridonov/deadhorse"

	"github.com/grafana/loki/v3/pkg/validation"
)

var errBoom = errors.New("boom")

// fakeThrottleCaller is a throttleCaller double keyed by RequestEntry.Key, so tests don't
// depend on the (random) map iteration order enforce() walks rlBuckets in -- entries and
// results stay positionally aligned by construction inside enforce(), but the test only needs
// to know which *outcome* to hand back for which key.
type fakeThrottleCaller struct {
	byKey      map[string]throttler.ResponseEntry
	err        error
	gotEntries []throttler.RequestEntry
}

func (f *fakeThrottleCaller) Throttle(_ context.Context, _ string, entries []throttler.RequestEntry) ([]throttler.ResponseEntry, error) {
	f.gotEntries = entries
	results := make([]throttler.ResponseEntry, len(entries))
	for i, e := range entries {
		results[i] = f.byKey[e.Key]
	}
	return results, f.err
}

func TestThrottlerEnforcer_Enforce(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{
		IngestionRateMB:      1.0,
		IngestionBurstSizeMB: 2.0,
		PolicyOverrideLimits: map[string]validation.PolicyOverridableLimits{
			"premium": {IngestionRateMB: ptr(5.0), IngestionBurstSizeMB: ptr(10.0)},
		},
	}, nil)
	require.NoError(t, err)

	tenantWideKey := "t1"
	policyKey := encodeRateLimitKey("t1", "premium")

	buckets := func() map[string]*rateLimitBucket {
		return map[string]*rateLimitBucket{
			"":        {policy: "", hasOverride: false, bytes: 100, lines: 1},
			"premium": {policy: "premium", hasOverride: true, bytes: 200, lines: 2},
		}
	}

	t.Run("all admitted", func(t *testing.T) {
		caller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
			tenantWideKey: {Key: tenantWideKey, Throttled: false},
			policyKey:     {Key: policyKey, Throttled: false},
		}}
		e := newThrottlerEnforcer(limits, caller)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets())
		require.NoError(t, err)
		require.Empty(t, exceeded)

		// Sanity check the entries actually sent: cost is the bucket's bytes, and the
		// policy override's rate/burst -- not the tenant's -- shaped its Limit.
		require.Len(t, caller.gotEntries, 2)
		for _, entry := range caller.gotEntries {
			if entry.Key == policyKey {
				require.EqualValues(t, 200, entry.Cost)
				require.EqualValues(t, int(10.0*float64(bytesInMB)), entry.Limit.Capacity)
			} else {
				require.EqualValues(t, 100, entry.Cost)
				require.EqualValues(t, int(2.0*float64(bytesInMB)), entry.Limit.Capacity)
			}
		}
	})

	t.Run("only the individually-exceeded bucket is attributed, not the whole group", func(t *testing.T) {
		// The group is denied (Throttled=true on both) because the policy bucket is over --
		// but the tenant-wide bucket's own RetryAfter is 0, meaning it individually had room.
		caller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
			tenantWideKey: {Key: tenantWideKey, Throttled: true, RetryAfter: 0},
			policyKey:     {Key: policyKey, Throttled: true, RetryAfter: 5 * time.Millisecond},
		}}
		e := newThrottlerEnforcer(limits, caller)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets())
		require.NoError(t, err)
		require.Len(t, exceeded, 1)
		require.Equal(t, "premium", exceeded[0].policy)
	})

	t.Run("group denied with no isolating RetryAfter attributes the whole group", func(t *testing.T) {
		caller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
			tenantWideKey: {Key: tenantWideKey, Throttled: true, RetryAfter: 0},
			policyKey:     {Key: policyKey, Throttled: true, RetryAfter: 0},
		}}
		e := newThrottlerEnforcer(limits, caller)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets())
		require.NoError(t, err)
		require.Len(t, exceeded, 2)
	})

	t.Run("a network error alone does not reject the request (fail-open is the caller's job)", func(t *testing.T) {
		caller := &fakeThrottleCaller{
			byKey: map[string]throttler.ResponseEntry{
				tenantWideKey: {Key: tenantWideKey, Throttled: false},
				policyKey:     {Key: policyKey, Throttled: false},
			},
			err: errBoom,
		}
		e := newThrottlerEnforcer(limits, caller)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets())
		require.Error(t, err) // diagnostic, surfaced to the caller for logging
		require.Empty(t, exceeded)
	})

	t.Run("no buckets is a no-op", func(t *testing.T) {
		caller := &fakeThrottleCaller{}
		e := newThrottlerEnforcer(limits, caller)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", map[string]*rateLimitBucket{})
		require.NoError(t, err)
		require.Empty(t, exceeded)
		require.Nil(t, caller.gotEntries) // never even called
	})
}

func TestThrottlerEnforcer_Limit(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{
		IngestionRateMB: 1.0,
		PolicyOverrideLimits: map[string]validation.PolicyOverridableLimits{
			"premium": {IngestionRateMB: ptr(5.0)},
		},
	}, nil)
	require.NoError(t, err)

	e := newThrottlerEnforcer(limits, &fakeThrottleCaller{})

	require.Equal(t, int(1.0*float64(bytesInMB)), e.limit(time.Now(), "t1", &rateLimitBucket{}))
	require.Equal(t, int(5.0*float64(bytesInMB)), e.limit(time.Now(), "t1", &rateLimitBucket{policy: "premium", hasOverride: true}))
}
