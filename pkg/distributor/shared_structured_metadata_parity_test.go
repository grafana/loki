package distributor

import (
	"bytes"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/grafana/loki/pkg/push"
)

// parityAttr is one attribute of the OTLP payload the parity tests push.
type parityAttr struct{ name, value string }

// parityBody builds a protobuf encoded OTLP export request holding a single log record, under a
// single scope, of a single resource. service.name is always present so that the record lands in a
// stream with at least one label; the resource, scope and record attributes are whatever the caller
// asks for, added in slice order.
func parityBody(t *testing.T, line string, resourceAttrs, scopeAttrs, recordAttrs []parityAttr, ts time.Time) []byte {
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
	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr(line)
	lr.SetTimestamp(pcommon.Timestamp(ts.UnixNano()))
	for _, a := range recordAttrs {
		lr.Attributes().PutStr(a.name, a.value)
	}

	body, err := plogotlp.NewExportRequestFromLogs(ld).MarshalProto()
	require.NoError(t, err)
	return body
}

// parityLimits are the limits the two sides of a parity run share, with only the deferred expansion
// flag differing. service.name is indexed as a label so that the payload produces a labelled
// stream, and level detection is off so that what reaches the ingesters is the sanitization result
// and nothing else.
func parityLimits(deferExpansion bool) *validation.Limits {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.AllowStructuredMetadata = true
	limits.RejectOldSamples = false
	limits.DiscoverLogLevels = false
	limits.OTLPDeferStructuredMetadataExpansion = deferExpansion

	otlpConfig := loghttp_push.DefaultOTLPConfig(loghttp_push.GlobalOTLPConfig{
		DefaultOTLPResourceAttributesAsIndexLabels: []string{"service.name"},
	})
	limits.OTLPConfig = &otlpConfig

	return limits
}

// parityPush pushes an OTLP payload through the two stages a real OTLP request goes through - the
// OTLP handler's translation, then Distributor.PushWithResolver, the in-process entry point
// pkg/distributor/http.go calls, which unlike the external Push endpoint does not strip pools - and
// returns the effective structured metadata of every entry that reached an ingester.
//
// The effective view is what makes the two modes comparable: with the flag off the entries already
// carry everything, with it on they carry references into their stream's pool, and this is the view
// the pair resolves to. The ingester no longer materializes it to store an entry - the chunk merges
// the shared pairs in symbol space, see sharedSets in pkg/ingester/stream.go - but it is still the
// view a tailer and a dataobj consumer are handed.
func parityPush(t *testing.T, limits *validation.Limits, body []byte) []push.LabelsAdapter {
	t.Helper()

	distributors, ingesters := prepare(t, 1, 3, limits, nil)
	d := distributors[0]

	req := httptest.NewRequest("POST", "/otlp/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")

	resolver := newRequestScopedStreamResolver("test", d.validator.Limits, d.logger)
	pushReq, _, err := loghttp_push.ParseOTLPRequest("test", req, d.validator.Limits, nil, 100<<20, 100<<20, nil, resolver, log.NewNopLogger())
	require.NoError(t, err)

	// The OTLP handler leaves a placeholder stream behind for every resource whose entries were all
	// promoted to their own label set. The HTTP push path never sees them because it hands the
	// request straight on, but the distributor rejects a stream with no labels, so they have to go.
	kept := pushReq.Streams[:0]
	for _, stream := range pushReq.Streams {
		if len(stream.Entries) > 0 {
			kept = append(kept, stream)
		}
	}
	pushReq.Streams = kept

	_, err = d.PushWithResolver(ctx, pushReq, resolver, constants.OTLP)
	require.NoError(t, err)

	// The replica pushes run on their own goroutines and PushWithResolver returns on a quorum, so
	// what an ingester recorded is only safe to read under its lock.
	for i := range ingesters {
		var out []push.LabelsAdapter

		ingesters[i].mu.Lock()
		for _, pushed := range ingesters[i].pushed {
			for j := range pushed.Streams {
				stream := pushed.Streams[j]
				for k := range stream.Entries {
					resource, scope := stream.SharedFor(&stream.Entries[k])
					out = append(out, push.EffectiveStructuredMetadata(resource, scope, stream.Entries[k].StructuredMetadata))
				}
			}
		}
		ingesters[i].mu.Unlock()

		if len(out) > 0 {
			return out
		}
	}

	t.Fatal("no entry reached any ingester")
	return nil
}

// parityPairs canonicalizes the effective structured metadata of a run so that two runs can be
// compared as multisets of pairs, independently of the order they are laid out in. The sort is
// stable and by name only, so pairs sharing a name keep their relative order and last-wins
// resolution is still visible. See residual 1 in the doc comment of
// Distributor.sanitizeSharedStructuredMetadata.
func parityPairs(entries []push.LabelsAdapter) []push.LabelsAdapter {
	out := make([]push.LabelsAdapter, 0, len(entries))
	for _, entry := range entries {
		pairs := slices.Clone(entry)
		slices.SortStableFunc(pairs, func(a, b push.LabelAdapter) int {
			return strings.Compare(a.Name, b.Name)
		})
		out = append(out, pairs)
	}
	return out
}

// parityMultiset is parityPairs sorted by value as well as by name, so that two layouts holding
// the very same pairs compare equal even when they disagree on the order of a repeated name. That
// disagreement is what decides a name collision, so it is asserted separately by parityResolved.
func parityMultiset(entries []push.LabelsAdapter) []push.LabelsAdapter {
	out := make([]push.LabelsAdapter, 0, len(entries))
	for _, entry := range entries {
		pairs := slices.Clone(entry)
		slices.SortStableFunc(pairs, func(a, b push.LabelAdapter) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.Value, b.Value)
		})
		out = append(out, pairs)
	}
	return out
}

// parityResolved collapses each entry's structured metadata the way the read path does: a name
// carried by more than one pair resolves to the last of them. This is what a query actually sees,
// and therefore the only thing a precedence change is visible in.
func parityResolved(entries []push.LabelsAdapter) []map[string]string {
	out := make([]map[string]string, 0, len(entries))
	for _, entry := range entries {
		resolved := make(map[string]string, len(entry))
		for _, l := range entry {
			resolved[l.Name] = l.Value
		}
		out = append(out, resolved)
	}
	return out
}

// TestDistributor_DeferredExpansionParity pushes one and the same OTLP payload through the whole
// distributor with the deferred expansion of resource and scope attributes off and then on, and
// compares the structured metadata the two runs leave an entry carrying.
//
// It is the regression guard for the parity the flag promises, and at the same time the record of
// where it deliberately falls short: every residual documented next to
// Distributor.sanitizeSharedStructuredMetadata has a case here asserting the divergent outcome, so
// that closing one of them fails this test rather than passing unnoticed.
//
// Name collisions are the one divergence that is intended rather than residual. The deferred path
// materializes the entry's own attributes LAST (see push.EffectiveStructuredMetadata), so a name
// carried by both the log record and a shared set resolves to the record's value - the
// own > scope > resource precedence OpenTelemetry prescribes - where the expanded path lets the
// shared value win. Those cases are marked differentPrecedence and assert both resolutions.
func TestDistributor_DeferredExpansionParity(t *testing.T) {
	ts := time.Now()

	for _, tc := range []struct {
		name          string
		resourceAttrs []parityAttr
		scopeAttrs    []parityAttr
		recordAttrs   []parityAttr

		// expectedOff and expectedOn are the effective structured metadata of the payload's single
		// entry, in the exact order each mode leaves it in.
		expectedOff push.LabelsAdapter
		expectedOn  push.LabelsAdapter
		// differentPairs marks the cases that diverge in which pairs are stored, not just in which
		// order.
		differentPairs bool
		// differentPrecedence marks the cases that store the very same pairs but resolve a
		// repeated name to a different one of them. resolvedOff and resolvedOn are what a query
		// sees in each mode.
		differentPrecedence bool
		resolvedOff         map[string]string
		resolvedOn          map[string]string
	}{
		{
			// The headline guarantee: ordinary attributes come out as the very same pairs either
			// way. The name carried by both the record and the resource is the intended
			// precedence divergence - same pairs, different winner.
			name:          "ordinary attributes, including a name collision",
			resourceAttrs: []parityAttr{{"host.name", "host-a"}, {"shared", "from-resource"}},
			scopeAttrs:    []parityAttr{{"scope.attr", "one"}},
			recordAttrs:   []parityAttr{{"zz.record", "r1"}, {"shared", "from-record"}},
			// Sorted, because the expanded path hands the whole merged list to
			// logproto.FromLabelAdaptersToLabels.
			expectedOff: push.LabelsAdapter{
				{Name: "host_name", Value: "host-a"},
				{Name: "scope_attr", Value: "one"},
				{Name: "shared", Value: "from-record"},
				{Name: "shared", Value: "from-resource"},
				{Name: "zz_record", Value: "r1"},
			},
			// Residual 1: the resource set, then the scope set, each in the order it was pooled
			// in, then the record's own attributes sorted among themselves. Same pairs, but the
			// record's copy of shared now comes last, so last-wins resolves shared to from-record
			// here and to from-resource on the expanded path.
			expectedOn: push.LabelsAdapter{
				{Name: "host_name", Value: "host-a"},
				{Name: "shared", Value: "from-resource"},
				{Name: "scope_attr", Value: "one"},
				{Name: "shared", Value: "from-record"},
				{Name: "zz_record", Value: "r1"},
			},
			differentPrecedence: true,
			resolvedOff: map[string]string{
				"host_name":  "host-a",
				"scope_attr": "one",
				"shared":     "from-resource",
				"zz_record":  "r1",
			},
			resolvedOn: map[string]string{
				"host_name":  "host-a",
				"scope_attr": "one",
				"shared":     "from-record",
				"zz_record":  "r1",
			},
		},
		{
			// An empty valued shared attribute is dropped by both modes. Before the pool
			// sanitization learned to drop them the deferred path stored host_name="" here, where
			// the expanded path's per-entry labels.Builder had always dropped it.
			name:          "empty valued shared attribute",
			resourceAttrs: []parityAttr{{"host.name", ""}},
			scopeAttrs:    []parityAttr{{"scope.attr", "one"}},
			recordAttrs:   []parityAttr{{"record.attr", "r1"}},
			expectedOff: push.LabelsAdapter{
				{Name: "record_attr", Value: "r1"},
				{Name: "scope_attr", Value: "one"},
			},
			expectedOn: push.LabelsAdapter{
				{Name: "scope_attr", Value: "one"},
				{Name: "record_attr", Value: "r1"},
			},
		},
		{
			// Invalid UTF-8 is scrubbed out of a pooled value just as it is out of an entry's own
			// one, and here the two modes agree down to the order.
			name:          "invalid utf-8 in a shared value",
			resourceAttrs: []parityAttr{{"host.name", "ho\xc5st"}},
			scopeAttrs:    []parityAttr{{"scope.attr", "one"}},
			recordAttrs:   []parityAttr{{"a.record", "r1"}},
			expectedOff: push.LabelsAdapter{
				{Name: "a_record", Value: "r1"},
				{Name: "host_name", Value: "ho st"},
				{Name: "scope_attr", Value: "one"},
			},
			expectedOn: push.LabelsAdapter{
				{Name: "host_name", Value: "ho st"},
				{Name: "scope_attr", Value: "one"},
				{Name: "a_record", Value: "r1"},
			},
		},
		{
			// A record attribute colliding with the normalized form of a resource attribute name.
			// This is the case residual 2 would apply to, and it shows why that residual is out of
			// reach from here: the OTLP handler normalizes attribute names before it pools them, so
			// by the time the distributor sees the set there is no name left to rewrite and no pair
			// is deleted on either path. The two modes store the same pair of pairs and, being a
			// collision, resolve it to opposite values.
			name:          "a record attribute colliding with a normalized shared name",
			resourceAttrs: []parityAttr{{"host.name", "host-a"}},
			recordAttrs:   []parityAttr{{"host_name", "from-record"}},
			expectedOff: push.LabelsAdapter{
				{Name: "host_name", Value: "from-record"},
				{Name: "host_name", Value: "host-a"},
			},
			expectedOn: push.LabelsAdapter{
				{Name: "host_name", Value: "host-a"},
				{Name: "host_name", Value: "from-record"},
			},
			differentPrecedence: true,
			resolvedOff:         map[string]string{"host_name": "host-a"},
			resolvedOn:          map[string]string{"host_name": "from-record"},
		},
		{
			// Residual 3. The delete list of the expanded path's per-entry builder is keyed by name
			// and applies to the merged list, so the empty valued resource pair takes the record's
			// foo=bar with it. The deferred path only ever drops the resource pair.
			name:           "empty valued shared attribute colliding with a record attribute",
			resourceAttrs:  []parityAttr{{"foo", ""}},
			scopeAttrs:     []parityAttr{{"scope.attr", "one"}},
			recordAttrs:    []parityAttr{{"foo", "bar"}},
			differentPairs: true,
			expectedOff: push.LabelsAdapter{
				{Name: "scope_attr", Value: "one"},
			},
			expectedOn: push.LabelsAdapter{
				{Name: "scope_attr", Value: "one"},
				{Name: "foo", Value: "bar"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := parityBody(t, "a line", tc.resourceAttrs, tc.scopeAttrs, tc.recordAttrs, ts)

			off := parityPush(t, parityLimits(false), body)
			on := parityPush(t, parityLimits(true), body)

			require.Equal(t, []push.LabelsAdapter{tc.expectedOff}, off, "expanded path")
			require.Equal(t, []push.LabelsAdapter{tc.expectedOn}, on, "deferred path")

			if tc.differentPairs {
				require.NotEqual(t, parityPairs(off), parityPairs(on),
					"this case exists to pin a known divergence: if the two modes now store the same pairs the residual has been closed, and this expectation should go with it")
				return
			}

			if tc.differentPrecedence {
				// The same pairs, so nothing was gained or lost...
				require.Equal(t, parityMultiset(off), parityMultiset(on),
					"a precedence divergence must not change which pairs are stored")
				// ...but a repeated name resolves to the other one of them, on purpose.
				require.Equal(t, []map[string]string{tc.resolvedOff}, parityResolved(off),
					"expanded path resolution")
				require.Equal(t, []map[string]string{tc.resolvedOn}, parityResolved(on),
					"deferred path resolution")
				require.NotEqual(t, parityResolved(off), parityResolved(on),
					"this case exists to pin the own-wins precedence change: if the two modes now resolve the collision the same way, this expectation should go with it")
				return
			}

			// Whatever order each mode lays them out in, the pairs themselves must match.
			require.Equal(t, parityPairs(off), parityPairs(on),
				"the two modes must store the same structured metadata pairs")
		})
	}
}

// TestDistributor_DeferredExpansionGenericFieldResidual pins the generic field detection residual
// documented in extractGenericField: when a resource attribute already carries the name of a
// configured detected field, the deferred path stores the resource value alone, where the expanded
// path appends a second, line-detected pair that wins the read path's last-wins resolution.
func TestDistributor_DeferredExpansionGenericFieldResidual(t *testing.T) {
	ts := time.Now()

	limitsFor := func(deferExpansion bool) *validation.Limits {
		limits := parityLimits(deferExpansion)
		// The field is named after a resource attribute the payload carries, and its hint matches
		// nothing in the labels or the metadata, so a detected value can only come from the line.
		limits.DiscoverGenericFields = validation.FieldDetectorConfig{
			Fields: map[string][]string{"trace_id": {"traceID"}},
		}
		return limits
	}

	body := parityBody(t, `{"traceID":"from-line"}`,
		[]parityAttr{{"trace.id", "from-resource"}}, nil, nil, ts)

	off := parityPush(t, limitsFor(false), body)
	on := parityPush(t, limitsFor(true), body)

	// Expanded: the resource attribute is part of the entry's own metadata by the time detection
	// runs, so the detected pair is appended after it and wins last-wins.
	require.Equal(t, []push.LabelsAdapter{{
		{Name: "trace_id", Value: "from-resource"},
		{Name: "trace_id", Value: "from-line"},
	}}, off, "expanded path")

	// Deferred: the guard in extractGenericField sees trace_id in the resource set and detects
	// nothing, so the resource value is all that is stored.
	require.Equal(t, []push.LabelsAdapter{{
		{Name: "trace_id", Value: "from-resource"},
	}}, on, "deferred path")
}
