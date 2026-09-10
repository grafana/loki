package kafka

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

const testTenant = "tenant123"

func internalEntry(line string, meta ...string) push.Entry {
	e := push.Entry{Timestamp: time.Unix(0, 1).UTC(), Line: line}
	for i := 0; i+1 < len(meta); i += 2 {
		e.StructuredMetadata = append(e.StructuredMetadata, push.LabelAdapter{Name: meta[i], Value: meta[i+1]})
	}
	return e
}

func internalAttrs(pairs ...string) []push.LabelAdapter {
	var out []push.LabelAdapter
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, push.LabelAdapter{Name: pairs[i], Value: pairs[i+1]})
	}
	return out
}

// decodeInternal reads a record back as a nested stream.
func decodeInternal(t *testing.T, value []byte) logproto.InternalStreamAdapter {
	t.Helper()
	var stream logproto.InternalStreamAdapter
	require.NoError(t, stream.Unmarshal(value))
	return stream
}

// shape is the entries of a nested stream, one slice of lines per scope, so a test can say which
// resource and scope each entry came back under.
func shape(stream logproto.InternalStreamAdapter) [][]string {
	var out [][]string
	for i := range stream.ResourceLogs {
		for j := range stream.ResourceLogs[i].ScopeLogs {
			var lines []string
			for _, e := range stream.ResourceLogs[i].ScopeLogs[j].Entries {
				lines = append(lines, e.Line)
			}
			out = append(out, lines)
		}
	}
	return out
}

func nestedStream() logproto.InternalStreamAdapter {
	return logproto.InternalStreamAdapter{
		Labels: `{app="a"}`,
		Hash:   7,
		ResourceLogs: []logproto.ResourceLogs{
			{
				Attrs: internalAttrs("service", "checkout"),
				ScopeLogs: []logproto.ScopeLogs{
					{Attrs: internalAttrs("scope", "one"), Entries: []push.Entry{
						internalEntry("a1"), internalEntry("a2"),
					}},
					{Attrs: internalAttrs("scope", "two"), Entries: []push.Entry{
						internalEntry("b1"),
					}},
				},
			},
			{
				Attrs: internalAttrs("service", "billing"),
				ScopeLogs: []logproto.ScopeLogs{
					{Entries: []push.Entry{internalEntry("c1")}},
				},
			},
		},
	}
}

// requireRecordsCarryTheStream asserts what has to hold for any stream and any limit: the records
// together hold every entry exactly once and in order, each entry unchanged and under a resource
// and scope carrying the attributes it arrived with, and no record, resource or scope is empty or
// over the limit.
//
// Filing an entry under the wrong resource produces data that reads as valid, so this is checked
// for every case rather than for one.
func requireRecordsCarryTheStream(t *testing.T, source logproto.InternalStreamAdapter, records []*kgo.Record, maxSize int) {
	t.Helper()
	require.NotEmpty(t, records, "no records were produced")

	// What the source holds, in order. Position is the identity: keying by line would let two
	// entries carrying the same line stand in for each other, and repeated lines are ordinary.
	type placed struct {
		entry         push.Entry
		resourceAttrs []push.LabelAdapter
		scopeAttrs    []push.LabelAdapter
		resourceIdx   int
		scopeIdx      int
	}
	var want []placed
	for i := range source.ResourceLogs {
		res := &source.ResourceLogs[i]
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]
			for _, entry := range scope.Entries {
				want = append(want, placed{entry, res.Attrs, scope.Attrs, i, j})
			}
		}
	}

	seen := 0
	for r, record := range records {
		require.LessOrEqual(t, len(record.Value), maxSize, "record %d is over the limit", r)
		require.Equal(t, testTenant, string(record.Key), "record %d key", r)

		decoded := decodeInternal(t, record.Value)
		require.Equal(t, source.Labels, decoded.Labels, "record %d labels", r)
		require.Equal(t, source.Hash, decoded.Hash, "record %d hash", r)
		require.NotEmpty(t, decoded.ResourceLogs, "record %d holds no resources", r)

		carriedResources := map[int]bool{}
		carriedScopes := map[[2]int]bool{}
		openedScopes := 0

		for i := range decoded.ResourceLogs {
			res := &decoded.ResourceLogs[i]
			require.NotEmpty(t, res.ScopeLogs, "record %d holds a resource with no scopes", r)

			for j := range res.ScopeLogs {
				scope := &res.ScopeLogs[j]
				require.NotEmpty(t, scope.Entries, "record %d holds a scope with no entries", r)
				openedScopes++

				for _, entry := range scope.Entries {
					require.Less(t, seen, len(want),
						"record %d carries more entries than the source held", r)
					w := want[seen]
					seen++

					require.Equal(t, w.entry, entry,
						"record %d: entry %d of the source came back changed", r, seen-1)
					require.Equal(t, w.resourceAttrs, res.Attrs,
						"record %d: entry %d is filed under the wrong resource", r, seen-1)
					require.Equal(t, w.scopeAttrs, scope.Attrs,
						"record %d: entry %d is filed under the wrong scope", r, seen-1)

					carriedResources[w.resourceIdx] = true
					carriedScopes[[2]int{w.resourceIdx, w.scopeIdx}] = true
				}
			}
		}

		// One resource per source resource carried, and one scope per source scope, so entries
		// that shared either share it here rather than repeating its attributes.
		require.Len(t, decoded.ResourceLogs, len(carriedResources),
			"record %d carries %d resources of the source", r, len(carriedResources))
		require.Equal(t, len(carriedScopes), openedScopes,
			"record %d carries %d scopes of the source", r, len(carriedScopes))
	}
	require.Equal(t, len(want), seen, "every entry exactly once, in the order given")
}

func splittableStream(entriesPerScope int) logproto.InternalStreamAdapter {
	stream := logproto.InternalStreamAdapter{Labels: `{app="a"}`, Hash: 7}
	for resource := 0; resource < 2; resource++ {
		res := logproto.ResourceLogs{Attrs: internalAttrs("service", fmt.Sprintf("svc-%d", resource))}
		for scope := 0; scope < 2; scope++ {
			sl := logproto.ScopeLogs{Attrs: internalAttrs("scope", fmt.Sprintf("s-%d", scope))}
			for i := 0; i < entriesPerScope; i++ {
				sl.Entries = append(sl.Entries, internalEntry(
					fmt.Sprintf("r%d-s%d-%04d-%s", resource, scope, i, strings.Repeat("x", 100))))
			}
			res.ScopeLogs = append(res.ScopeLogs, sl)
		}
		stream.ResourceLogs = append(stream.ResourceLogs, res)
	}
	return stream
}

// resourcePerEntryStream is several resources of one scope and two entries each, so a split has to
// open a new resource in the record rather than adding a scope to the one it is filling.
func resourcePerEntryStream(resources int) logproto.InternalStreamAdapter {
	stream := logproto.InternalStreamAdapter{Labels: `{app="a"}`}
	for r := 0; r < resources; r++ {
		stream.ResourceLogs = append(stream.ResourceLogs, logproto.ResourceLogs{
			Attrs: internalAttrs("service", fmt.Sprintf("svc-%d", r)),
			ScopeLogs: []logproto.ScopeLogs{{
				Attrs: internalAttrs("scope", fmt.Sprintf("scope-%d", r)),
				Entries: []push.Entry{
					internalEntry(fmt.Sprintf("r%d-a-%s", r, strings.Repeat("x", 40))),
					internalEntry(fmt.Sprintf("r%d-b-%s", r, strings.Repeat("x", 40))),
				},
			}},
		})
	}
	return stream
}

// oneScopeStream is one resource of one scope holding n identical entries, so the size of a
// record holding a given number of them can be had by encoding just that many.
func oneScopeStream(entries int) logproto.InternalStreamAdapter {
	scope := logproto.ScopeLogs{Attrs: internalAttrs("scope", "one")}
	for i := 0; i < entries; i++ {
		scope.Entries = append(scope.Entries, internalEntry(strings.Repeat("x", 100)))
	}
	return logproto.InternalStreamAdapter{
		Labels: `{app="a"}`,
		Hash:   7,
		ResourceLogs: []logproto.ResourceLogs{{
			Attrs:     internalAttrs("service", "checkout"),
			ScopeLogs: []logproto.ScopeLogs{scope},
		}},
	}
}
func TestEncodeInternal(t *testing.T) {
	entrylessResourceAndScope := logproto.InternalStreamAdapter{Labels: `{app="a"}`, ResourceLogs: []logproto.ResourceLogs{
		{Attrs: internalAttrs("service", "checkout"), ScopeLogs: []logproto.ScopeLogs{
			{Attrs: internalAttrs("scope", "empty")},
			{Attrs: internalAttrs("scope", "full"), Entries: []push.Entry{internalEntry("a1")}},
		}},
		{Attrs: internalAttrs("service", "idle")},
	}}

	entrylessScopeOnly := logproto.InternalStreamAdapter{Labels: `{app="a"}`, ResourceLogs: []logproto.ResourceLogs{
		{Attrs: internalAttrs("service", "checkout"), ScopeLogs: []logproto.ScopeLogs{
			{Attrs: internalAttrs("scope", "empty")},
			{Attrs: internalAttrs("scope", "full"), Entries: []push.Entry{internalEntry("a1")}},
		}},
	}}
	scopelessResourceOnly := logproto.InternalStreamAdapter{Labels: `{app="a"}`, ResourceLogs: []logproto.ResourceLogs{
		{Attrs: internalAttrs("service", "checkout"), ScopeLogs: []logproto.ScopeLogs{
			{Attrs: internalAttrs("scope", "full"), Entries: []push.Entry{internalEntry("a1")}},
		}},
		{Attrs: internalAttrs("service", "idle")},
	}}

	nested := nestedStream()

	for _, tc := range []struct {
		name    string
		stream  logproto.InternalStreamAdapter
		maxSize int

		wantRecords  int        // exact record count, when the case is about that
		minRecords   int        // lower bound, for cases that only need a split to happen
		wantShape    [][]string // entries per scope of the single record, when the case is about that
		wantPacked   bool       // every record but the last close to full
		wantVerbatim bool       // the one record is the stream exactly as given
	}{
		{
			name:         "a stream that fits stays in one record",
			stream:       nestedStream(),
			maxSize:      1 << 20,
			wantRecords:  1,
			wantVerbatim: true,
		},
		{
			name:       "entries are split across records and packed",
			stream:     splittableStream(200),
			maxSize:    4096,
			minRecords: 2,
			wantPacked: true,
		},
		{
			name:       "each source resource opens a resource of its own",
			stream:     resourcePerEntryStream(4),
			maxSize:    300,
			minRecords: 2,
		},
		{
			name:      "a scope or resource with no entries is left out when the stream is split",
			stream:    entrylessResourceAndScope,
			maxSize:   entrylessResourceAndScope.Size() - 1,
			wantShape: [][]string{{"a1"}},
		},
		{
			// The same, for a stream that fits. Writing it verbatim would keep the empty groups,
			// so the limit would decide the structure.
			name:      "a scope or resource with no entries is left out when the stream fits",
			stream:    entrylessResourceAndScope,
			maxSize:   entrylessResourceAndScope.Size(),
			wantShape: [][]string{{"a1"}},
		},
		{
			name:      "a scope with no entries is left out when the stream fits",
			stream:    entrylessScopeOnly,
			maxSize:   entrylessScopeOnly.Size(),
			wantShape: [][]string{{"a1"}},
		},
		{
			name:      "a resource with no scopes is left out when the stream fits",
			stream:    scopelessResourceOnly,
			maxSize:   scopelessResourceOnly.Size(),
			wantShape: [][]string{{"a1"}},
		},
		{
			name:         "a stream whose size is exactly the limit stays in one record",
			stream:       nested,
			maxSize:      nested.Size(),
			wantRecords:  1,
			wantVerbatim: true,
		},
		{
			name:       "a stream of one entry per scope still splits cleanly",
			stream:     splittableStream(1),
			maxSize:    200,
			minRecords: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records, err := EncodeInternal(0, testTenant, tc.stream, tc.maxSize)
			require.NoError(t, err)

			requireRecordsCarryTheStream(t, tc.stream, records, tc.maxSize)

			if tc.wantRecords > 0 {
				require.Len(t, records, tc.wantRecords)
			}
			if tc.minRecords > 0 {
				require.GreaterOrEqual(t, len(records), tc.minRecords, "this stream is meant to split")
			}
			if tc.wantVerbatim {
				require.Equal(t, tc.stream, decodeInternal(t, records[0].Value))
			}
			if tc.wantShape != nil {
				require.Len(t, records, 1)
				require.Equal(t, tc.wantShape, shape(decodeInternal(t, records[0].Value)))
			}
			if tc.wantPacked {
				for i, record := range records[:len(records)-1] {
					require.Greater(t, len(record.Value), tc.maxSize-400,
						"record %d is only %d bytes, far below the %d limit", i, len(record.Value), tc.maxSize)
				}
			}
		})
	}
}

func TestEncodeInternalRejectsAnEntryLargerThanTheLimit(t *testing.T) {
	const maxSize = 1000

	for _, tc := range []struct {
		name    string
		labels  string
		entries []push.Entry
	}{
		{
			// The entry is well under the limit on its own; the record it needs is not, once the
			// labels every record carries are counted.
			name:    "an entry that fits until the labels it shares a record with are counted",
			labels:  `{app="` + strings.Repeat("a", 900) + `"}`,
			entries: []push.Entry{internalEntry(strings.Repeat("b", 101))},
		},
		{
			name:    "the first entry",
			entries: []push.Entry{internalEntry(strings.Repeat("b", 5000))},
		},
		{
			name: "an entry found part way through",
			entries: []push.Entry{
				internalEntry(strings.Repeat("a", 100)),
				internalEntry(strings.Repeat("b", 5000)),
			},
		},
		{
			name: "the first entry, with entries after it that would have fit",
			entries: []push.Entry{
				internalEntry(strings.Repeat("b", 5000)),
				internalEntry(strings.Repeat("a", 100)),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			labels := tc.labels
			if labels == "" {
				labels = `{app="a"}`
			}
			stream := logproto.InternalStreamAdapter{Labels: labels, ResourceLogs: []logproto.ResourceLogs{
				{ScopeLogs: []logproto.ScopeLogs{{Entries: tc.entries}}},
			}}

			records, err := EncodeInternal(0, testTenant, stream, maxSize)
			require.ErrorContains(t, err, "exceeds maximum allowed size")
			require.Nil(t, records, "an error must not come back with records alongside it")
		})
	}
}

func TestEncodeInternalFillsARecordToExactlyTheLimit(t *testing.T) {
	// The size of a record holding three of these entries, so the limit is one a record lands on
	// exactly rather than near. A record that reaches the limit has to still be allowed.
	measured, err := EncodeInternal(0, testTenant, oneScopeStream(3), 1<<20)
	require.NoError(t, err)
	require.Len(t, measured, 1)
	limit := len(measured[0].Value)

	source := oneScopeStream(7)
	records, err := EncodeInternal(0, testTenant, source, limit)
	require.NoError(t, err)
	requireRecordsCarryTheStream(t, source, records, limit)

	require.Equal(t, limit, len(records[0].Value),
		"a record that reaches the limit exactly should still be filled to it")
	// seven entries split with upto 3 per record should result in 3 records
	require.Len(t, records, 3)
}

func TestEncodeInternalRecordMetadataComesFromTheArguments(t *testing.T) {
	const (
		topic     = "loki-writes"
		partition = int32(3)
		tenant    = "tenant-99"
	)

	for _, tc := range []struct {
		name    string
		stream  logproto.InternalStreamAdapter
		maxSize int
		want    int
	}{
		{name: "one record", stream: nestedStream(), maxSize: 1 << 20, want: 1},
		{name: "several records", stream: splittableStream(20), maxSize: 2048, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records, err := EncodeInternalWithTopic(topic, partition, tenant, tc.stream, tc.maxSize)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(records), tc.want)

			for i, record := range records {
				require.Equal(t, topic, record.Topic, "record %d", i)
				require.Equal(t, partition, record.Partition, "record %d", i)
				require.Equal(t, tenant, string(record.Key), "record %d", i)
			}
		})
	}
}
