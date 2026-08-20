package kafka

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// nestedStream builds a stream of one resource and one scope with shared attributes.
func nestedStream(entries int, lineLen int) logproto.InternalStreamAdapter {
	scope := logproto.ScopeLogs{
		Attrs: []push.LabelAdapter{{Name: "scope_attr", Value: "value"}},
	}
	for i := range entries {
		scope.Entries = append(scope.Entries, push.Entry{
			Timestamp: time.Unix(0, int64(i+1)),
			Line:      fmt.Sprintf("%0*d", lineLen, i),
		})
	}
	return logproto.InternalStreamAdapter{
		Labels: `{app="a"}`,
		Hash:   1234,
		ResourceLogs: []logproto.ResourceLogs{{
			Attrs:     []push.LabelAdapter{{Name: "host_name", Value: "host-1"}},
			ScopeLogs: []logproto.ScopeLogs{scope},
		}},
	}
}

// decodeInternal reads a record back into the nested form.
func decodeInternal(t *testing.T, value []byte) logproto.InternalStreamAdapter {
	t.Helper()
	var got logproto.InternalStreamAdapter
	require.NoError(t, got.Unmarshal(value))
	return got
}

func TestEncodeInternalFitsInOneRecord(t *testing.T) {
	records, err := EncodeInternal(0, "tenant", nestedStream(10, 20), 1<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)

	got := decodeInternal(t, records[0].Value)
	require.Equal(t, `{app="a"}`, got.Labels)
	require.Equal(t, uint64(1234), got.Hash)
	require.Equal(t, 10, got.EntryCount())
}

func TestEncodeInternalSplitsAndKeepsEveryEntryInOrder(t *testing.T) {
	const entries = 200
	const maxSize = 4096

	records, err := EncodeInternal(0, "tenant", nestedStream(entries, 100), maxSize)
	require.NoError(t, err)
	require.Greater(t, len(records), 1, "this stream is meant to need several records")

	var lines []string
	for _, rec := range records {
		require.LessOrEqual(t, len(rec.Value), maxSize, "a record exceeded the size limit")

		got := decodeInternal(t, rec.Value)
		require.NotZero(t, got.EntryCount(), "an empty record must never be emitted")

		// Every part must still say which resource and scope its entries belong to,
		// otherwise the attributes are lost for those lines.
		require.Len(t, got.ResourceLogs, 1)
		require.Equal(t, []push.LabelAdapter{{Name: "host_name", Value: "host-1"}}, got.ResourceLogs[0].Attrs)
		require.Equal(t, []push.LabelAdapter{{Name: "scope_attr", Value: "value"}}, got.ResourceLogs[0].ScopeLogs[0].Attrs)

		for _, e := range got.ResourceLogs[0].ScopeLogs[0].Entries {
			lines = append(lines, e.Line)
		}
	}

	require.Len(t, lines, entries, "every entry must survive the split exactly once")
	for i := range lines {
		require.Equal(t, fmt.Sprintf("%0*d", 100, i), lines[i], "entries must stay in order")
	}
}

func TestEncodeInternalKeepsSeparateGroupsSeparate(t *testing.T) {
	// Two resources, enough entries to force a split. No part may merge them, or lines
	// would inherit the wrong host.
	var s logproto.InternalStreamAdapter
	s.Labels = `{app="a"}`
	for _, host := range []string{"host-1", "host-2"} {
		scope := logproto.ScopeLogs{}
		for i := range 100 {
			scope.Entries = append(scope.Entries, push.Entry{
				Timestamp: time.Unix(0, int64(i+1)),
				Line:      host + "-" + fmt.Sprintf("%080d", i),
			})
		}
		s.ResourceLogs = append(s.ResourceLogs, logproto.ResourceLogs{
			Attrs:     []push.LabelAdapter{{Name: "host_name", Value: host}},
			ScopeLogs: []logproto.ScopeLogs{scope},
		})
	}

	records, err := EncodeInternal(0, "tenant", s, 4096)
	require.NoError(t, err)
	require.Greater(t, len(records), 1)

	total := 0
	for _, rec := range records {
		got := decodeInternal(t, rec.Value)
		for i := range got.ResourceLogs {
			host := got.ResourceLogs[i].Attrs[0].Value
			for j := range got.ResourceLogs[i].ScopeLogs {
				for _, e := range got.ResourceLogs[i].ScopeLogs[j].Entries {
					require.True(t, strings.HasPrefix(e.Line, host),
						"line %q was filed under %s", e.Line, host)
					total++
				}
			}
		}
	}
	require.Equal(t, 200, total)
}

func TestEncodeInternalRejectsAnEntryLargerThanTheLimit(t *testing.T) {
	s := logproto.InternalStreamAdapter{
		Labels: `{app="a"}`,
		ResourceLogs: []logproto.ResourceLogs{{
			ScopeLogs: []logproto.ScopeLogs{{
				Entries: []push.Entry{{Timestamp: time.Unix(0, 1), Line: strings.Repeat("x", 5000)}},
			}},
		}},
	}

	_, err := EncodeInternal(0, "tenant", s, 1000)
	require.ErrorContains(t, err, "exceeds maximum allowed size")
}

func TestEncodeInternalRejectsAnOversizedEntryFoundPartWayThrough(t *testing.T) {
	s := nestedStream(4, 10)
	s.ResourceLogs[0].ScopeLogs[0].Entries = append(s.ResourceLogs[0].ScopeLogs[0].Entries,
		push.Entry{Timestamp: time.Unix(0, 99), Line: strings.Repeat("b", 5000)})

	_, err := EncodeInternal(0, "tenant", s, 1000)
	require.ErrorContains(t, err, "exceeds maximum allowed size")
}

func TestEncodeInternalIsSmallerOnTheWireThanTheFlatForm(t *testing.T) {
	// The point of the nesting: with attributes shared by many entries, storing them once
	// beats storing them per entry.
	s := nestedStream(100, 20)
	s.ResourceLogs[0].Attrs = []push.LabelAdapter{
		{Name: "host_name", Value: "host-1"},
		{Name: "cluster", Value: "prod-eu-west-2"},
		{Name: "namespace", Value: "loki-prod-029"},
	}

	nested, err := EncodeInternal(0, "tenant", s, 1<<20)
	require.NoError(t, err)
	require.Len(t, nested, 1)

	flat, err := Encode(0, "tenant", s.ToStream(), 1<<20)
	require.NoError(t, err)
	require.Len(t, flat, 1)

	require.Less(t, len(nested[0].Value), len(flat[0].Value))
	t.Logf("nested %d bytes, flat %d bytes (%.1f%% smaller)",
		len(nested[0].Value), len(flat[0].Value),
		100*(1-float64(len(nested[0].Value))/float64(len(flat[0].Value))))
}
