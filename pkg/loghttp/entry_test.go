package loghttp

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/grafana/jsonparser"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
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
			data, err := jsoniter.Marshal(tt.entry)
			require.NoError(t, err)

			// Verify the marshaled data matches our expected format
			require.JSONEq(t, tt.expected, string(data))

			// Unmarshal the JSON back to an entry
			var unmarshaledEntry Entry
			err = jsoniter.Unmarshal(data, &unmarshaledEntry)
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
	data, err := jsoniter.Marshal(original)
	require.NoError(t, err)

	// Verify the string representation contains the correct escape sequences
	jsonStr := string(data)
	// The JSON encoding should have \n (backslash + n) for newlines
	// but not double-escaped (\\n)
	require.Contains(t, jsonStr, `"message":"a\nb\nc"`)
	require.NotContains(t, jsonStr, `"message":"a\\nb\\nc"`)

	// Now unmarshal back to an Entry
	var decoded Entry
	err = jsoniter.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify the structured metadata value still has actual newlines
	require.Equal(t, "a\nb\nc", decoded.StructuredMetadata.Get("message"))
}

func TestEntryJsoniterEncoding(t *testing.T) {
	// This test specifically checks if newlines in structured metadata are properly encoded using jsoniter
	entry := Entry{
		Timestamp:          time.Unix(0, 123456789012345),
		Line:               "test line",
		StructuredMetadata: labels.FromStrings("message", "a\nb\nc"),
	}

	// Use the custom jsoniter instance that has our extension registered
	// This instance uses our custom encoder/decoder
	json := jsoniter.ConfigCompatibleWithStandardLibrary

	// Marshal the entry using jsoniter
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

func TestEntryEncoderSpecifically(t *testing.T) {
	// Test the specific EntryEncoder.Encode method directly
	entry := Entry{
		Timestamp:          time.Unix(0, 123456789012345),
		Line:               "test line",
		StructuredMetadata: labels.FromStrings("message", "a\nb\nc"),
	}

	// Create a jsoniter stream
	stream := jsoniter.NewStream(jsoniter.ConfigDefault, nil, 256)

	// Create an EntryEncoder
	encoder := EntryEncoder{}

	// Encode the entry to the stream
	encoder.Encode(unsafePtr(&entry), stream)

	// Get the bytes from the stream
	data := stream.Buffer()

	// Convert to string for examination
	jsonStr := string(data)
	t.Logf("Directly encoded JSON: %s", jsonStr)

	// Check if newlines are properly escaped (should be \n, not \\n)
	require.Contains(t, jsonStr, `"message":"a\n`)
	require.NotContains(t, jsonStr, `"message":"a\\n`)
}

// Helper to get unsafe pointer to entry
func unsafePtr(e *Entry) unsafe.Pointer {
	return unsafe.Pointer(e)
}

func TestUnmarshalEscapingIssue(t *testing.T) {
	// Test the unmarshaling of JSON that contains newlines in structured metadata
	// This emulates the real issue where a client sends JSON with newlines
	// and it gets double-escaped in the response

	// This is what the client would send to /loki/api/v1/push with JSON encoding
	inputJSON := `["123456789012345","test line",{"message":"a\nb\nc"}]`

	// Unmarshal this to an Entry
	var entry Entry
	err := jsoniter.Unmarshal([]byte(inputJSON), &entry)
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
	marshaled, err := jsoniter.Marshal(entry)
	require.NoError(t, err)

	// Print the marshaled JSON for inspection
	t.Logf("Re-marshaled JSON: %s", string(marshaled))

	// Now unmarshal the JSON again
	var reUnmarshaled Entry
	err = jsoniter.Unmarshal(marshaled, &reUnmarshaled)
	require.NoError(t, err)

	// Check the value after double marshaling/unmarshaling
	t.Logf("Value after second unmarshal: %#v", reUnmarshaled.StructuredMetadata.Get("message"))
}

func TestParseStringWithNewlines(t *testing.T) {
	// Test how jsonparser.ParseString handles newlines in JSON strings
	// This should help identify exactly where the issue is occurring

	// Original JSON with a newline - similar to what we'd receive in a push request
	json := `{"message":"a\nb\nc"}`

	// Parse the JSON using jsonparser
	value, dataType, _, err := jsonparser.Get([]byte(json), "message")
	require.NoError(t, err)
	require.Equal(t, jsonparser.String, dataType)

	// Now parse the string value
	parsedStr, err := jsonparser.ParseString(value)
	require.NoError(t, err)

	// Log the value to see what it actually contains
	t.Logf("Parsed string via jsonparser: %#v", parsedStr)

	// Check the raw bytes to really see what's going on
	bytes := []byte(parsedStr)
	t.Logf("Raw bytes: %v", bytes)

	// Verify the correct parsing of the newline
	require.Equal(t, "a\nb\nc", parsedStr, "Should properly parse newlines")
}

func TestObjectEachHandlingOfNewlines(t *testing.T) {
	// Test how jsonparser.ObjectEach handles newlines in our specific context
	// This simulates what happens in the Entry.UnmarshalJSON method

	// JSON object similar to what we'd receive in a push request
	jsonObj := `{"message":"a\nb\nc"}`

	// Parse using ObjectEach like we do in the UnmarshalJSON method
	var parsedValue string
	err := jsonparser.ObjectEach([]byte(jsonObj), func(_ []byte, value []byte, dataType jsonparser.ValueType, _ int) error {
		if dataType == jsonparser.String {
			parsedStr, err := jsonparser.ParseString(value)
			if err != nil {
				return err
			}
			parsedValue = parsedStr
		}
		return nil
	})
	require.NoError(t, err)

	// Log the parsed value
	t.Logf("Parsed value via ObjectEach: %#v", parsedValue)

	// Verify proper parsing of newlines
	require.Equal(t, "a\nb\nc", parsedValue)
}

// debugUnescapeJSONString is a custom debug version of the function in the main codebase
func debugUnescapeJSONString(b []byte) string {
	// First log the raw bytes before doing any unescaping
	fmt.Printf("Raw bytes before unescaping: %v\n", b)

	var stackbuf [1024]byte
	bU, err := jsonparser.Unescape(b, stackbuf[:])
	if err != nil {
		return ""
	}

	// Log the unescaped bytes
	fmt.Printf("Bytes after unescaping: %v\n", bU)

	return string(bU)
}

// Write a custom test showing the different ways escaping can happen
func TestDifferentEscapingMethods(t *testing.T) {
	// Original string with actual newlines
	original := "a\nb\nc"

	// Test jsonparser's Unescape
	escapedBytes := []byte(`a\nb\nc`)
	var buf [1024]byte
	unescaped, err := jsonparser.Unescape(escapedBytes, buf[:])
	require.NoError(t, err)
	jsonparserResult := string(unescaped)
	t.Logf("jsonparser.Unescape result: %#v", jsonparserResult)

	// Test standard Go JSON unmarshaling
	var goResult string
	err = jsoniter.Unmarshal([]byte(`"a\nb\nc"`), &goResult)
	require.NoError(t, err)
	t.Logf("go json.Unmarshal result: %#v", goResult)

	// Test the strings.Replace approach
	replaced := strings.Replace(string(escapedBytes), `\n`, "\n", -1)
	t.Logf("strings.Replace result: %#v", replaced)

	// Compare the results
	require.Equal(t, original, jsonparserResult)
	require.Equal(t, original, goResult)
	require.Equal(t, original, replaced)
}

// Create a simple stream with a full entry including structured metadata
func createStream() string {
	// Create a JSON string that simulates the HTTP API interaction
	return `{
		"streams": [
			{
				"stream": {"service_name":"test"},
				"values": [["1723986725000000000", "{}", {"message": "a\nb\nc"}]]
			}
		]
	}`
}

// Test that simulates the full flow from HTTP API to response
func TestFullPushApiFlow(t *testing.T) {
	// 1. Create a test input that simulates pushing to /loki/api/v1/push
	jsonStr := createStream()

	// 2. Parse the raw values first and verify what's getting unmarshaled
	var parsed map[string]interface{}
	err := jsoniter.Unmarshal([]byte(jsonStr), &parsed)
	require.NoError(t, err)

	// 3. Log the parsed values for inspection
	t.Logf("Parsed JSON: %+v", parsed)

	// 4. Manually extract the structured metadata to see what's happening
	streams := parsed["streams"].([]interface{})
	values := streams[0].(map[string]interface{})["values"].([]interface{})
	entry := values[0].([]interface{})
	metadata := entry[2].(map[string]interface{})

	t.Logf("Raw metadata: %+v", metadata)
	require.Equal(t, "a\nb\nc", metadata["message"], "JSON unmarshal should preserve newlines")

	// 5. Now use our PushRequest implementation
	var request PushRequest
	err = jsoniter.Unmarshal([]byte(jsonStr), &request)
	require.NoError(t, err)

	// 6. Extract and verify the structured metadata
	require.NotEmpty(t, request.Streams)
	logEntry := request.Streams[0].Entries[0]
	t.Logf("Entry line: %s", logEntry.Line)
	t.Logf("Entry structured metadata: %v", logEntry.StructuredMetadata)

	// 7. Verify the structured metadata contains actual newlines
	require.Equal(t, "a\nb\nc", logEntry.StructuredMetadata[0].Value,
		"After unmarshaling, structured metadata should contain actual newlines")

	// 8. Marshal it back like the query would do
	marshaledData, err := jsoniter.Marshal(logEntry)
	require.NoError(t, err)
	t.Logf("Marshaled entry: %s", string(marshaledData))

	// 9. Verify no double escaping in the JSON representation
	// We can't easily check the exact JSON string since the order of fields might change
	// and timestamps might be formatted differently, so we'll just check that double
	// escaping doesn't happen
	jsonOutput := string(marshaledData)
	require.NotContains(t, jsonOutput, `\\n`, "JSON should not have double-escaped newlines")

	// 10. Unmarshal again and verify
	var reUnmarshaled logproto.Entry
	err = jsoniter.Unmarshal(marshaledData, &reUnmarshaled)
	require.NoError(t, err)

	// 11. Final verification
	require.Equal(t, "a\nb\nc", reUnmarshaled.StructuredMetadata[0].Value,
		"After round-trip marshaling, structured metadata should still contain actual newlines")
}
