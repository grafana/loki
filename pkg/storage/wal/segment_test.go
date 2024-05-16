package wal

import (
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/stretchr/testify/require"
)

func TestWalSegmentWriter_Append(t *testing.T) {
	type batch struct {
		tenant  string
		labels  string
		entries []*logproto.Entry
	}
	// Test cases
	tests := []struct {
		name     string
		batches  [][]batch
		expected []batch
	}{
		{
			name: "add two streams",
			batches: [][]batch{
				{
					{
						labels: "foo",
						tenant: "tenant1",
						entries: []*logproto.Entry{
							{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
							{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						},
					},
					{
						labels: "bar",
						tenant: "tenant1",

						entries: []*logproto.Entry{
							{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
							{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						},
					},
				},
				{
					{
						labels: "foo",
						tenant: "tenant1",

						entries: []*logproto.Entry{
							{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
							{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						},
					},
					{
						labels: "bar",
						tenant: "tenant1",

						entries: []*logproto.Entry{
							{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
						},
					},
				},
			},
			expected: []batch{
				{
					labels: "foo",
					tenant: "tenant1",
					entries: []*logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
						{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
						{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
					},
				},
				{
					labels: "bar",
					tenant: "tenant1",
					entries: []*logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
						{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
						{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
					},
				},
			},
		},
	}

	// Run the test cases
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a new WalSegmentWriter
			w := NewWalSegmentWriter()
			// Append the entries
			for _, batch := range tt.batches {
				for _, stream := range batch {
					labels, err := syntax.ParseLabels(stream.labels)
					require.NoError(t, err)
					w.Append(stream.tenant, stream.labels, labels, stream.entries)
				}
			}
			require.NotEmpty(t, tt.expected, "expected entries are empty")
			// Check the entries
			for _, expected := range tt.expected {
				stream, ok := w.streams.Get(streamID{labels: expected.labels, tenant: expected.tenant})
				require.True(t, ok)
				labels, err := syntax.ParseLabels(expected.labels)
				require.NoError(t, err)
				require.Equal(t, labels, stream.lbls)
				require.Equal(t, expected.entries, stream.entries)
			}
		})
	}
}
