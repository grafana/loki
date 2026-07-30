package ingester

import (
	"context"
	"math"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	gokit_log "github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"

	pushtypes "github.com/grafana/loki/pkg/push"
)

// sm builds a structured metadata list from name/value pairs.
func sm(pairs ...string) pushtypes.LabelsAdapter {
	out := make(pushtypes.LabelsAdapter, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, pushtypes.LabelAdapter{Name: pairs[i], Value: pairs[i+1]})
	}
	return out
}

// smPool builds a stream's shared structured metadata pool. Entries reference its sets 1-based, so
// the first set passed here is reference 1.
func smPool(sets ...pushtypes.LabelsAdapter) []logproto.SharedStructuredMetadataSet {
	out := make([]logproto.SharedStructuredMetadataSet, 0, len(sets))
	for _, s := range sets {
		out = append(out, logproto.SharedStructuredMetadataSet{Attrs: s})
	}
	return out
}

// smEntry builds an entry referencing the resource and scope sets of its stream's pool. A 0
// reference means the entry references no set.
func smEntry(ts int64, line string, own pushtypes.LabelsAdapter, resourceRef, scopeRef uint32) logproto.Entry {
	return logproto.Entry{
		Timestamp:          time.Unix(ts, 0),
		Line:               line,
		StructuredMetadata: own,
		SharedResourceRef:  resourceRef,
		SharedScopeRef:     scopeRef,
	}
}

// effectiveSM is the structured metadata a producer that expanded the pool would have put on an
// entry. It mirrors pushtypes.EffectiveStructuredMetadata, which today emits the entry's own
// attributes first and then the shared ones, so that a name carried by both resolves to the
// shared value under the read path's last-pair-wins collapse.
func effectiveSM(resource, scope, own pushtypes.LabelsAdapter) pushtypes.LabelsAdapter {
	out := make(pushtypes.LabelsAdapter, 0, len(resource)+len(scope)+len(own))
	out = append(out, own...)
	out = append(out, resource...)
	out = append(out, scope...)
	return out
}

// expandedEntries returns the entries an expanding producer would have sent for a stream with
// this pool: every entry carrying its full effective structured metadata and referencing
// nothing, so the resulting stream needs no pool at all.
func expandedEntries(entries []logproto.Entry, sets []logproto.SharedStructuredMetadataSet) []logproto.Entry {
	view := pushtypes.Stream{SharedStructuredMetadataSets: sets}

	out := make([]logproto.Entry, len(entries))
	for i := range entries {
		resource, scope := view.SharedFor(&entries[i])

		out[i] = entries[i]
		out[i].StructuredMetadata = effectiveSM(resource, scope, entries[i].StructuredMetadata)
		out[i].SharedResourceRef = 0
		out[i].SharedScopeRef = 0
	}

	return out
}

// readSM is what the read path materializes for a structured metadata list: sorted by label
// name, with pairs repeating a name collapsed down to the last one.
func readSM(metas pushtypes.LabelsAdapter) pushtypes.LabelsAdapter {
	sorted := slices.Clone(metas)
	slices.SortStableFunc(sorted, func(a, b logproto.LabelAdapter) int { return strings.Compare(a.Name, b.Name) })

	deduped := sorted[:0]
	for i, l := range sorted {
		if i+1 < len(sorted) && sorted[i+1].Name == l.Name {
			continue
		}
		deduped = append(deduped, l)
	}

	return deduped
}

// fixedRateStrategy pins a stream's rate limit, so that StreamRateLimiter.AllowN keeps the
// limiter it was built with instead of rebuilding it from the tenant's live configuration on
// its first recheck.
type fixedRateStrategy struct {
	limit rate.Limit
	burst int
}

func (s *fixedRateStrategy) RateLimit(_, _ string) validation.RateLimit {
	return validation.RateLimit{Limit: s.limit, Burst: s.burst}
}

func (s *fixedRateStrategy) SetDisabled(bool) {}

func valueOfSM(metas pushtypes.LabelsAdapter, name string) string {
	for _, l := range metas {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

func newSharedMetadataStream(t testing.TB, calc *StreamRateCalculator) *stream {
	t.Helper()

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	chunkfmt, headfmt := defaultChunkFormat(t)

	if calc == nil {
		calc = NewStreamRateCalculator()
	}

	return newStream(
		chunkfmt,
		headfmt,
		defaultConfig(),
		limiter.rateLimitStrategy,
		"fake",
		model.Fingerprint(0),
		labels.FromStrings("foo", "bar"),
		calc,
		NilMetrics,
		nil,
		nil,
		util.RetentionHours(limiter.limits.RetentionPeriod("fake")),
		noPolicy,
	)
}

func readStreamEntries(t testing.TB, s *stream) []logproto.Entry {
	t.Helper()

	it, err := s.Iterator(
		context.Background(),
		nil,
		time.Unix(0, 0),
		time.Unix(0, math.MaxInt64),
		logproto.FORWARD,
		log.NewNoopPipeline().ForStream(labels.EmptyLabels()),
	)
	require.NoError(t, err)
	defer it.Close() //nolint:errcheck

	var out []logproto.Entry
	for it.Next() {
		out = append(out, it.At())
	}
	require.NoError(t, it.Err())

	return out
}

func closedChunkBytes(t testing.TB, s *stream) [][]byte {
	t.Helper()

	out := make([][]byte, 0, len(s.chunks))
	for i := range s.chunks {
		require.NoError(t, s.chunks[i].chunk.Close())
		b, err := s.chunks[i].chunk.Bytes()
		require.NoError(t, err)
		out = append(out, b)
	}

	return out
}

// TestStreamPushSharedStructuredMetadataSets is the ingester side guardrail of deferred OTLP
// attribute expansion: pushing entries that reference their stream's pool of shared structured
// metadata must build exactly the chunk an equivalent expanding producer builds, and read back
// the same entries.
//
// Every case keeps the pool's names disjoint from the entries' own names. Byte identity is only
// guaranteed without such a collision, since the expanding path sorts the pre-merged union with
// an unstable sort; the precedence a collision resolves to is asserted separately, below.
func TestStreamPushSharedStructuredMetadataSets(t *testing.T) {
	resource := sm("service_name", "checkout", "cluster", "eu-west-2")
	scope := sm("scope_name", "otelhttp", "scope_version", "1.2.0")
	otherResource := sm("service_name", "payments", "cluster", "us-east-1")

	for _, tc := range []struct {
		name    string
		sets    []logproto.SharedStructuredMetadataSet
		entries []logproto.Entry
	}{
		{
			name: "one resource set referenced by every entry",
			sets: smPool(resource),
			entries: []logproto.Entry{
				smEntry(1, "one", nil, 1, 0),
				smEntry(2, "two", sm("trace_id", "abc"), 1, 0),
			},
		},
		{
			name: "resource and scope set",
			sets: smPool(resource, scope),
			entries: []logproto.Entry{
				smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
				smEntry(2, "two", nil, 1, 2),
			},
		},
		{
			name: "two resources coexisting in one stream",
			sets: smPool(resource, otherResource, scope),
			entries: []logproto.Entry{
				smEntry(1, "one", sm("trace_id", "abc"), 1, 3),
				smEntry(2, "two", sm("trace_id", "def"), 2, 3),
				smEntry(3, "three", nil, 1, 3),
				smEntry(4, "four", sm("span_id", "1"), 2, 0),
			},
		},
		{
			name: "partial references, including entries referencing nothing",
			sets: smPool(resource, scope),
			entries: []logproto.Entry{
				smEntry(1, "one", sm("trace_id", "abc"), 1, 0),
				smEntry(2, "two", nil, 0, 2),
				smEntry(3, "three", sm("span_id", "1"), 1, 2),
				smEntry(4, "four", sm("span_id", "2"), 0, 0),
			},
		},
		{
			name: "unsorted attributes in the pool",
			sets: smPool(sm("zone", "a", "cluster", "eu-west-2", "app", "checkout")),
			entries: []logproto.Entry{
				smEntry(1, "one", sm("trace_id", "abc", "a_first", "x"), 1, 0),
				smEntry(2, "two", sm("zzz", "last"), 1, 0),
			},
		},
		{
			name: "out of range reference is treated as no set",
			sets: smPool(resource),
			entries: []logproto.Entry{
				smEntry(1, "one", sm("trace_id", "abc"), 1, 0),
				smEntry(2, "two", sm("trace_id", "def"), 7, 9),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deferredStream := newSharedMetadataStream(t, nil)
			expandedStream := newSharedMetadataStream(t, nil)

			// The entries and the pool handed to the deferred push are the caller's: they must
			// come back unmodified, they are aliased by the WAL, replication and tailer paths.
			deferredEntries := slices.Clone(tc.entries)
			entriesBefore := slices.Clone(deferredEntries)
			poolBefore := slices.Clone(tc.sets)

			_, err := deferredStream.Push(context.Background(), deferredEntries, tc.sets, nil, 0, true, false, nil, "otlp")
			require.NoError(t, err)

			_, err = expandedStream.Push(context.Background(), expandedEntries(tc.entries, tc.sets), nil, nil, 0, true, false, nil, "otlp")
			require.NoError(t, err)

			require.Equal(t, entriesBefore, deferredEntries, "Push must not modify the entries it is given")
			require.Equal(t, poolBefore, tc.sets, "Push must not modify the pool it is given")

			// Read side: the entries materialize resource++scope++own merged.
			deferredRead := readStreamEntries(t, deferredStream)
			require.Equal(t, readStreamEntries(t, expandedStream), deferredRead)
			require.Len(t, deferredRead, len(tc.entries))

			want := expandedEntries(tc.entries, tc.sets)
			for i, e := range deferredRead {
				require.Equal(t, readSM(want[i].StructuredMetadata), e.StructuredMetadata,
					"entry %d structured metadata", i)
			}

			// Flush side: the chunks are byte for byte identical.
			require.Equal(t, closedChunkBytes(t, expandedStream), closedChunkBytes(t, deferredStream))
		})
	}
}

// TestStreamPushSharedStructuredMetadataPrecedence pins the precedence a name present in more
// than one place resolves to at read time: the entry's own value beats the scope value, which
// beats the resource value.
func TestStreamPushSharedStructuredMetadataPrecedence(t *testing.T) {
	sets := smPool(
		sm("attr", "resource", "only_resource", "r"),
		sm("attr", "scope", "only_scope", "s"),
	)

	s := newSharedMetadataStream(t, nil)
	_, err := s.Push(context.Background(), []logproto.Entry{
		// Collides with both the resource and the scope set.
		smEntry(1, "own wins", sm("attr", "own"), 1, 2),
		// Collides with the resource set only, through the scope set.
		smEntry(2, "scope wins", nil, 1, 2),
		// No scope set, so the resource value stands.
		smEntry(3, "resource stands", nil, 1, 0),
	}, sets, nil, 0, true, false, nil, "otlp")
	require.NoError(t, err)

	read := readStreamEntries(t, s)
	require.Len(t, read, 3)

	require.Equal(t, "own", valueOfSM(read[0].StructuredMetadata, "attr"), "own must win over scope and resource")
	require.Equal(t, "scope", valueOfSM(read[1].StructuredMetadata, "attr"), "scope must win over resource")
	require.Equal(t, "resource", valueOfSM(read[2].StructuredMetadata, "attr"), "the resource value stands with no scope set")

	// The non colliding attributes of both sets survive regardless.
	require.Equal(t, "r", valueOfSM(read[0].StructuredMetadata, "only_resource"))
	require.Equal(t, "s", valueOfSM(read[0].StructuredMetadata, "only_scope"))
}

// TestInstancePushSharedStructuredMetadataSets is the flagship end to end scenario. Two
// resources, each with its own scope, that resolve to the same stream labels now form a single
// wire stream carrying a pool of sets, with the entries telling apart which attributes are
// theirs. The instance must thread the pool down to the stream and build the same chunks an
// expanding producer would.
func TestInstancePushSharedStructuredMetadataSets(t *testing.T) {
	const streamLabels = `{foo="bar"}`

	sets := smPool(
		sm("service_name", "checkout", "cluster", "eu-west-2"), // resource A
		sm("service_name", "payments", "cluster", "us-east-1"), // resource B
		sm("scope_name", "otelhttp"),                           // scope of A
		sm("scope_name", "otelgrpc"),                           // scope of B
	)
	entries := []logproto.Entry{
		smEntry(1, "from A", sm("trace_id", "abc"), 1, 3),
		smEntry(2, "from B", sm("trace_id", "def"), 2, 4),
		smEntry(3, "from A again", nil, 1, 3),
		smEntry(4, "from B, no scope", sm("span_id", "1"), 2, 0),
	}

	newTestInstance := func(t *testing.T) *instance {
		t.Helper()

		limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
		require.NoError(t, err)
		limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})

		inst, err := newInstance(
			defaultConfig(), defaultPeriodConfigs, "test", limiter, runtime.DefaultTenantConfigs(),
			noopWAL{}, NilMetrics, &OnceSwitch{}, nil, nil, nil, NewStreamRateCalculator(), nil, nil,
			retention.NewTenantsRetention(limits),
		)
		require.NoError(t, err)

		return inst
	}

	// Deferred: one wire stream, the attributes carried once in its pool.
	deferredInstance := newTestInstance(t)
	require.NoError(t, deferredInstance.Push(context.Background(), &logproto.PushRequest{
		Streams: []logproto.Stream{{
			Labels:                       streamLabels,
			Entries:                      slices.Clone(entries),
			SharedStructuredMetadataSets: sets,
		}},
		Format: "otlp",
	}))

	// Expanded: the same logical data, copied onto every entry.
	expandedInstance := newTestInstance(t)
	require.NoError(t, expandedInstance.Push(context.Background(), &logproto.PushRequest{
		Streams: []logproto.Stream{{
			Labels:  streamLabels,
			Entries: expandedEntries(entries, sets),
		}},
		Format: "otlp",
	}))

	loadStream := func(inst *instance) *stream {
		s, ok := inst.streams.Load(streamLabels)
		require.True(t, ok)
		return s
	}

	deferredStream, expandedStream := loadStream(deferredInstance), loadStream(expandedInstance)

	// One stream, all four entries, each with the attributes of its own resource and scope.
	read := readStreamEntries(t, deferredStream)
	require.Equal(t, readStreamEntries(t, expandedStream), read)
	require.Len(t, read, len(entries))

	want := expandedEntries(entries, sets)
	for i, e := range read {
		require.Equal(t, readSM(want[i].StructuredMetadata), e.StructuredMetadata, "entry %d", i)
	}

	// Flush side: byte for byte identical chunks.
	require.Equal(t, closedChunkBytes(t, expandedStream), closedChunkBytes(t, deferredStream))
}

// TestStreamPushSharedStructuredMetadataDuplicates covers the data loss this feature could
// have caused: entries are stored unexpanded, so two entries that only differ by which shared
// sets they reference look identical to the duplicate check.
func TestStreamPushSharedStructuredMetadataDuplicates(t *testing.T) {
	checkout := sm("service_name", "checkout")
	payments := sm("service_name", "payments")
	scope := sm("scope_name", "otelhttp")

	t.Run("different resource set is not a duplicate", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		_, err := s.Push(context.Background(), []logproto.Entry{
			smEntry(1, "same line", sm("trace_id", "abc"), 1, 0),
			smEntry(1, "same line", sm("trace_id", "abc"), 2, 0),
		}, smPool(checkout, payments), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		read := readStreamEntries(t, s)
		require.Len(t, read, 2, "entries from different resources must both be stored")

		got := []pushtypes.LabelsAdapter{read[0].StructuredMetadata, read[1].StructuredMetadata}
		sort.Slice(got, func(i, j int) bool {
			return logproto.FromLabelAdaptersToLabels(got[i]).String() < logproto.FromLabelAdaptersToLabels(got[j]).String()
		})
		require.Equal(t, []pushtypes.LabelsAdapter{
			readSM(effectiveSM(checkout, nil, sm("trace_id", "abc"))),
			readSM(effectiveSM(payments, nil, sm("trace_id", "abc"))),
		}, got)
	})

	t.Run("same resource but different scope is not a duplicate", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		_, err := s.Push(context.Background(), []logproto.Entry{
			smEntry(1, "same line", nil, 1, 0),
			smEntry(1, "same line", nil, 1, 2),
		}, smPool(checkout, scope), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		require.Len(t, readStreamEntries(t, s), 2, "the scope set is part of an entry's shared identity")
	})

	t.Run("swapped colliding references are not a duplicate", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		// The same two sets in opposite roles. They collide on a name, so which value wins
		// depends on which set is the scope: the two entries mean different things and both
		// have to be stored.
		_, err := s.Push(context.Background(), []logproto.Entry{
			smEntry(1, "same line", nil, 1, 2),
			smEntry(1, "same line", nil, 2, 1),
		}, smPool(sm("attr", "resource"), sm("attr", "scope")), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		read := readStreamEntries(t, s)
		require.Len(t, read, 2)

		// Whichever set played the scope role wins, so the two entries read back differently.
		got := []string{valueOfSM(read[0].StructuredMetadata, "attr"), valueOfSM(read[1].StructuredMetadata, "attr")}
		sort.Strings(got)
		require.Equal(t, []string{"resource", "scope"}, got)
	})

	t.Run("swapped non colliding references are a duplicate", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		// Swapping the roles of two sets that share no label name leaves the effective
		// metadata unchanged, so these two entries really are the same entry.
		_, err := s.Push(context.Background(), []logproto.Entry{
			smEntry(1, "same line", nil, 1, 2),
			smEntry(1, "same line", nil, 2, 1),
		}, smPool(sm("a", "1"), sm("b", "2")), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		read := readStreamEntries(t, s)
		require.Len(t, read, 1)
		require.Equal(t, readSM(sm("a", "1", "b", "2")), read[0].StructuredMetadata)
	})

	t.Run("identical references are a duplicate", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		_, err := s.Push(context.Background(), []logproto.Entry{
			smEntry(1, "same line", sm("trace_id", "abc"), 1, 2),
			smEntry(1, "same line", sm("trace_id", "abc"), 1, 2),
		}, smPool(checkout, scope), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		require.Len(t, readStreamEntries(t, s), 1, "the second identical entry must be dropped as a duplicate")
	})

	t.Run("distinct sets with identical content are a duplicate", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		// Two pool slots holding the same attributes: what identifies an entry's shared
		// metadata is the content, not the index it came from.
		_, err := s.Push(context.Background(), []logproto.Entry{
			smEntry(1, "same line", nil, 1, 0),
			smEntry(1, "same line", nil, 2, 0),
		}, smPool(checkout, sm("service_name", "checkout")), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		require.Len(t, readStreamEntries(t, s), 1)
	})

	t.Run("across pushes", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)
		e := []logproto.Entry{smEntry(1, "same line", sm("trace_id", "abc"), 1, 0)}

		_, err := s.Push(context.Background(), slices.Clone(e), smPool(checkout), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		// A different pool holding the same content behind the same reference: still a duplicate.
		_, err = s.Push(context.Background(), slices.Clone(e), smPool(sm("service_name", "checkout")), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)
		require.Len(t, readStreamEntries(t, s), 1)

		// Different content behind the same reference: not a duplicate.
		_, err = s.Push(context.Background(), slices.Clone(e), smPool(payments), nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)
		require.Len(t, readStreamEntries(t, s), 2)
	})
}

// TestStreamPushSharedStructuredMetadataRateAccounting pins the two units a push is measured in,
// and the fact that they deliberately differ.
//
// The per-stream rate LIMITER is tenant-facing and unexpanded: the pool is stored once per stream,
// so it is charged once per push, every set of it whether or not an entry references it, keeping
// the ingester consistent with how the distributor meters. That is the subtests below.
//
// The stream rate CALCULATOR is not tenant-facing - it feeds the rate store the distributor reads
// to size stream sharding - and is expanded-equivalent: every entry is charged for the sets it
// references, so a pooled push records exactly what the same payload would have recorded with
// otlp_defer_structured_metadata_expansion off. That is the table below, which asserts both the
// arithmetic and the flag-on/flag-off equality directly.
func TestStreamPushSharedStructuredMetadataRateAccounting(t *testing.T) {
	resource := sm("service_name", "checkout", "cluster", "eu-west-2")
	scope := sm("scope_name", "otelhttp")
	unreferenced := sm("never", "referenced-by-any-entry")

	recordedRate := func(t *testing.T, entries []logproto.Entry, sets []logproto.SharedStructuredMetadataSet) int64 {
		t.Helper()

		calc := NewStreamRateCalculator()
		defer calc.Stop()

		s := newSharedMetadataStream(t, calc)
		_, err := s.Push(context.Background(), entries, sets, nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		calc.updateRates()
		rates := calc.Rates()
		require.Len(t, rates, 1)

		return rates[0].Rate
	}

	entriesSize := func(entries []logproto.Entry) int64 {
		var total int64
		for i := range entries {
			total += int64(util.EntryTotalSize(&entries[i]))
		}
		return total
	}

	for _, tc := range []struct {
		name    string
		sets    []logproto.SharedStructuredMetadataSet
		entries []logproto.Entry
	}{
		{
			name: "every entry references both sets",
			sets: smPool(resource, scope),
			entries: []logproto.Entry{
				smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
				smEntry(2, "two", nil, 1, 2),
				smEntry(3, "three", nil, 1, 2),
			},
		},
		{
			name: "entries reference set 1 only",
			sets: smPool(resource, scope),
			entries: []logproto.Entry{
				smEntry(1, "one", nil, 1, 0),
				smEntry(2, "two", nil, 1, 0),
			},
		},
		{
			name: "entries reference set 2 only",
			sets: smPool(resource, scope),
			entries: []logproto.Entry{
				smEntry(1, "one", nil, 0, 2),
				smEntry(2, "two", nil, 0, 2),
			},
		},
		{
			name: "entries reference neither set",
			sets: smPool(resource, scope),
			entries: []logproto.Entry{
				smEntry(1, "one", nil, 0, 0),
				smEntry(2, "two", nil, 0, 0),
			},
		},
		{
			name: "pool holds a set no entry references",
			sets: smPool(resource, scope, unreferenced),
			entries: []logproto.Entry{
				smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
				smEntry(2, "two", nil, 1, 0),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Each entry charged for the sets it references, and the pool never charged on its
			// own: a set no entry references costs nothing. Notably this depends on which sets
			// the entries reference, which is the whole difference from the unexpanded unit.
			expanded := expandedEntries(tc.entries, tc.sets)
			want := entriesSize(expanded)
			require.Equal(t, want, recordedRate(t, slices.Clone(tc.entries), tc.sets),
				"the rate calculator must record the expanded-equivalent size")

			// And the point of that unit: the very same payload with the expansion already done
			// by the producer, carrying no pool at all, records the same rate. Sharding is sized
			// off this number, so a stream must shard the same way either way.
			require.Equal(t, want, recordedRate(t, slices.Clone(expanded), nil),
				"a pooled push and the equivalent expanded push must record the same rate")
		})
	}

	// The limiter charge is asserted behaviourally rather than by reading the token count:
	// tokens refill with wall clock time, which makes any arithmetic on them racy. A rate of
	// one byte per second makes the refill over a test negligible, so the burst alone decides
	// what fits, and whether the batch is admitted tells us exactly what was charged for.
	pushWithBurst := func(t *testing.T, burst int, entries []logproto.Entry, sets []logproto.SharedStructuredMetadataSet) error {
		t.Helper()

		s := newSharedMetadataStream(t, nil)
		s.limiter = NewStreamRateLimiter(&fixedRateStrategy{limit: 1, burst: burst}, "fake", noPolicy, time.Hour)

		_, err := s.Push(context.Background(), entries, sets, nil, 0, true, false, nil, "otlp")
		return err
	}

	t.Run("the pool is part of the limiter charge, once", func(t *testing.T) {
		sets := smPool(resource, scope, unreferenced)
		entries := []logproto.Entry{
			smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
			smEntry(2, "two", nil, 1, 2),
		}

		entryBytes := entriesSize(entries)
		poolBytes := int64(util.SharedSetsSize(sets))
		require.Positive(t, poolBytes)

		// Enough for every entry plus one copy of the pool.
		require.NoError(t, pushWithBurst(t, int(entryBytes+poolBytes), entries, sets),
			"the pool must be charged once per batch, not once per entry")

		// One byte short: the pool has to be in the charge for this to be rejected.
		err := pushWithBurst(t, int(entryBytes+poolBytes)-1, entries, sets)
		require.Error(t, err, "the pool must be included in the limiter charge")
		require.Contains(t, err.Error(), "Per stream rate limit exceeded")

		// Enough for the entries but not for the pool: still rejected, for the same reason.
		err = pushWithBurst(t, int(entryBytes), entries, sets)
		require.Error(t, err)
		require.Contains(t, err.Error(), "Per stream rate limit exceeded")
	})

	t.Run("an all duplicate batch is charged nothing", func(t *testing.T) {
		sets := smPool(resource, scope)
		entries := []logproto.Entry{smEntry(1, "one", sm("trace_id", "abc"), 1, 2)}

		calc := NewStreamRateCalculator()
		defer calc.Stop()

		s := newSharedMetadataStream(t, calc)
		_, err := s.Push(context.Background(), slices.Clone(entries), sets, nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		calc.updateRates()
		first := calc.Rates()[0].Rate
		require.Positive(t, first)

		_, err = s.Push(context.Background(), slices.Clone(entries), sets, nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		calc.updateRates()
		rates := calc.Rates()
		require.Len(t, rates, 1)
		require.Zero(t, rates[0].Rate, "a batch of only duplicates must record no bytes at all")
	})
}

// TestStreamPushSharedStructuredMetadataWALRecord checks what a push puts in a WAL record: the
// entries exactly as they were pushed, keeping their own structured metadata and their pool
// references, plus the stream's pool alongside them. Nothing is expanded on the way in.
func TestStreamPushSharedStructuredMetadataWALRecord(t *testing.T) {
	sets := smPool(
		sm("service_name", "checkout", "cluster", "eu-west-2"),
		sm("scope_name", "otelhttp"),
	)
	entries := []logproto.Entry{
		smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
		smEntry(2, "two", nil, 1, 0),
	}

	s := newSharedMetadataStream(t, nil)
	record := recordPool.GetRecord()

	_, err := s.Push(context.Background(), slices.Clone(entries), sets, record, 0, true, false, nil, "otlp")
	require.NoError(t, err)

	require.Len(t, record.RefEntries, 1)
	require.Equal(t, entries, record.RefEntries[0].Entries,
		"the record must hold the entries as pushed, own metadata and references intact")
	require.Equal(t, sets, record.RefEntries[0].SharedStructuredMetadataSets,
		"the record must carry the stream's pool")

	// A pool is only expressible from V4 on, so that is what this record is written as.
	require.Equal(t, wal.WALRecordEntriesV4, record.EntriesVersion())
}

// TestStreamPushSharedStructuredMetadataWALReplay replays a WAL record produced by a push with a
// pool and checks the replay reconstructs the push exactly: the same entries read back, and
// byte for byte the same chunk. The latter is the property the earlier expand-on-write interim
// could not provide, because it changed what the replayed entries looked like.
func TestStreamPushSharedStructuredMetadataWALReplay(t *testing.T) {
	sets := smPool(
		sm("service_name", "checkout", "cluster", "eu-west-2"),
		sm("scope_name", "otelhttp"),
		sm("service_name", "payments"),
	)
	entries := []logproto.Entry{
		smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
		smEntry(2, "two", nil, 3, 0),
		// Same timestamp, line and own metadata as the entry above but a different resource:
		// only the references tell them apart, so this is what a replay losing them would drop.
		smEntry(2, "two", nil, 1, 0),
	}

	source := newSharedMetadataStream(t, nil)
	record := recordPool.GetRecord()
	_, err := source.Push(context.Background(), slices.Clone(entries), sets, record, 0, true, false, nil, "otlp")
	require.NoError(t, err)

	// Round trip the record through its wire encoding, as a real replay would.
	encoded := record.EncodeEntries(record.EntriesVersion(), nil)
	decoded := &wal.Record{}
	require.NoError(t, wal.DecodeRecord(encoded, decoded))
	require.Len(t, decoded.RefEntries, 1)
	require.Equal(t, sets, decoded.RefEntries[0].SharedStructuredMetadataSets)

	// Replay hands the stream back the decoded pool, which is what recovery.go does.
	replayed := newSharedMetadataStream(t, nil)
	_, err = replayed.Push(context.Background(), decoded.RefEntries[0].Entries, decoded.RefEntries[0].SharedStructuredMetadataSets, nil, 0, true, false, nil, "loki")
	require.NoError(t, err)

	read := readStreamEntries(t, replayed)
	require.Equal(t, readStreamEntries(t, source), read)
	require.Len(t, read, len(entries), "the entries told apart only by their references must all survive")

	// Two of the entries share a timestamp, and the iterator is free to return those in either
	// order, so compare what was materialized as a set.
	asStrings := func(entries []logproto.Entry, sm func(logproto.Entry) pushtypes.LabelsAdapter) []string {
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, logproto.FromLabelAdaptersToLabels(sm(e)).String())
		}
		sort.Strings(out)
		return out
	}

	want := expandedEntries(entries, sets)
	require.Equal(t,
		asStrings(want, func(e logproto.Entry) pushtypes.LabelsAdapter { return readSM(e.StructuredMetadata) }),
		asStrings(read, func(e logproto.Entry) pushtypes.LabelsAdapter { return e.StructuredMetadata }),
		"the replayed entries must materialize the same structured metadata")

	// The replay rebuilt the very same chunk, not merely an equivalent one.
	require.Equal(t, closedChunkBytes(t, source), closedChunkBytes(t, replayed))
}

// TestIngesterWALReplaySharedStructuredMetadata is the full cycle through a real ingester: push
// with a pool, write the WAL, restart, replay the segments. The replayed chunks must be byte for
// byte the chunks an equivalent expanding push builds live, which is what proves nothing was
// lost or reshaped on the way through the WAL.
//
// This exercises recovery.go handing the decoded pool back to stream.Push. It is also the
// property the earlier expand-on-write interim could not provide.
func TestIngesterWALReplaySharedStructuredMetadata(t *testing.T) {
	const streamLabels = `{foo="bar"}`

	sets := smPool(
		sm("service_name", "checkout", "cluster", "eu-west-2"),
		sm("service_name", "payments", "cluster", "us-east-1"),
		sm("scope_name", "otelhttp"),
	)
	entries := []logproto.Entry{
		smEntry(1, "from A", sm("trace_id", "abc"), 1, 3),
		smEntry(2, "from B", sm("trace_id", "def"), 2, 3),
		smEntry(3, "from A again", nil, 1, 0),
		// Told apart from the entry above only by which resource it references.
		smEntry(3, "from A again", nil, 2, 0),
	}

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	readRingMock := mockReadRingWithOneActiveIngester()

	newIngester := func(t *testing.T, walDir string) *Ingester {
		t.Helper()

		cfg := defaultIngesterTestConfigWithWAL(t, walDir)
		// Keep checkpointing out of the picture: a checkpoint stores encoded chunks, so
		// recovering from one would bypass the segment replay this test is about.
		cfg.WAL.CheckpointDuration = time.Hour

		ing, err := New(cfg, client.Config{}, &mockStore{chunks: map[string][]chunk.Chunk{}}, limits,
			runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki,
			gokit_log.NewNopLogger(), nil, readRingMock, nil)
		require.NoError(t, err)
		require.NoError(t, services.StartAndAwaitRunning(context.Background(), ing))

		return ing
	}

	chunksOf := func(t *testing.T, ing *Ingester) [][]byte {
		t.Helper()

		inst, ok := ing.instances["test"]
		require.True(t, ok, "instance was not recovered")
		s, ok := inst.streams.Load(streamLabels)
		require.True(t, ok, "stream was not recovered")

		return closedChunkBytes(t, s)
	}

	ctx := user.InjectOrgID(context.Background(), "test")

	// The pooled push, through a WAL, then a restart that has to replay it.
	walDir := t.TempDir()
	deferred := newIngester(t, walDir)
	_, err = deferred.Push(ctx, &logproto.PushRequest{
		Streams: []logproto.Stream{{
			Labels:                       streamLabels,
			Entries:                      slices.Clone(entries),
			SharedStructuredMetadataSets: sets,
		}},
		Format: "otlp",
	})
	require.NoError(t, err)
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), deferred))

	replayed := newIngester(t, walDir)
	defer services.StopAndAwaitTerminated(context.Background(), replayed) //nolint:errcheck

	// The equivalent expanding push, live, as the reference.
	expanded := newIngester(t, t.TempDir())
	defer services.StopAndAwaitTerminated(context.Background(), expanded) //nolint:errcheck
	_, err = expanded.Push(ctx, &logproto.PushRequest{
		Streams: []logproto.Stream{{
			Labels:  streamLabels,
			Entries: expandedEntries(entries, sets),
		}},
		Format: "otlp",
	})
	require.NoError(t, err)

	// Every entry came back, including the pair only their references tell apart.
	replayedStream, ok := replayed.instances["test"].streams.Load(streamLabels)
	require.True(t, ok)
	require.Len(t, readStreamEntries(t, replayedStream), len(entries),
		"the entries told apart only by their references must all survive the replay")

	require.Equal(t, chunksOf(t, expanded), chunksOf(t, replayed),
		"replayed chunks must be byte for byte the chunks of an equivalent expanded push")
}

// TestStreamPushWALRecordEmissionPolicy pins the emission policy: only a push that actually
// carries a pool moves the record to V4, so segments written for every other tenant stay
// exactly what they were and stay readable by an ingester that predates V4.
func TestStreamPushWALRecordEmissionPolicy(t *testing.T) {
	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "one", StructuredMetadata: sm("trace_id", "abc")},
		{Timestamp: time.Unix(2, 0), Line: "two"},
	}

	t.Run("a pool-less push stays on the current version", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)
		record := recordPool.GetRecord()
		record.UserID = "fake"

		_, err := s.Push(context.Background(), entries, nil, record, 0, true, false, nil, "loki")
		require.NoError(t, err)

		require.Equal(t, wal.CurrentEntriesRec, record.EntriesVersion())
		require.Empty(t, record.RefEntries[0].SharedStructuredMetadataSets)

		// And the bytes are the ones the previous version wrote for the same record.
		require.Equal(t,
			record.EncodeEntries(wal.WALRecordEntriesV3, nil),
			record.EncodeEntries(record.EntriesVersion(), nil),
			"a pool-less record must encode to exactly the V3 bytes")
	})

	t.Run("a pooled push moves the record to V4", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)
		record := recordPool.GetRecord()
		record.UserID = "fake"

		_, err := s.Push(context.Background(), []logproto.Entry{smEntry(1, "one", nil, 1, 0)},
			smPool(sm("service_name", "checkout")), record, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		require.Equal(t, wal.WALRecordEntriesV4, record.EntriesVersion())
	})

	t.Run("one pooled stream moves the whole record to V4", func(t *testing.T) {
		// The version is a property of the record, so a record mixing a pooled and a pool-less
		// stream is written as V4 throughout, the pool-less stream carrying an empty pool.
		poolLess := newSharedMetadataStream(t, nil)
		pooled := newSharedMetadataStream(t, nil)
		pooled.fp = 1

		record := recordPool.GetRecord()
		record.UserID = "fake"

		_, err := poolLess.Push(context.Background(), entries, nil, record, 0, true, false, nil, "loki")
		require.NoError(t, err)
		_, err = pooled.Push(context.Background(), []logproto.Entry{smEntry(1, "one", nil, 1, 0)},
			smPool(sm("service_name", "checkout")), record, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		require.Len(t, record.RefEntries, 2)
		require.Equal(t, wal.WALRecordEntriesV4, record.EntriesVersion())

		decoded := &wal.Record{}
		require.NoError(t, wal.DecodeRecord(record.EncodeEntries(record.EntriesVersion(), nil), decoded))
		require.Len(t, decoded.RefEntries, 2)
		require.Empty(t, decoded.RefEntries[0].SharedStructuredMetadataSets)
		require.Len(t, decoded.RefEntries[1].SharedStructuredMetadataSets, 1)
	})
}

// TestStreamPushSharedStructuredMetadataRestartOverlap covers the overlap an abrupt restart
// produces: the same data reaches the stream twice and nothing may be stored twice because of it.
//
// The shape of that overlap changed with WALRecordEntriesV4. Both sources now hand the stream the
// same shape - entries referencing a pool the record carried - so the mainline case is a
// same-shape overlap, and what has to hold is that the identity the stream level check compares
// is derived from the CONTENT of the sets an entry references and not from the indices it
// carries, because the WAL record's pool and the queue record's pool are built independently and
// need not agree on layout. That is the first sub-test.
//
// The cross-shape overlap has not disappeared, it moved: a WAL segment written before this
// version was deployed holds materialized entries and no pool, so replaying it against pooled
// records off the queue still compares a materialized entry to a pooled one. The stream level
// check does not match those. The last sub-test pins what actually saves the data there, the
// chunk level duplicate check, which compares interned symbols and is therefore shape
// independent, and the residual that comes with relying on it.
func TestStreamPushSharedStructuredMetadataRestartOverlap(t *testing.T) {
	sets := smPool(
		sm("service_name", "checkout", "cluster", "eu-west-2"),
		sm("scope_name", "otelhttp"),
	)
	entries := []logproto.Entry{
		smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
		smEntry(2, "two", nil, 1, 0),
	}

	// The very same sets, pooled the other way round, as a record built by another producer may
	// well have them. The references are swapped to match, so every entry resolves to exactly
	// the sets it does against sets above.
	reorderedSets := smPool(
		sm("scope_name", "otelhttp"),
		sm("service_name", "checkout", "cluster", "eu-west-2"),
	)
	reordered := []logproto.Entry{
		smEntry(1, "one", sm("trace_id", "abc"), 2, 1),
		smEntry(2, "two", nil, 2, 0),
	}

	// Re-consuming a batch means pushing entries older than the highest timestamp the stream
	// has seen, so the stream needs the max chunk age a real one runs with. Without it the
	// validity window closes at the highest timestamp itself and every re-consumed entry but
	// the last is rejected as too far behind, which is not what this test is about.
	cfg := *defaultConfig()
	cfg.MaxChunkAge = 2 * time.Hour
	newOverlappingStream := func(t *testing.T) *stream {
		t.Helper()

		s := newSharedMetadataStream(t, nil)
		s.cfg = &cfg
		return s
	}

	// What a V4 WAL record replays, the way recovery.go replays it: the entries as they were
	// pushed, the pool the record carried, and a counter, which is what makes the push a replay.
	replay := func(t *testing.T, s *stream, batch []logproto.Entry, pool []logproto.SharedStructuredMetadataSet) {
		t.Helper()

		_, err := s.Push(context.Background(), slices.Clone(batch), pool, nil, int64(len(batch)), true, false, nil, "loki")
		require.NoError(t, err)
	}

	// The same logical batch as it comes back off the queue.
	reconsume := func(t *testing.T, s *stream, batch []logproto.Entry, pool []logproto.SharedStructuredMetadataSet) {
		t.Helper()

		_, err := s.Push(context.Background(), slices.Clone(batch), pool, nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)
	}

	t.Run("the stream level check matches across two independently built pools", func(t *testing.T) {
		// A single entry, so that the overlapping entry is the last line the stream remembers
		// and the stream level check is what has to catch it. entryCt only advances for entries
		// that reach a chunk, so it tells us whether the check did.
		s := newOverlappingStream(t)
		replay(t, s, entries[:1], sets)
		require.Equal(t, int64(1), s.entryCt)

		reconsume(t, s, reordered[:1], reorderedSets)
		require.Equal(t, int64(1), s.entryCt,
			"the re-consumed entry must be recognized as a duplicate of the replayed one and never reach a chunk")
		require.Len(t, readStreamEntries(t, s), 1)

		// What makes the two compare equal, spelled out: the identity is the pair hash, built
		// from the content hash of each set, so the differing pool layouts resolve to the same
		// value and the entry's own metadata compares equal pairwise.
		replayed, reconsumed := newOverlappingStream(t), newOverlappingStream(t)
		replay(t, replayed, entries[:1], sets)
		reconsume(t, reconsumed, reordered[:1], reorderedSets)
		require.NotZero(t, replayed.lastLine.sharedHash)
		require.Equal(t, replayed.lastLine, reconsumed.lastLine)
	})

	t.Run("no duplicate rows survive the overlap", func(t *testing.T) {
		// A batch of more than one entry: the stream level check only knows the last line, so
		// the earlier entries do reach the chunk, which drops them as the duplicates they are.
		s := newOverlappingStream(t)
		replay(t, s, entries, sets)
		reconsume(t, s, reordered, reorderedSets)

		read := readStreamEntries(t, s)
		require.Len(t, read, len(entries), "the overlapping entries must not be stored twice")

		want := expandedEntries(entries, sets)
		for i, e := range read {
			require.Equal(t, readSM(want[i].StructuredMetadata), e.StructuredMetadata, "entry %d", i)
		}

		// And the chunk is the one a single push of the batch builds.
		once := newOverlappingStream(t)
		reconsume(t, once, entries, sets)
		require.Equal(t, closedChunkBytes(t, once), closedChunkBytes(t, s))
	})

	t.Run("a pre-upgrade segment overlaps through the chunk level check", func(t *testing.T) {
		// A segment written before this version: entries already materialized, no pool at all.
		// This is the one overlap whose two sides still differ in shape.
		s := newOverlappingStream(t)
		_, err := s.Push(context.Background(), expandedEntries(entries, sets), nil, nil, int64(len(entries)), true, false, nil, "loki")
		require.NoError(t, err)

		before := s.entryCt
		reconsume(t, s, entries, sets)

		// The stream level check compares a materialized entry against a pooled one and does
		// not match them: a materialized entry carries everything as its own metadata and shares
		// nothing, so neither the pair hash nor the own list agrees. Every re-consumed entry
		// therefore reaches a chunk.
		require.Equal(t, before+int64(len(entries)), s.entryCt,
			"the stream level check is shape dependent and must be seen not to match here")

		// The chunk level check is what saves the data: it compares the interned symbols of the
		// stored metadata, which are the same symbols either way round, so no duplicate row is
		// stored.
		read := readStreamEntries(t, s)
		require.Len(t, read, len(entries), "the chunk level check must drop the re-consumed entries")

		// The residual, stated so that it is not mistaken for a guarantee: the chunk level check
		// only sees the head block of the open chunk. Had the chunk been cut or flushed between
		// the replay and the re-consume, nothing would have caught these. Closing that would mean
		// giving the stream level check a shape independent identity again, which is what the
		// interim materialize-once-per-batch ingester had and what this POC trades away.
	})
}

// TestStreamPushSharedStructuredMetadataTailers checks tail clients see the effective structured
// metadata with the pool references cleared, and that building that view never mutates what was
// stored.
func TestStreamPushSharedStructuredMetadataTailers(t *testing.T) {
	sets := smPool(
		sm("service_name", "checkout", "cluster", "eu-west-2"),
		sm("scope_name", "otelhttp"),
	)
	newEntries := func() []logproto.Entry {
		return []logproto.Entry{
			smEntry(1, "one", sm("trace_id", "abc"), 1, 2),
			smEntry(2, "two", nil, 1, 0),
		}
	}

	t.Run("tailed entries carry the effective structured metadata", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		expr, err := syntax.ParseLogSelector(`{foo="bar"}`, true)
		require.NoError(t, err)
		tail, err := newTailer("fake", expr, &fakeTailServer{}, 10)
		require.NoError(t, err)
		s.addTailer(tail)

		entries := newEntries()
		_, err = s.Push(context.Background(), entries, sets, nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		// The raw request off the tailer's queue is the stream recordAndSendToTailers handed
		// over, before any pipeline processing.
		var sent logproto.Stream
		select {
		case req := <-tail.queue:
			sent = req.stream
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the tailer to be sent the stream")
		}

		want := expandedEntries(newEntries(), sets)
		require.Len(t, sent.Entries, len(want))
		for i, e := range sent.Entries {
			require.Equal(t, want[i].StructuredMetadata, e.StructuredMetadata,
				"tailed entry %d must carry the effective structured metadata", i)
			require.Zero(t, e.SharedResourceRef, "tailed entry %d must not keep a resource reference", i)
			require.Zero(t, e.SharedScopeRef, "tailed entry %d must not keep a scope reference", i)
		}

		// The entries the caller handed us, and therefore the ones the chunk aliases, are
		// untouched: own metadata only, references intact.
		require.Equal(t, newEntries(), entries)
	})

	t.Run("no tailers and no record leaves the entries untouched", func(t *testing.T) {
		s := newSharedMetadataStream(t, nil)

		entries := newEntries()
		_, err := s.Push(context.Background(), entries, sets, nil, 0, true, false, nil, "otlp")
		require.NoError(t, err)

		require.Equal(t, newEntries(), entries)

		// And the chunk still materializes the merged view on read.
		read := readStreamEntries(t, s)
		want := expandedEntries(newEntries(), sets)
		require.Len(t, read, len(want))
		for i, e := range read {
			require.Equal(t, readSM(want[i].StructuredMetadata), e.StructuredMetadata)
		}
	})
}

// TestStreamPushWithoutSharedStructuredMetadata makes sure the native push path is untouched.
func TestStreamPushWithoutSharedStructuredMetadata(t *testing.T) {
	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "one", StructuredMetadata: sm("trace_id", "abc")},
		{Timestamp: time.Unix(2, 0), Line: "two"},
	}

	for _, sets := range [][]logproto.SharedStructuredMetadataSet{nil, {}} {
		s := newSharedMetadataStream(t, nil)
		record := recordPool.GetRecord()

		_, err := s.Push(context.Background(), entries, sets, record, 0, true, false, nil, "loki")
		require.NoError(t, err)

		read := readStreamEntries(t, s)
		require.Len(t, read, len(entries))
		require.Equal(t, sm("trace_id", "abc"), read[0].StructuredMetadata)
		require.Empty(t, read[1].StructuredMetadata)

		// The record aliases the caller's entries rather than a rebuilt copy.
		require.Len(t, record.RefEntries, 1)
		require.Equal(t, entries, record.RefEntries[0].Entries)
	}
}
