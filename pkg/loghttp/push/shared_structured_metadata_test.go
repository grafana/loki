package push

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"

	"github.com/grafana/loki/pkg/push"
)

// TestCalculateStreamsStats_SharedStructuredMetadata asserts that the exported helper accounts for
// the shared structured metadata pool of a stream, which the OTLP push path populates when the
// expansion of resource and scope attributes is deferred. Without it the helper silently
// under-reports the size of such requests.
func TestCalculateStreamsStats_SharedStructuredMetadata(t *testing.T) {
	resource := []push.LabelAdapter{
		{Name: "service_name", Value: "myservice"},
		{Name: "deployment_environment", Value: "prod"},
	}
	scope := []push.LabelAdapter{
		{Name: "scope_name", Value: "mylib"},
	}
	resourceSize := int64(util.StructuredMetadataSize(resource))
	scopeSize := int64(util.StructuredMetadataSize(scope))
	require.NotZero(t, resourceSize)
	require.NotZero(t, scopeSize)

	pool := []logproto.SharedStructuredMetadataSet{{Attrs: resource}, {Attrs: scope}}

	entries := []logproto.Entry{
		{Timestamp: time.Unix(1, 0), Line: "line 1", SharedResourceRef: 1, SharedScopeRef: 2},
		{Timestamp: time.Unix(2, 0), Line: "line 2", SharedResourceRef: 1, SharedScopeRef: 2},
	}

	statsFor := func(t *testing.T, stream logproto.Stream) *Stats {
		t.Helper()

		stats := NewPushStats()
		req := &logproto.PushRequest{Streams: []logproto.Stream{stream}}
		require.NoError(t, CalculateStreamsStats(context.Background(), "fake", req, nil, nil, stats))
		return stats
	}

	withShared := statsFor(t, logproto.Stream{
		Labels:                       `{foo="bar"}`,
		Entries:                      entries,
		SharedStructuredMetadataSets: pool,
	})
	withoutShared := statsFor(t, logproto.Stream{
		Labels:  `{foo="bar"}`,
		Entries: []logproto.Entry{{Timestamp: time.Unix(1, 0), Line: "line 1"}, {Timestamp: time.Unix(2, 0), Line: "line 2"}},
	})

	// Structured metadata bytes are metered unexpanded: each pool set is stored once per stream,
	// so each is counted once no matter how many entries reference it.
	require.Equal(t,
		withoutShared.StructuredMetadataBytes[""][0]+resourceSize+scopeSize,
		withShared.StructuredMetadataBytes[""][0],
	)

	// TotalExpandedEntriesSize reports what the request would weigh with the attributes copied
	// onto every entry, so the sets an entry references are counted once per entry there.
	require.Equal(t,
		withoutShared.TotalExpandedEntriesSize+(resourceSize+scopeSize)*int64(len(entries)),
		withShared.TotalExpandedEntriesSize,
	)

	// Log line bytes are unaffected by structured metadata.
	require.Equal(t, withoutShared.LogLinesBytes[""][0], withShared.LogLinesBytes[""][0])
}

// TestCalculateStreamsStats_PartialSharedRefs covers entries that reference only one of the two
// kinds of set, or none, alongside entries that reference both.
func TestCalculateStreamsStats_PartialSharedRefs(t *testing.T) {
	resource := []push.LabelAdapter{{Name: "service_name", Value: "myservice"}}
	scope := []push.LabelAdapter{{Name: "scope_name", Value: "mylib"}}
	resourceSize := int64(util.StructuredMetadataSize(resource))
	scopeSize := int64(util.StructuredMetadataSize(scope))

	stream := logproto.Stream{
		Labels: `{foo="bar"}`,
		SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{
			{Attrs: resource},
			{Attrs: scope},
		},
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(1, 0), Line: "a", SharedResourceRef: 1, SharedScopeRef: 2},
			{Timestamp: time.Unix(2, 0), Line: "b", SharedResourceRef: 1},
			{Timestamp: time.Unix(3, 0), Line: "c", SharedScopeRef: 2},
			{Timestamp: time.Unix(4, 0), Line: "d"},
		},
	}
	require.NoError(t, stream.ValidateSharedRefs())

	stats := NewPushStats()
	req := &logproto.PushRequest{Streams: []logproto.Stream{stream}}
	require.NoError(t, CalculateStreamsStats(context.Background(), "fake", req, nil, nil, stats))

	lines := int64(4) // one byte each
	expanded := lines + (resourceSize + scopeSize) + resourceSize + scopeSize
	require.Equal(t, expanded, stats.TotalExpandedEntriesSize)

	// The pool is still counted exactly once, whatever the references are.
	require.Equal(t, resourceSize+scopeSize, stats.StructuredMetadataBytes[""][0])
}

// TestParseLokiRequest_StripsSharedStructuredMetadata asserts that the native push path discards a
// shared structured metadata pool and the entry references into it.
//
// The pool is internal to Loki's OTLP ingest pipeline and is only populated for tenants that have
// deferred expansion enabled. A native client that sets the fields itself would otherwise opt into
// deferred semantics whatever its tenant's limit says, and be metered for a pool counted once per
// stream while the read path expands it onto every entry.
func TestParseLokiRequest_StripsSharedStructuredMetadata(t *testing.T) {
	sharedResource := []push.LabelAdapter{{Name: "service_name", Value: "smuggled"}}
	sharedScope := []push.LabelAdapter{{Name: "scope_name", Value: "smuggled"}}

	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
				Entries: []logproto.Entry{
					{
						Timestamp:          time.Unix(0, 1570818238000000000),
						Line:               "a line",
						StructuredMetadata: push.LabelsAdapter{{Name: "own", Value: "kept"}},
						SharedResourceRef:  1,
						SharedScopeRef:     2,
					},
					{
						Timestamp:         time.Unix(0, 1570818239000000000),
						Line:              "another line",
						SharedResourceRef: 1,
					},
				},
				SharedStructuredMetadataSets: []logproto.SharedStructuredMetadataSet{
					{Attrs: sharedResource},
					{Attrs: sharedScope},
				},
			},
		},
	}

	request := httptest.NewRequest("POST", "/loki/api/v1/push", strings.NewReader(snappyString(marshalProto(req))))
	request.Header.Add("Content-Type", "application/x-protobuf")

	limits := &fakeLimits{}
	parsed, stats, err := ParseLokiRequest("fake", request, limits, nil, 100<<20, 100<<20, nil, newMockStreamResolver("fake", limits), log.NewNopLogger())
	require.NoError(t, err)

	require.Len(t, parsed.Streams, 1)
	stream := parsed.Streams[0]
	require.Empty(t, stream.SharedStructuredMetadataSets, "the pool must be discarded on the native push path")
	require.Len(t, stream.Entries, 2)
	for i := range stream.Entries {
		require.Zerof(t, stream.Entries[i].SharedResourceRef, "entry %d kept its resource reference", i)
		require.Zerof(t, stream.Entries[i].SharedScopeRef, "entry %d kept its scope reference", i)
	}

	// Own structured metadata is untouched, and nothing shared is left to meter: only the entry's
	// own pairs are counted, and the expanded size is lines plus own metadata with no shared bytes
	// added per entry.
	require.Equal(t, push.LabelsAdapter{{Name: "own", Value: "kept"}}, stream.Entries[0].StructuredMetadata)

	var structuredMetadataBytes int64
	for _, byRetention := range stats.StructuredMetadataBytes {
		for _, v := range byRetention {
			structuredMetadataBytes += v
		}
	}
	require.Equal(t, int64(len("ownkept")), structuredMetadataBytes)
	require.Equal(t, int64(len("a line")+len("another line")+len("ownkept")), stats.TotalExpandedEntriesSize)
}

// TestOTLPStreamRefDedupesByContent covers the normal path of the pool index: identical sets are
// pooled once, different sets get different references, and an empty set is never pooled.
func TestOTLPStreamRefDedupesByContent(t *testing.T) {
	s := &otlpStream{}

	resource := push.LabelsAdapter{{Name: "service_name", Value: "svc"}}
	scope := push.LabelsAdapter{{Name: "scope_name", Value: "lib"}}
	resourceHash := util.StructuredMetadataHash(resource)
	scopeHash := util.StructuredMetadataHash(scope)

	require.Zero(t, s.ref(nil, util.StructuredMetadataHash(nil)), "an empty set is not pooled")
	require.Empty(t, s.stream.SharedStructuredMetadataSets)

	require.Equal(t, uint32(1), s.ref(resource, resourceHash))
	require.Equal(t, uint32(1), s.ref(resource, resourceHash), "the same set must dedupe to one pool entry")
	require.Equal(t, uint32(2), s.ref(scope, scopeHash))
	require.Equal(t, uint32(1), s.ref(resource, resourceHash))
	require.Len(t, s.stream.SharedStructuredMetadataSets, 2)

	// A byte-identical copy under the same hash still resolves to the existing set.
	copyOfResource := push.LabelsAdapter{{Name: "service_name", Value: "svc"}}
	require.Equal(t, uint32(1), s.ref(copyOfResource, resourceHash))
	require.Len(t, s.stream.SharedStructuredMetadataSets, 2)
}

// TestOTLPStreamRefSurvivesHashCollision covers the collision path of the pool index.
//
// xxhash collisions cannot be produced on demand, so the collision is injected: two different sets
// are indexed under one hash. The index has to keep a candidate list per hash, otherwise the
// colliding set is appended but never indexed and every later entry carrying it appends another
// copy, turning the pool into one set per entry.
func TestOTLPStreamRefSurvivesHashCollision(t *testing.T) {
	const collidingHash = uint64(42)

	first := push.LabelsAdapter{{Name: "service_name", Value: "first"}}
	second := push.LabelsAdapter{{Name: "service_name", Value: "second"}}

	s := &otlpStream{}
	require.Equal(t, uint32(1), s.ref(first, collidingHash))
	// The second set collides: it cannot reuse reference 1, so it is pooled separately.
	require.Equal(t, uint32(2), s.ref(second, collidingHash))
	require.Len(t, s.stream.SharedStructuredMetadataSets, 2)

	// Both must now be found again, and the pool must stop growing however many entries reference
	// either of them.
	for i := 0; i < 10; i++ {
		require.Equal(t, uint32(1), s.ref(first, collidingHash))
		require.Equal(t, uint32(2), s.ref(second, collidingHash))
	}
	require.Len(t, s.stream.SharedStructuredMetadataSets, 2, "a hash collision must not make the pool grow per entry")

	// The references resolve to the right contents.
	require.Equal(t, first, push.LabelsAdapter(s.stream.SharedStructuredMetadataSets[0].Attrs))
	require.Equal(t, second, push.LabelsAdapter(s.stream.SharedStructuredMetadataSets[1].Attrs))
}

// TestLookupSharedRefIgnoresOutOfRangeCandidates guards the candidate scan against a reference that
// does not address the pool it is given.
func TestLookupSharedRefIgnoresOutOfRangeCandidates(t *testing.T) {
	attrs := push.LabelsAdapter{{Name: "service_name", Value: "svc"}}
	sets := []logproto.SharedStructuredMetadataSet{{Attrs: attrs}}

	ref, ok := lookupSharedRef(sets, []uint32{0, 7, 1}, attrs)
	require.True(t, ok)
	require.Equal(t, uint32(1), ref)

	_, ok = lookupSharedRef(sets, []uint32{0, 7}, attrs)
	require.False(t, ok)

	_, ok = lookupSharedRef(sets, nil, attrs)
	require.False(t, ok)
}
