package distributor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
		// Throttled is each entry's own check: the tenant-wide bucket had room, so it
		// reports false even though the policy bucket's failure denied the whole call.
		caller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
			tenantWideKey: {Key: tenantWideKey, Throttled: false},
			policyKey:     {Key: policyKey, Throttled: true, RetryAfter: 5 * time.Millisecond},
		}}
		e := newThrottlerEnforcer(limits, caller)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets())
		require.NoError(t, err)
		require.Len(t, exceeded, 1)
		require.Equal(t, "premium", exceeded[0].policy)
	})

	t.Run("a bucket that can never fit is attributed alongside one that is merely over rate", func(t *testing.T) {
		// Throttled with RetryAfter == 0 means the cost exceeds the bucket's burst; it must
		// not be dropped just because it has no retry time.
		caller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
			tenantWideKey: {Key: tenantWideKey, Throttled: true, RetryAfter: 0},
			policyKey:     {Key: policyKey, Throttled: true, RetryAfter: 5 * time.Millisecond},
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

// TestThrottlerEnforcer_HighRateNotTruncated guards against a real incident: a rate expressed
// as "nanoseconds per unit" can't represent anything above 1 byte/ns (1e9 bytes/sec) -- above
// that, the duration truncates to zero, which the throttler treats as a nonsensical Limit and
// fails closed unconditionally, regardless of Capacity. deadhorse.Rate{Units, Period} exists
// specifically to avoid that ceiling; this pins down that Loki actually uses it correctly for a
// tenant whose configured rate exceeds it (4000MB/s, matching the tenant that hit this for
// real).
func TestThrottlerEnforcer_HighRateNotTruncated(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{
		IngestionRateMB:      4000.0,
		IngestionBurstSizeMB: 5000.0,
	}, nil)
	require.NoError(t, err)

	caller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
		"t1": {Key: "t1", Throttled: false},
	}}
	e := newThrottlerEnforcer(limits, caller)

	_, err = e.enforce(context.Background(), time.Now(), "t1", map[string]*rateLimitBucket{"": {bytes: 100, lines: 1}})
	require.NoError(t, err)

	require.Len(t, caller.gotEntries, 1)
	entry := caller.gotEntries[0]
	require.EqualValues(t, int64(4000.0*float64(bytesInMB)), entry.Limit.Rate.Units, "Units must carry the exact configured rate, not a rounded-to-zero duration")
	require.EqualValues(t, time.Second, entry.Limit.Rate.Period)
	require.Greater(t, entry.Limit.Rate.Units, int64(0), "a zero Units value makes the throttler fail closed unconditionally, regardless of Capacity")
}

// fakeIngestionRateEnforcer is a controllable ingestionRateEnforcer double for testing
// shadowEnforcer's composition -- it records what it was called with and returns whatever the
// test configured, independent of whatever the shadow path decides.
type fakeIngestionRateEnforcer struct {
	exceeded []*rateLimitBucket
	err      error
	limitVal int
	called   bool
}

func (f *fakeIngestionRateEnforcer) enforce(_ context.Context, _ time.Time, _ string, _ map[string]*rateLimitBucket) ([]*rateLimitBucket, error) {
	f.called = true
	return f.exceeded, f.err
}

func (f *fakeIngestionRateEnforcer) limit(_ time.Time, _ string, _ *rateLimitBucket) int {
	return f.limitVal
}

func TestShadowEnforcer(t *testing.T) {
	buckets := map[string]*rateLimitBucket{"": {bytes: 100, lines: 1}}

	t.Run("enforce returns the wrapped enforcer's decision even when shadow disagrees", func(t *testing.T) {
		enforcing := &fakeIngestionRateEnforcer{exceeded: nil} // enforcing admits
		shadowCaller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
			"t1": {Key: "t1", Throttled: true, RetryAfter: time.Millisecond}, // shadow would deny
		}}
		limits, err := validation.NewOverrides(validation.Limits{IngestionRateMB: 1.0, IngestionBurstSizeMB: 2.0}, nil)
		require.NoError(t, err)

		reg := prometheus.NewPedanticRegistry()
		m := newMetrics(reg)
		e := newShadowEnforcer(enforcing, newThrottlerEnforcer(limits, shadowCaller), m)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets)
		require.NoError(t, err)
		require.Empty(t, exceeded, "enforcing admitted -- the real decision must be admit, regardless of what shadow says")
		require.True(t, enforcing.called)

		require.Eventually(t, func() bool {
			return testutil.ToFloat64(m.exactShadowDecisions.WithLabelValues("t1", "throttled")) == 1
		}, time.Second, time.Millisecond, "shadow's own decision must still be recorded in the metric")
	})

	t.Run("enforce returns throttled from the wrapped enforcer even when shadow would admit", func(t *testing.T) {
		exceededBucket := &rateLimitBucket{}
		enforcing := &fakeIngestionRateEnforcer{exceeded: []*rateLimitBucket{exceededBucket}}
		shadowCaller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{
			"t1": {Key: "t1", Throttled: false},
		}}
		limits, err := validation.NewOverrides(validation.Limits{IngestionRateMB: 1.0, IngestionBurstSizeMB: 2.0}, nil)
		require.NoError(t, err)

		reg := prometheus.NewPedanticRegistry()
		m := newMetrics(reg)
		e := newShadowEnforcer(enforcing, newThrottlerEnforcer(limits, shadowCaller), m)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets)
		require.NoError(t, err)
		require.Equal(t, []*rateLimitBucket{exceededBucket}, exceeded)

		require.Eventually(t, func() bool {
			return testutil.ToFloat64(m.exactShadowDecisions.WithLabelValues("t1", "admit")) == 1
		}, time.Second, time.Millisecond)
	})

	t.Run("a shadow transport error is recorded without affecting enforcement", func(t *testing.T) {
		enforcing := &fakeIngestionRateEnforcer{exceeded: nil}
		shadowCaller := &fakeThrottleCaller{
			byKey: map[string]throttler.ResponseEntry{"t1": {Key: "t1", Throttled: false}},
			err:   errBoom,
		}
		limits, err := validation.NewOverrides(validation.Limits{IngestionRateMB: 1.0, IngestionBurstSizeMB: 2.0}, nil)
		require.NoError(t, err)

		reg := prometheus.NewPedanticRegistry()
		m := newMetrics(reg)
		e := newShadowEnforcer(enforcing, newThrottlerEnforcer(limits, shadowCaller), m)

		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", buckets)
		require.NoError(t, err)
		require.Empty(t, exceeded)

		require.Eventually(t, func() bool {
			return testutil.ToFloat64(m.exactShadowFailed.WithLabelValues("t1")) == 1
		}, time.Second, time.Millisecond)
	})

	t.Run("limit delegates to the wrapped (enforcing) enforcer, never to shadow", func(t *testing.T) {
		enforcing := &fakeIngestionRateEnforcer{limitVal: 42}
		limits, err := validation.NewOverrides(validation.Limits{IngestionRateMB: 1.0}, nil)
		require.NoError(t, err)

		e := newShadowEnforcer(enforcing, newThrottlerEnforcer(limits, &fakeThrottleCaller{}), newMetrics(prometheus.NewPedanticRegistry()))
		require.Equal(t, 42, e.limit(time.Now(), "t1", &rateLimitBucket{}))
	})

	t.Run("enforce does not block on the shadow call", func(t *testing.T) {
		enforcing := &fakeIngestionRateEnforcer{exceeded: nil}
		block := make(chan struct{})
		shadowCaller := &blockingThrottleCaller{release: block}
		limits, err := validation.NewOverrides(validation.Limits{IngestionRateMB: 1.0, IngestionBurstSizeMB: 2.0}, nil)
		require.NoError(t, err)

		e := newShadowEnforcer(enforcing, newThrottlerEnforcer(limits, shadowCaller), newMetrics(prometheus.NewPedanticRegistry()))

		done := make(chan struct{})
		go func() {
			_, _ = e.enforce(context.Background(), time.Now(), "t1", buckets)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("enforce blocked on the shadow call instead of returning immediately")
		}
		close(block) // let the shadow goroutine finish so it doesn't leak past the test
	})
}

// blockingThrottleCaller blocks in Throttle until release is closed, used to prove enforce()
// never waits on the shadow call.
type blockingThrottleCaller struct {
	release chan struct{}
}

func (b *blockingThrottleCaller) Throttle(ctx context.Context, _ string, entries []throttler.RequestEntry) ([]throttler.ResponseEntry, error) {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	results := make([]throttler.ResponseEntry, len(entries))
	for i, e := range entries {
		results[i] = throttler.ResponseEntry{Key: e.Key}
	}
	return results, nil
}

func TestEscapeThrottlerKey(t *testing.T) {
	forbidden := func(key string) bool {
		return strings.ContainsFunc(key, func(r rune) bool { return r == '|' || unicode.IsSpace(r) })
	}

	t.Run("keys without forbidden characters are unchanged", func(t *testing.T) {
		for _, key := range []string{"t1", "t1:premium", "tenant-1.a_b*(x)!'", "t1:team:finance:eu"} {
			require.Equal(t, key, escapeThrottlerKey(key))
		}
	})

	t.Run("whitespace and pipe are escaped", func(t *testing.T) {
		for _, key := range []string{"t1:a b", "t1:a|b", "t1:a\tb", "t1:a\u00a0b", "t1:a\u2003b", "t1:a\vb"} {
			got := escapeThrottlerKey(key)
			require.False(t, forbidden(got), "%q escaped to %q", key, got)
		}
		require.Equal(t, "t1:a%20b%7Cc", escapeThrottlerKey("t1:a b|c"))
	})

	t.Run("escaping is injective", func(t *testing.T) {
		// A policy literally containing the escape sequence must not collide with the one it escapes to.
		require.NotEqual(t, escapeThrottlerKey("t1:a b"), escapeThrottlerKey("t1:a%20b"))
		require.Equal(t, "t1:a%2520b", escapeThrottlerKey("t1:a%20b"))
	})

	t.Run("enforce sends only valid keys for a policy with forbidden characters", func(t *testing.T) {
		limits, err := validation.NewOverrides(validation.Limits{
			IngestionRateMB:      1.0,
			IngestionBurstSizeMB: 2.0,
			PolicyOverrideLimits: map[string]validation.PolicyOverridableLimits{
				"my policy|x": {IngestionRateMB: ptr(5.0), IngestionBurstSizeMB: ptr(10.0)},
			},
		}, nil)
		require.NoError(t, err)

		caller := &fakeThrottleCaller{byKey: map[string]throttler.ResponseEntry{}}
		e := newThrottlerEnforcer(limits, caller)
		exceeded, err := e.enforce(context.Background(), time.Now(), "t1", map[string]*rateLimitBucket{
			"":            {policy: "", bytes: 100, lines: 1},
			"my policy|x": {policy: "my policy|x", hasOverride: true, bytes: 200, lines: 2},
		})
		require.NoError(t, err)
		require.Empty(t, exceeded)
		require.Len(t, caller.gotEntries, 2)
		for _, entry := range caller.gotEntries {
			require.False(t, forbidden(entry.Key), "sent key %q", entry.Key)
		}
	})
}
