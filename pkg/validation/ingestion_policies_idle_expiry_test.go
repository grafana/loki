package validation

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestOverrides_PolicyStreamIdleExpiry(t *testing.T) {
	limits := &Limits{
		PolicyOverrideLimits: map[string]PolicyOverridableLimits{
			"backfill": {StreamIdleExpiry: ptr(model.Duration(10 * time.Minute))},
			"zero":     {StreamIdleExpiry: ptr(model.Duration(0))},
			"unset":    {MaxGlobalStreamsPerUser: ptr(5)},
			// inherit_limits does not apply: there is no tenant-level expiry.
			"inherit": {InheritLimits: true},
		},
	}
	overrides := &Overrides{defaultLimits: limits}

	expiry, ok := overrides.PolicyStreamIdleExpiry("tenant1", "backfill")
	require.True(t, ok)
	require.Equal(t, 10*time.Minute, expiry)

	for _, policy := range []string{"zero", "unset", "inherit", "unknown", ""} {
		_, ok = overrides.PolicyStreamIdleExpiry("tenant1", policy)
		require.False(t, ok, policy)
	}
}

func TestPolicyOverridableLimits_ValidateStreamIdleExpiry(t *testing.T) {
	inherit := PolicyOverridableLimits{InheritLimits: true}
	for _, tc := range []struct {
		name    string
		limits  PolicyOverridableLimits
		expiry  *model.Duration
		wantErr bool
	}{
		{name: "unset"},
		{name: "zero", expiry: ptr(model.Duration(0))},
		{name: "minimum", limits: inherit, expiry: ptr(model.Duration(MinStreamIdleExpiry))},
		{name: "above minimum", limits: inherit, expiry: ptr(model.Duration(time.Hour))},
		{name: "below minimum", limits: inherit, expiry: ptr(model.Duration(MinStreamIdleExpiry - time.Second)), wantErr: true},
		{name: "negative", limits: inherit, expiry: ptr(model.Duration(-time.Minute)), wantErr: true},
		{
			name:   "with max_global_streams_per_user",
			limits: PolicyOverridableLimits{MaxGlobalStreamsPerUser: ptr(100)},
			expiry: ptr(model.Duration(time.Hour)),
		},
		{
			name:    "without a stream bucket of its own",
			expiry:  ptr(model.Duration(time.Hour)),
			wantErr: true,
		},
		{
			// The limits service only uses the global stream limit.
			name:    "with only max_streams_per_user",
			limits:  PolicyOverridableLimits{MaxLocalStreamsPerUser: ptr(100)},
			expiry:  ptr(model.Duration(time.Hour)),
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := tc.limits
			limits.StreamIdleExpiry = tc.expiry
			err := limits.Validate()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
