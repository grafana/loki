package loghttp

import (
	"encoding/json/v2"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestEntryMarshalUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		entry    Entry
		expected string
	}{
		{
			name: "simple entry",
			entry: Entry{
				Timestamp: time.Unix(0, 123456789012345),
				Line:      "test line",
			},
			expected: `["123456789012345","test line"]`,
		},
		{
			name: "entry with structured metadata",
			entry: Entry{
				Timestamp:          time.Unix(0, 123456789012345),
				Line:               "test line",
				StructuredMetadata: labels.FromStrings("foo", "bar", "count", "42"),
			},
			expected: `["123456789012345","test line",{"count":"42","foo":"bar"}]`,
		},
		{
			name: "entry with newlines in structured metadata",
			entry: Entry{
				Timestamp:          time.Unix(0, 123456789012345),
				Line:               "test line",
				StructuredMetadata: labels.FromStrings("message", "a\nb\nc"),
			},
			expected: `["123456789012345","test line",{"message":"a\nb\nc"}]`,
		},
		{
			name: "entry with quotes in structured metadata",
			entry: Entry{
				Timestamp:          time.Unix(0, 123456789012345),
				Line:               "test line",
				StructuredMetadata: labels.FromStrings("message", `"test"`),
			},
			expected: `["123456789012345","test line",{"message":"\"test\""}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the entry to JSON
			data, err := json.Marshal(tt.entry)
			require.NoError(t, err)

			// Verify the marshaled data matches our expected format
			require.JSONEq(t, tt.expected, string(data))

			// Unmarshal the JSON back to an entry
			var unmarshaledEntry Entry
			err = json.Unmarshal(data, &unmarshaledEntry)
			require.NoError(t, err)

			// Verify the values match
			require.Equal(t, tt.entry.Timestamp, unmarshaledEntry.Timestamp)
			require.Equal(t, tt.entry.Line, unmarshaledEntry.Line)
			require.Equal(t, tt.entry.StructuredMetadata, unmarshaledEntry.StructuredMetadata)
		})
	}
}

func TestEntryRoundTripWithNewlines(t *testing.T) {
	// This test specifically checks if newlines in structured metadata are preserved correctly
	original := Entry{
		Timestamp:          time.Unix(0, 123456789012345),
		Line:               "test line",
		StructuredMetadata: labels.FromStrings("message", "a\nb\nc"),
	}

	// First marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Verify the string representation contains the correct escape sequences
	jsonStr := string(data)
	// The JSON encoding should have \n (backslash + n) for newlines
	// but not double-escaped (\\n)
	require.Contains(t, jsonStr, `"message":"a\nb\nc"`)
	require.NotContains(t, jsonStr, `"message":"a\\nb\\nc"`)

	// Now unmarshal back to an Entry
	var decoded Entry
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify the structured metadata value still has actual newlines
	require.Equal(t, "a\nb\nc", decoded.StructuredMetadata.Get("message"))
}

func TestEntryJSONEncoding(t *testing.T) {
	// This test specifically checks if newlines in structured metadata are properly encoded
	entry := Entry{
		Timestamp:          time.Unix(0, 123456789012345),
		Line:               "test line",
		StructuredMetadata: labels.FromStrings("message", "a\nb\nc"),
	}

	// Marshal the entry
	data, err := json.Marshal(entry)
	require.NoError(t, err)

	// Print the encoded JSON for debugging
	jsonStr := string(data)
	t.Logf("JSON representation: %s", jsonStr)

	// Now unmarshal back to an Entry
	var decoded Entry
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify the values match, especially the newlines in structured metadata
	require.Equal(t, entry.Timestamp, decoded.Timestamp)
	require.Equal(t, entry.Line, decoded.Line)
	require.Equal(t, "a\nb\nc", decoded.StructuredMetadata.Get("message"))
}

func TestUnmarshalEscapingIssue(t *testing.T) {
	// Test the unmarshaling of JSON that contains newlines in structured metadata
	// This emulates the real issue where a client sends JSON with newlines
	// and it gets double-escaped in the response

	// This is what the client would send to /loki/api/v1/push with JSON encoding
	inputJSON := `["123456789012345","test line",{"message":"a\nb\nc"}]`

	// Unmarshal this to an Entry
	var entry Entry
	err := json.Unmarshal([]byte(inputJSON), &entry)
	require.NoError(t, err)

	// Print the actual value of StructuredMetadata for debugging
	t.Logf("Structured metadata value after unmarshal: %#v", entry.StructuredMetadata.Get("message"))

	// Verify the value still has actual newlines - this is the core issue
	// We expect "a\nb\nc" when printed to log but in memory it should be a real newline
	actualValue := entry.StructuredMetadata.Get("message")
	require.Equal(t, "a\nb\nc", actualValue, "Newlines should be properly preserved")

	// Also directly check the byte representation of the string to see what's happening
	actualBytes := []byte(actualValue)
	t.Logf("String bytes: %v", actualBytes)

	// Now marshal it back and see if it maintains proper escaping
	marshaled, err := json.Marshal(entry)
	require.NoError(t, err)

	// Print the marshaled JSON for inspection
	t.Logf("Re-marshaled JSON: %s", string(marshaled))

	// Now unmarshal the JSON again
	var reUnmarshaled Entry
	err = json.Unmarshal(marshaled, &reUnmarshaled)
	require.NoError(t, err)

	// Check the value after double marshaling/unmarshaling
	t.Logf("Value after second unmarshal: %#v", reUnmarshaled.StructuredMetadata.Get("message"))
}

func benchmarkEntries(n int, withMetadata bool) []Entry {
	entries := make([]Entry, n)
	for i := range entries {
		entries[i] = Entry{
			Timestamp: time.Unix(0, 1723986725000000000+int64(i)),
			Line:      `level=info ts=2024-08-18T13:12:05Z caller=main.go:42 msg="request <done>" path=/api/v1/push status=204`,
		}
		if withMetadata {
			entries[i].StructuredMetadata = labels.FromStrings("trace_id", "0242ac120002", "user_id", "superUser123", "message", "a\nb")
		}
	}
	return entries
}

func BenchmarkEntriesMarshal(b *testing.B) {
	for _, withMetadata := range []bool{false, true} {
		b.Run("metadata="+strconv.FormatBool(withMetadata), func(b *testing.B) {
			entries := benchmarkEntries(1000, withMetadata)
			b.ReportAllocs()
			for b.Loop() {
				_, err := json.Marshal(entries)
				require.NoError(b, err)
			}
		})
	}
}

// BenchmarkStreamUnmarshal decodes entries the way query responses do, via Stream.UnmarshalJSON.
func BenchmarkStreamUnmarshal(b *testing.B) {
	for _, withMetadata := range []bool{false, true} {
		b.Run("metadata="+strconv.FormatBool(withMetadata), func(b *testing.B) {
			values, err := json.Marshal(benchmarkEntries(1000, withMetadata))
			require.NoError(b, err)
			data := []byte(`{"stream":{"app":"foo"},"values":` + string(values) + `}`)
			var s Stream
			b.ReportAllocs()
			for b.Loop() {
				require.NoError(b, s.UnmarshalJSON(data))
			}
		})
	}
}
