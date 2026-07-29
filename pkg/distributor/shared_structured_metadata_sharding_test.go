package distributor

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/test"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/v3/pkg/ingester"
	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
	"github.com/grafana/loki/v3/pkg/validation"
)

// shardParityBody builds a protobuf encoded OTLP export request holding one log record per line,
// all under a single scope of a single resource. It is parityBody with room for more than one
// record: stream sharding cannot produce more shards than there are entries, so a payload meant to
// be sharded N ways needs at least N of them.
func shardParityBody(t *testing.T, lines []string, resourceAttrs, scopeAttrs []parityAttr, ts time.Time) []byte {
	t.Helper()

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "svc")
	for _, a := range resourceAttrs {
		rl.Resource().Attributes().PutStr(a.name, a.value)
	}
	sl := rl.ScopeLogs().AppendEmpty()
	for _, a := range scopeAttrs {
		sl.Scope().Attributes().PutStr(a.name, a.value)
	}
	for i, line := range lines {
		lr := sl.LogRecords().AppendEmpty()
		lr.Body().SetStr(line)
		lr.SetTimestamp(pcommon.Timestamp(ts.Add(time.Duration(i) * time.Millisecond).UnixNano()))
	}

	body, err := plogotlp.NewExportRequestFromLogs(ld).MarshalProto()
	require.NoError(t, err)
	return body
}

// shardParityLimits are parityLimits plus rate based stream sharding at the given desired rate.
// The two sides of a run differ only in the deferred expansion flag.
func shardParityLimits(deferExpansion bool, desiredRate loki_flagext.ByteSize) *validation.Limits {
	limits := parityLimits(deferExpansion)
	limits.ShardStreams.Enabled = true
	limits.ShardStreams.TimeShardingEnabled = false
	limits.ShardStreams.DesiredRate = desiredRate
	return limits
}

// parseShardParityOTLP runs just the OTLP handler's translation for the given limits and returns
// the streams it produced, dropping the placeholder streams the handler leaves behind for
// resources whose entries were all promoted to their own label set (see parityPush).
//
// This is the request as the distributor receives it, which is what the two sizes below are
// measured on.
func parseShardParityOTLP(t *testing.T, limits *validation.Limits, body []byte) *logproto.PushRequest {
	t.Helper()

	overrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/otlp/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")

	resolver := newRequestScopedStreamResolver("test", overrides, log.NewNopLogger())
	pushReq, _, err := loghttp_push.ParseOTLPRequest("test", req, overrides, nil, 100<<20, 100<<20, nil, resolver, log.NewNopLogger())
	require.NoError(t, err)

	kept := pushReq.Streams[:0]
	for _, stream := range pushReq.Streams {
		if len(stream.Entries) > 0 {
			kept = append(kept, stream)
		}
	}
	pushReq.Streams = kept

	return pushReq
}

// requestSizes returns the two units the distributor measures a request in, mirroring the split
// documented in Distributor.PushWithResolver:
//
//   - unexpanded is the tenant-facing size: every entry's own structured metadata, plus each set of
//     a stream's shared structured metadata pool exactly once. This is what the ingestion rate
//     limit buckets, the discard metrics and the ingest-limits path are charged.
//   - expanded is the internal size stream sharding is measured in: every entry charged for the
//     sets it references, as if those attributes had been copied into it.
//
// For a request that carries no pool the two are the same number.
func requestSizes(req *logproto.PushRequest) (unexpanded, expanded int) {
	for i := range req.Streams {
		stream := req.Streams[i]
		for j := range stream.Entries {
			own := util.EntryTotalSize(&stream.Entries[j])
			resource, scope := stream.SharedFor(&stream.Entries[j])

			unexpanded += own
			expanded += own + util.StructuredMetadataSize(resource) + util.StructuredMetadataSize(scope)
		}
		unexpanded += util.SharedSetsSize(stream.SharedStructuredMetadataSets)
	}

	return unexpanded, expanded
}

// pushShardParity pushes the payload through the whole distributor under the given limits and
// returns the label sets of the distinct streams that reached the ingester, i.e. the shards.
//
// The rate store is replaced by a fake reporting one push per second and no ingested rate, so that
// the shard count is decided by the push size alone: with a real rate store the first push of a
// stream is never sharded (see Distributor.shardCountFor).
func pushShardParity(t *testing.T, limits *validation.Limits, body []byte) []string {
	t.Helper()

	distributors, ingesters := prepare(t, 1, 3, limits, nil)
	d := distributors[0]
	d.rateStore = &fakeRateStore{pushRate: 1}

	pushReq := parseShardParityOTLP(t, limits, body)
	resolver := newRequestScopedStreamResolver("test", d.validator.Limits, d.logger)
	_, err := d.PushWithResolver(ctx, pushReq, resolver, constants.OTLP)
	require.NoError(t, err)

	// PushWithResolver returns as soon as it has a quorum, so the last replica's push can still be
	// in flight. Every ingester receives exactly one request for one push, so waiting until all of
	// them have recorded one is waiting for the push to have fully landed - and it is what makes
	// the read below ordered after the writes.
	test.Poll(t, 5*time.Second, true, func() interface{} {
		for i := range ingesters {
			ingesters[i].mu.Lock()
			landed := len(ingesters[i].pushed) > 0
			ingesters[i].mu.Unlock()
			if !landed {
				return false
			}
		}
		return true
	})

	seen := map[string]struct{}{}
	var out []string
	for i := range ingesters {
		ingesters[i].mu.Lock()
		for _, pushed := range ingesters[i].pushed {
			for j := range pushed.Streams {
				lbls := pushed.Streams[j].Labels
				if _, ok := seen[lbls]; ok {
					continue
				}
				seen[lbls] = struct{}{}
				out = append(out, lbls)
			}
		}
		ingesters[i].mu.Unlock()
	}
	require.NotEmpty(t, out, "no stream reached any ingester")

	return out
}

// TestDistributor_DeferredExpansionShardCountParity is the regression guard for the sharding half
// of the two-unit split: one and the same logical OTLP payload must shard into the same number of
// shards with otlp_defer_structured_metadata_expansion off and on.
//
// Sharding is internal load distribution, not tenant-facing metering, and the load a shard puts on
// an ingester is still the expanded one - the ingester materializes own++resource++scope per push
// batch. So the distributor sizes sharding in expanded-equivalent bytes, and a flag-on stream lands
// on as many ingesters as the same payload would have flag-off.
//
// The payload is deliberately built so that the unexpanded size would under-shard it: almost all of
// its bytes are resource and scope attributes, which the unexpanded unit counts once for the whole
// stream rather than once per entry.
func TestDistributor_DeferredExpansionShardCountParity(t *testing.T) {
	ts := time.Now()

	// Bulk in the resource and scope attributes, tiny lines and no record attributes: this is the
	// shape whose accounting the flag changes. Four records so that four shards can materialize -
	// the shard count is capped by the number of entries.
	resourceAttrs := []parityAttr{
		{"deployment_environment", strings.Repeat("a", 200)},
		{"k8s_namespace_name", strings.Repeat("b", 200)},
	}
	scopeAttrs := []parityAttr{{"library_build_id", strings.Repeat("c", 200)}}
	lines := []string{"one", "two", "three", "four"}

	body := shardParityBody(t, lines, resourceAttrs, scopeAttrs, ts)

	// The two sizes of the deferred request, and the size of the expanded one. The expanded push
	// carries no pool, so its two units coincide - and they coincide with the deferred request's
	// expanded-equivalent size, which is the whole reason that unit gives parity.
	deferredUnexpanded, deferredExpanded := requestSizes(parseShardParityOTLP(t, shardParityLimits(true, 1), body))
	expandedUnexpanded, expandedExpanded := requestSizes(parseShardParityOTLP(t, shardParityLimits(false, 1), body))

	require.Equal(t, expandedUnexpanded, expandedExpanded, "a request with no pool has only one size")
	require.Equal(t, expandedExpanded, deferredExpanded,
		"the expanded-equivalent size of the deferred request must equal the size of the expanded one")
	require.Less(t, deferredUnexpanded, deferredExpanded,
		"the payload must actually be cheaper unexpanded, otherwise this test proves nothing")

	// Pick the desired rate so that the two units disagree about the shard count as loudly as
	// possible: at exactly the unexpanded size, sharding on the unexpanded unit yields a single
	// shard while the expanded-equivalent unit yields several.
	desiredRate := loki_flagext.ByteSize(deferredUnexpanded)
	wantShards := calculateShards(0, deferredExpanded, desiredRate.Val())
	require.Greater(t, wantShards, 1, "the expanded-equivalent size must shard this payload")
	require.Equal(t, 1, calculateShards(0, deferredUnexpanded, desiredRate.Val()),
		"the unexpanded size must not shard this payload, that is the regression this test guards")

	expandedShards := pushShardParity(t, shardParityLimits(false, desiredRate), body)
	deferredShards := pushShardParity(t, shardParityLimits(true, desiredRate), body)

	require.Len(t, expandedShards, wantShards, "flag off: sharded on the size of the expanded entries")
	require.Len(t, deferredShards, wantShards, "flag on: must shard exactly like flag off, not less")
	require.Equal(t, len(expandedShards), len(deferredShards))

	// And they are the same shards: same label set, same shard numbering.
	require.ElementsMatch(t, expandedShards, deferredShards)
	for _, lbls := range deferredShards {
		parsed, err := syntax.ParseLabels(lbls)
		require.NoError(t, err)
		require.NotEmpty(t, parsed.Get(ingester.ShardLbName), "expected a sharded stream, got %s", lbls)
	}
}

// TestDistributor_DeferredExpansionRateLimitBucketStaysUnexpanded is the other half of the split:
// sharding moved to the expanded-equivalent unit, the tenant-facing ingestion rate limit buckets
// did not.
//
// The bytes a bucket was charged are read back off the 429 the distributor returns, which reports
// them verbatim.
func TestDistributor_DeferredExpansionRateLimitBucketStaysUnexpanded(t *testing.T) {
	ts := time.Now()

	resourceAttrs := []parityAttr{
		{"deployment_environment", strings.Repeat("a", 200)},
		{"k8s_namespace_name", strings.Repeat("b", 200)},
	}
	scopeAttrs := []parityAttr{{"library_build_id", strings.Repeat("c", 200)}}
	lines := []string{"one", "two", "three", "four"}

	body := shardParityBody(t, lines, resourceAttrs, scopeAttrs, ts)

	// A limit of one byte rejects anything, so every push comes back reporting what its bucket was
	// asked for.
	meteredBytes := func(t *testing.T, deferExpansion bool) int {
		t.Helper()

		limits := shardParityLimits(deferExpansion, 1<<20)
		limits.IngestionRateStrategy = validation.LocalIngestionRateStrategy
		limits.IngestionRateMB = datasize.ByteSize(1).MBytes()
		limits.IngestionBurstSizeMB = datasize.ByteSize(1).MBytes()

		distributors, _ := prepare(t, 1, 3, limits, nil)
		d := distributors[0]

		pushReq := parseShardParityOTLP(t, limits, body)
		resolver := newRequestScopedStreamResolver("test", d.validator.Limits, d.logger)
		_, err := d.PushWithResolver(ctx, pushReq, resolver, constants.OTLP)
		require.Error(t, err, "the push must be rate limited for its size to be readable")

		matches := regexp.MustCompile(`totaling '(\d+)' bytes`).FindStringSubmatch(err.Error())
		require.Len(t, matches, 2, "could not read the metered bytes out of %q", err.Error())

		metered, convErr := strconv.Atoi(matches[1])
		require.NoError(t, convErr)
		return metered
	}

	deferredUnexpanded, deferredExpanded := requestSizes(parseShardParityOTLP(t, shardParityLimits(true, 1), body))
	require.Less(t, deferredUnexpanded, deferredExpanded)

	require.Equal(t, deferredUnexpanded, meteredBytes(t, true),
		"the rate limit bucket must charge the unexpanded size: the pool once per stream, not once per entry")
	require.Equal(t, deferredExpanded, meteredBytes(t, false),
		"with the flag off there is no pool, so the tenant is charged the entries as they arrive")
}

// TestDistributor_ShardCountUnaffectedWithoutPool pins that none of this moved the flag-off, native
// push behavior: with no shared structured metadata pool the two units are the same number, so a
// stream shards exactly as it did before the split.
func TestDistributor_ShardCountUnaffectedWithoutPool(t *testing.T) {
	lbs, err := syntax.ParseLabels(`{app="myapp"}`)
	require.NoError(t, err)

	entries := generateEntries(10)
	for i := range entries {
		entries[i].StructuredMetadata = logproto.FromLabelsToLabelAdapters(
			labels.FromStrings("trace_id", fmt.Sprintf("%d", i)),
		)
	}

	stream := logproto.Stream{
		Labels:  lbs.String(),
		Hash:    labels.StableHash(lbs),
		Entries: entries,
	}

	// Both units of an unpooled stream, computed the way the distributor does.
	unexpanded, expanded := requestSizes(&logproto.PushRequest{Streams: []logproto.Stream{stream}})
	require.Equal(t, unexpanded, expanded, "without a pool the two units must be identical")

	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.ShardStreams.DesiredRate = loki_flagext.ByteSize(unexpanded / 3)

	overrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)
	validator, err := NewValidator(overrides, nil)
	require.NoError(t, err)

	d := Distributor{
		rateStore:    &fakeRateStore{pushRate: 1},
		validator:    validator,
		m:            newMetrics(nil),
		shardTracker: NewShardTracker(),
	}

	require.Equal(t,
		d.shardCountFor(log.NewNopLogger(), &stream, unexpanded, "fake", d.validator.ShardStreams("fake")),
		d.shardCountFor(log.NewNopLogger(), &stream, expanded, "fake", d.validator.ShardStreams("fake")),
	)
}
