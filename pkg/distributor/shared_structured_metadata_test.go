package distributor

import (
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/grafana/loki/pkg/push"
)

// sharedTestResource and sharedTestScope are the kind of attribute sets the OTLP push path pools
// when the expansion of resource and scope attributes is deferred to the read path.
func sharedTestResource() []push.LabelAdapter {
	return []push.LabelAdapter{
		{Name: "service_name", Value: "myservice"},
		{Name: "deployment_environment", Value: "prod"},
	}
}

func sharedTestScope() []push.LabelAdapter {
	return []push.LabelAdapter{{Name: "scope_name", Value: "mylib"}}
}

// sharedTestPool is a two set pool: the resource set at reference 1, the scope set at reference 2.
func sharedTestPool() []logproto.SharedStructuredMetadataSet {
	return []logproto.SharedStructuredMetadataSet{
		{Attrs: sharedTestResource()},
		{Attrs: sharedTestScope()},
	}
}

// refAllEntries points every entry of the stream at the two sets of sharedTestPool.
func refAllEntries(stream *logproto.Stream) {
	for i := range stream.Entries {
		stream.Entries[i].SharedResourceRef = 1
		stream.Entries[i].SharedScopeRef = 2
	}
}

// pushDeferred pushes a request that carries a shared structured metadata pool through the
// in-process entry point the OTLP handler uses.
//
// Distributor.Push is the external gRPC endpoint and strips pools and references from its input
// (see push.Stream.StripSharedStructuredMetadata), so it cannot be used to exercise the
// distributor's handling of them. PushWithResolver is what pkg/distributor/http.go calls for both
// the native and the OTLP HTTP push paths, and is where a pool legitimately arrives.
func pushDeferred(t *testing.T, d *Distributor, req *logproto.PushRequest) error {
	t.Helper()

	resolver := newRequestScopedStreamResolver("test", d.validator.Limits, d.logger)
	_, err := d.PushWithResolver(ctx, req, resolver, constants.Loki)
	return err
}

// TestShardStream_PreservesSharedStructuredMetadata covers the rate based sharding path: every
// shard must keep the whole shared structured metadata pool of the source stream, otherwise the
// references carried by the entries it holds stop resolving and sharding silently drops all
// resource and scope attributes of high volume streams.
func TestShardStream_PreservesSharedStructuredMetadata(t *testing.T) {
	lbs, err := syntax.ParseLabels(`{app="myapp"}`)
	require.NoError(t, err)

	pool := sharedTestPool()
	baseStream := logproto.Stream{
		Labels:                       lbs.String(),
		Hash:                         labels.StableHash(lbs),
		Entries:                      generateEntries(20),
		SharedStructuredMetadataSets: pool,
	}
	refAllEntries(&baseStream)

	desiredRate := loki_flagext.ByteSize(300)

	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.ShardStreams.DesiredRate = desiredRate

	overrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)

	validator, err := NewValidator(overrides, nil)
	require.NoError(t, err)

	d := Distributor{
		rateStore:    &fakeRateStore{pushRate: 1},
		validator:    validator,
		m:            newMetrics(prometheus.NewPedanticRegistry()),
		shardTracker: NewShardTracker(),
	}

	// Force 4 shards.
	derivedStreams := d.shardStream(baseStream, 1+(desiredRate.Val()*3), "fake", "", d.validator.ShardStreams("fake"))
	require.Len(t, derivedStreams, 4)

	for i, s := range derivedStreams {
		require.Equalf(t, pool, s.Stream.SharedStructuredMetadataSets, "shard %d lost the shared structured metadata pool", i)
		require.NotEmptyf(t, s.Stream.Entries, "shard %d has no entries", i)
		require.NoErrorf(t, s.Stream.ValidateSharedRefs(), "shard %d has references that no longer resolve", i)

		// Spot check that a reference resolves to the same sets before and after sharding.
		for j := range s.Stream.Entries {
			resource, scope := s.Stream.SharedFor(&s.Stream.Entries[j])
			require.Equal(t, push.LabelsAdapter(sharedTestResource()), resource)
			require.Equal(t, push.LabelsAdapter(sharedTestScope()), scope)
		}
	}
}

// TestShardStreamByTime_PreservesSharedStructuredMetadata covers the time sharding path, both for
// the time sharded streams and for the trailing stream that keeps the original labels.
func TestShardStreamByTime_PreservesSharedStructuredMetadata(t *testing.T) {
	baseTimestamp := time.Date(2024, 10, 31, 12, 34, 56, 0, time.UTC)
	lbs, err := syntax.ParseLabels(`{app="myapp"}`)
	require.NoError(t, err)

	pool := sharedTestPool()
	stream := logproto.Stream{
		Labels: lbs.String(),
		Hash:   labels.StableHash(lbs),
		Entries: []logproto.Entry{
			{Timestamp: baseTimestamp, Line: "foo"},
			{Timestamp: baseTimestamp.Add(time.Hour), Line: "bar"},
			// This one is after ignoreFrom, so it ends up in the trailing stream that keeps the
			// original labels.
			{Timestamp: baseTimestamp.Add(3 * time.Hour), Line: "baz"},
		},
		SharedStructuredMetadataSets: pool,
	}
	refAllEntries(&stream)

	shards, ok := shardStreamByTime(stream, lbs, time.Hour, baseTimestamp.Add(2*time.Hour))
	require.True(t, ok)
	require.Len(t, shards, 3)

	// The last shard is the one without a time shard label.
	require.Equal(t, stream.Labels, shards[len(shards)-1].Stream.Labels)

	for i, s := range shards {
		require.Equalf(t, pool, s.Stream.SharedStructuredMetadataSets, "time shard %d lost the shared structured metadata pool", i)
		require.NoErrorf(t, s.Stream.ValidateSharedRefs(), "time shard %d has references that no longer resolve", i)

		for j := range s.Stream.Entries {
			resource, scope := s.Stream.SharedFor(&s.Stream.Entries[j])
			require.Equal(t, push.LabelsAdapter(sharedTestResource()), resource)
			require.Equal(t, push.LabelsAdapter(sharedTestScope()), scope)
		}
	}
}

// TestCalculateStreamSizes_PostShardStreamCountsSharedMetadata makes sure the ingest limits path,
// which runs on already sharded streams, keeps counting the shared structured metadata pool, and
// counts each of its sets exactly once however many entries reference it.
func TestCalculateStreamSizes_PostShardStreamCountsSharedMetadata(t *testing.T) {
	lbs, err := syntax.ParseLabels(`{app="myapp"}`)
	require.NoError(t, err)

	pool := sharedTestPool()
	baseStream := logproto.Stream{
		Labels:                       lbs.String(),
		Hash:                         labels.StableHash(lbs),
		Entries:                      generateEntries(20),
		SharedStructuredMetadataSets: pool,
	}
	refAllEntries(&baseStream)

	desiredRate := loki_flagext.ByteSize(300)

	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.ShardStreams.DesiredRate = desiredRate

	overrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)

	validator, err := NewValidator(overrides, nil)
	require.NoError(t, err)

	d := Distributor{
		rateStore:    &fakeRateStore{pushRate: 1},
		validator:    validator,
		m:            newMetrics(prometheus.NewPedanticRegistry()),
		shardTracker: NewShardTracker(),
	}

	derivedStreams := d.shardStream(baseStream, 1+(desiredRate.Val()*3), "fake", "", d.validator.ShardStreams("fake"))
	require.Len(t, derivedStreams, 4)

	poolSize := uint64(util.SharedSetsSize(pool))
	require.NotZero(t, poolSize)

	for i, s := range derivedStreams {
		_, structuredMetadataSize := calculateStreamSizes(s.Stream)
		require.Equalf(t, poolSize, structuredMetadataSize, "shard %d did not count the shared structured metadata pool exactly once", i)
	}
}

// TestDistributor_SharedStructuredMetadataSanitization asserts that values of the stream wide
// structured metadata are scrubbed of invalid UTF-8 and that their names are normalized, exactly
// like the per entry structured metadata is, and that both increment the sanitization counter.
func TestDistributor_SharedStructuredMetadataSanitization(t *testing.T) {
	for _, tc := range []struct {
		name             string
		pool             []logproto.SharedStructuredMetadataSet
		expected         []logproto.SharedStructuredMetadataSet
		numSanitizations float64
	}{
		{
			name:             "clean pool sets are left alone",
			pool:             []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service_name", Value: "myservice"}}}},
			expected:         []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service_name", Value: "myservice"}}}},
			numSanitizations: 0,
		},
		{
			name:             "invalid utf-8 in a pooled value is scrubbed",
			pool:             []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service_name", Value: "my\xc5service"}}}},
			expected:         []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service_name", Value: "my service"}}}},
			numSanitizations: 1,
		},
		{
			name:             "an invalid pooled name is normalized",
			pool:             []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service.name", Value: "myservice"}}}},
			expected:         []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service_name", Value: "myservice"}}}},
			numSanitizations: 1,
		},
		{
			name:             "sanitizing happens once per pooled set, not once per entry",
			pool:             []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service.name", Value: "my\xc5service"}}}},
			expected:         []logproto.SharedStructuredMetadataSet{{Attrs: []push.LabelAdapter{{Name: "service_name", Value: "my service"}}}},
			numSanitizations: 2, // one for the name, one for the value - not multiplied by the entries
		},
		{
			name: "every set of the pool is sanitized",
			pool: []logproto.SharedStructuredMetadataSet{
				{Attrs: []push.LabelAdapter{{Name: "service.name", Value: "myservice"}}},
				{Attrs: []push.LabelAdapter{{Name: "scope.name", Value: "mylib"}}},
			},
			expected: []logproto.SharedStructuredMetadataSet{
				{Attrs: []push.LabelAdapter{{Name: "service_name", Value: "myservice"}}},
				{Attrs: []push.LabelAdapter{{Name: "scope_name", Value: "mylib"}}},
			},
			numSanitizations: 2, // one name per set
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.AllowStructuredMetadata = true
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false

			distributors, _ := prepare(t, 1, 3, limits, nil)
			tee := &mockTee{}
			distributors[0].tee = tee

			req := &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{foo="bar"}`,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(123456, 0), Line: "line 1"},
							{Timestamp: time.Unix(123457, 0), Line: "line 2"},
							{Timestamp: time.Unix(123458, 0), Line: "line 3"},
						},
						SharedStructuredMetadataSets: tc.pool,
					},
				},
			}

			require.NoError(t, pushDeferred(t, distributors[0], req))

			require.Len(t, tee.duplicated, 1)
			require.Len(t, tee.duplicated[0], 1)
			require.Equal(t, tc.expected, tee.duplicated[0][0].Stream.SharedStructuredMetadataSets)

			require.Equal(t, tc.numSanitizations, testutil.ToFloat64(
				distributors[0].m.tenantPushSanitizedStructuredMetadata.WithLabelValues("test", constants.Loki),
			))
		})
	}
}

// TestDistributor_LevelDetectionWithSharedStructuredMetadata covers the ways the shared structured
// metadata an entry references interacts with level detection: either referenced set can be the
// source of the level, and either can already carry a detected_level that must not be duplicated
// onto the entry.
func TestDistributor_LevelDetectionWithSharedStructuredMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		// resource and scope attribute sets pooled by the pushed stream. Empty means the entry
		// does not reference that kind of set.
		resource []push.LabelAdapter
		scope    []push.LabelAdapter
		// own structured metadata of the pushed entry.
		own push.LabelsAdapter
		// expected detected_level values on the entry after the push. Empty means none.
		expectedLevels []string
	}{
		{
			name:           "severity in the referenced resource set is used to derive detected_level",
			resource:       []push.LabelAdapter{{Name: "severity", Value: "ERROR"}},
			expectedLevels: []string{constants.LogLevelError},
		},
		{
			name:           "severity in the referenced scope set is used to derive detected_level",
			scope:          []push.LabelAdapter{{Name: "severity", Value: "WARN"}},
			expectedLevels: []string{constants.LogLevelWarn},
		},
		{
			name:           "normalized detected_level already in the referenced resource set is not duplicated",
			resource:       []push.LabelAdapter{{Name: constants.LevelLabel, Value: constants.LogLevelWarn}},
			expectedLevels: nil,
		},
		{
			name:           "normalized detected_level already in the referenced scope set is not duplicated",
			scope:          []push.LabelAdapter{{Name: constants.LevelLabel, Value: constants.LogLevelWarn}},
			expectedLevels: nil,
		},
		{
			// A shared set cannot be normalized in place, it is shared with the other entries
			// referencing it, and nothing is appended to the entry either: a second
			// detected_level would be a duplicate. The raw value is stored as is. This is the
			// one accepted difference from the expanded path, which rewrites the pair in place.
			name:           "unnormalized detected_level in the referenced resource set is left alone",
			resource:       []push.LabelAdapter{{Name: constants.LevelLabel, Value: "WARNING"}},
			expectedLevels: nil,
		},
		{
			name:           "unnormalized detected_level in the referenced scope set is left alone",
			scope:          []push.LabelAdapter{{Name: constants.LevelLabel, Value: "WARNING"}},
			expectedLevels: nil,
		},
		{
			// Whichever set carries it, a shared detected_level only suppresses detection.
			name:           "a detected_level in both sets adds nothing to the entry",
			resource:       []push.LabelAdapter{{Name: constants.LevelLabel, Value: "ERR"}},
			scope:          []push.LabelAdapter{{Name: constants.LevelLabel, Value: "WARNING"}},
			expectedLevels: nil,
		},
		{
			// The expanded equivalent of this entry's metadata holds the resource attributes
			// before the scope ones and is scanned front to back, so resource wins.
			name:           "the resource set takes precedence over the scope set",
			resource:       []push.LabelAdapter{{Name: "severity", Value: "ERROR"}},
			scope:          []push.LabelAdapter{{Name: "severity", Value: "WARN"}},
			expectedLevels: []string{constants.LogLevelError},
		},
		{
			name:           "the entry's own metadata takes precedence over both shared sets",
			resource:       []push.LabelAdapter{{Name: "severity", Value: "ERROR"}},
			scope:          []push.LabelAdapter{{Name: "severity", Value: "WARN"}},
			own:            push.LabelsAdapter{{Name: "severity", Value: "debug"}},
			expectedLevels: []string{constants.LogLevelDebug},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.AllowStructuredMetadata = true
			limits.DiscoverLogLevels = true
			limits.RejectOldSamples = false

			distributors, _ := prepare(t, 1, 3, limits, nil)
			tee := &mockTee{}
			distributors[0].tee = tee

			// Build the pool out of whichever sets the case defines, and point the entry at them.
			var (
				pool                  []logproto.SharedStructuredMetadataSet
				resourceRef, scopeRef uint32
			)
			if len(tc.resource) > 0 {
				pool = append(pool, logproto.SharedStructuredMetadataSet{Attrs: tc.resource})
				resourceRef = uint32(len(pool))
			}
			if len(tc.scope) > 0 {
				pool = append(pool, logproto.SharedStructuredMetadataSet{Attrs: tc.scope})
				scopeRef = uint32(len(pool))
			}

			req := &logproto.PushRequest{
				Streams: []logproto.Stream{
					{
						Labels: `{foo="bar"}`,
						Entries: []logproto.Entry{
							{
								Timestamp:          time.Unix(123456, 0),
								Line:               "a line with no level in it",
								StructuredMetadata: tc.own,
								SharedResourceRef:  resourceRef,
								SharedScopeRef:     scopeRef,
							},
						},
						SharedStructuredMetadataSets: pool,
					},
				},
			}

			require.NoError(t, pushDeferred(t, distributors[0], req))

			require.Len(t, tee.duplicated, 1)
			require.Len(t, tee.duplicated[0], 1)
			pushed := tee.duplicated[0][0].Stream
			require.Len(t, pushed.Entries, 1)

			var levels []string
			for _, sm := range pushed.Entries[0].StructuredMetadata {
				if sm.Name == constants.LevelLabel {
					levels = append(levels, sm.Value)
				}
			}
			require.Equal(t, tc.expectedLevels, levels)

			// The pool itself is never rewritten by detection, and the references still resolve.
			require.Equal(t, pool, pushed.SharedStructuredMetadataSets)
			require.NoError(t, pushed.ValidateSharedRefs())
		})
	}
}

// TestDistributor_SharedStructuredMetadataLimits asserts that per entry structured metadata limits
// see the effective metadata of an entry: its own plus the two sets it references.
func TestDistributor_SharedStructuredMetadataLimits(t *testing.T) {
	resource := []push.LabelAdapter{{Name: "service_name", Value: "myservice"}}
	scope := []push.LabelAdapter{{Name: "scope_name", Value: "mylib"}}

	newReq := func(resourceRef, scopeRef uint32) *logproto.PushRequest {
		return &logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels: `{foo="bar"}`,
					Entries: []logproto.Entry{
						{
							Timestamp:         time.Unix(123456, 0),
							Line:              "a line",
							SharedResourceRef: resourceRef,
							SharedScopeRef:    scopeRef,
						},
					},
					SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{
						{Attrs: resource},
						{Attrs: scope},
					},
				},
			},
		}
	}

	for _, tc := range []struct {
		name                  string
		maxCount              int
		resourceRef, scopeRef uint32
		expectErr             bool
	}{
		{name: "no references stays under the limit", maxCount: 1, expectErr: false},
		{name: "one referenced set stays under the limit", maxCount: 1, resourceRef: 1, expectErr: false},
		{name: "two referenced sets exceed the limit", maxCount: 1, resourceRef: 1, scopeRef: 2, expectErr: true},
		{name: "two referenced sets fit a larger limit", maxCount: 2, resourceRef: 1, scopeRef: 2, expectErr: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.AllowStructuredMetadata = true
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false
			limits.MaxStructuredMetadataEntriesCount = tc.maxCount

			distributors, _ := prepare(t, 1, 3, limits, nil)
			distributors[0].tee = &mockTee{}

			err := pushDeferred(t, distributors[0], newReq(tc.resourceRef, tc.scopeRef))
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "structured metadata")
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestDistributor_SanitizeDoesNotCorruptAliasedPoolSet is a regression test for pool sets whose
// backing array is shared by two streams.
//
// otlpToLokiPushRequest produces exactly that for one OTLP resource whose entries are split
// between the resource's own label set and a promoted one: both wire streams pool the same
// resource attribute slice. Sanitizing used to write the normalized set back through that array
// with logproto.CopyToLabelAdapters, which clears and reuses it. When normalization collapses two
// names onto one the result is shorter, so the second stream was left a set whose length still
// counted a zeroed tail, and sanitizing it fed an empty name to the label namer and failed the
// whole push.
func TestDistributor_SanitizeDoesNotCorruptAliasedPoolSet(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.AllowStructuredMetadata = true
	limits.RejectOldSamples = false
	limits.DiscoverLogLevels = false

	distributors, _ := prepare(t, 1, 3, limits, nil)
	tee := &mockTee{}
	distributors[0].tee = tee

	// "a.b" normalizes onto the already present "a_b", so the sanitized set is one pair shorter
	// than the original.
	aliased := []push.LabelAdapter{
		{Name: "a.b", Value: "FROM_DOTTED"},
		{Name: "a_b", Value: "FROM_UNDERSCORE"},
	}
	before := append([]push.LabelAdapter(nil), aliased...)

	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels:                       `{foo="bar"}`,
				Entries:                      []logproto.Entry{{Timestamp: time.Unix(123456, 0), Line: "l1", SharedResourceRef: 1}},
				SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{{Attrs: aliased}},
			},
			{
				Labels:                       `{foo="bar", promoted="yes"}`,
				Entries:                      []logproto.Entry{{Timestamp: time.Unix(123457, 0), Line: "l2", SharedResourceRef: 1}},
				SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{{Attrs: aliased}},
			},
		},
	}

	require.NoError(t, pushDeferred(t, distributors[0], req))

	require.Len(t, tee.duplicated, 1)
	require.Len(t, tee.duplicated[0], 2)

	expected := []push.LabelAdapter{{Name: "a_b", Value: "FROM_DOTTED"}}
	for i, ks := range tee.duplicated[0] {
		got := ks.Stream.SharedStructuredMetadataSets[0].Attrs
		require.Equalf(t, expected, got, "stream %d was not sanitized correctly", i)
		for _, a := range got {
			require.NotEmptyf(t, a.Name, "stream %d has a zeroed pair in its pool", i)
		}
		require.NoErrorf(t, ks.Stream.ValidateSharedRefs(), "stream %d has references that no longer resolve", i)
	}

	// Sanitizing is copy-on-write: the array the two pools came in on is untouched.
	require.Equal(t, before, aliased, "the shared backing array must not have been written to")

	// Metering is computed on the sanitized values, not the originals.
	sanitizedSize := uint64(util.SharedSetsSize([]logproto.SharedStructuredMetadataSet{{Attrs: expected}}))
	require.NotZero(t, sanitizedSize)
	for i, ks := range tee.duplicated[0] {
		_, structuredMetadataSize := calculateStreamSizes(ks.Stream)
		require.Equalf(t, sanitizedSize, structuredMetadataSize, "stream %d metered unsanitized values", i)
	}
}

// TestDistributor_PushStripsSharedStructuredMetadata asserts that the external gRPC push endpoint
// discards a pool and the references to it, so that a native client cannot opt itself into
// deferred expansion semantics regardless of its tenant's OTLP limit.
func TestDistributor_PushStripsSharedStructuredMetadata(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.AllowStructuredMetadata = true
	limits.RejectOldSamples = false
	limits.DiscoverLogLevels = false

	distributors, _ := prepare(t, 1, 3, limits, nil)
	tee := &mockTee{}
	distributors[0].tee = tee

	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
				Entries: []logproto.Entry{
					{
						Timestamp:          time.Unix(123456, 0),
						Line:               "a line",
						StructuredMetadata: push.LabelsAdapter{{Name: "own", Value: "kept"}},
						SharedResourceRef:  1,
						SharedScopeRef:     2,
					},
				},
				SharedStructuredMetadataSets: sharedTestPool(),
			},
		},
	}

	_, err := distributors[0].Push(ctx, req)
	require.NoError(t, err)

	require.Len(t, tee.duplicated, 1)
	require.Len(t, tee.duplicated[0], 1)
	pushed := tee.duplicated[0][0].Stream

	require.Empty(t, pushed.SharedStructuredMetadataSets, "the pool must be discarded on the external endpoint")
	require.Len(t, pushed.Entries, 1)
	require.Zero(t, pushed.Entries[0].SharedResourceRef)
	require.Zero(t, pushed.Entries[0].SharedScopeRef)
	// The entry's own structured metadata is untouched.
	require.Equal(t, push.LabelsAdapter{{Name: "own", Value: "kept"}}, pushed.Entries[0].StructuredMetadata)

	// Nothing shared is left to meter.
	_, structuredMetadataSize := calculateStreamSizes(pushed)
	require.Equal(t, uint64(len("ownkept")), structuredMetadataSize)
}
