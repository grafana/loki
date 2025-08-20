package executor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogfmtTokenizer(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      []struct {
			key   string
			value string
		}
	}{
		{
			name:          "simple key-value pair",
			input:         "level=error",
			requestedKeys: []string{"level"},
			expected: []struct {
				key   string
				value string
			}{
				{key: "level", value: "error"},
			},
		},
		{
			name:          "multiple pairs",
			input:         "level=error status=500 msg=failed",
			requestedKeys: []string{"level", "status", "msg"},
			expected: []struct {
				key   string
				value string
			}{
				{key: "level", value: "error"},
				{key: "status", value: "500"},
				{key: "msg", value: "failed"},
			},
		},
		{
			name:          "quoted values with spaces",
			input:         `msg="hello world" level=error`,
			requestedKeys: []string{"msg", "level"},
			expected: []struct {
				key   string
				value string
			}{
				{key: "msg", value: "hello world"},
				{key: "level", value: "error"},
			},
		},
		{
			name:          "missing/empty values",
			input:         "level= status=200 empty=",
			requestedKeys: []string{"level", "status", "empty"},
			expected: []struct {
				key   string
				value string
			}{
				{key: "level", value: ""},
				{key: "status", value: "200"},
				{key: "empty", value: ""},
			},
		},
		{
			name:          "duplicate keys without early stop (no requested keys)",
			input:         "level=debug level=error level=info",
			requestedKeys: []string{}, // No requested keys = get all tokens
			expected: []struct {
				key   string
				value string
			}{
				{key: "level", value: "debug"},
				{key: "level", value: "error"},
				{key: "level", value: "info"},
			},
		},
		{
			name:          "duplicate keys with early stop (requested keys)",
			input:         "level=debug level=error level=info",
			requestedKeys: []string{"level"},
			expected: []struct {
				key   string
				value string
			}{
				{key: "level", value: "debug"}, // Stops after finding first occurrence
			},
		},
		{
			name:          "skipped values aren't processed",
			input:         `skip="heavy\n\t\r\\\"\'" level=info msg=simple`,
			requestedKeys: []string{"level", "msg"}, // Not requesting 'skip'
			expected: []struct {
				key   string
				value string
			}{
				{key: "level", value: "info"},
				{key: "msg", value: "simple"},
			},
		},
		{
			name:          "unquoted values preserve backslashes",
			input:         `path=C:\Users\file.txt level=info`,
			requestedKeys: []string{"path", "level"},
			expected: []struct {
				key   string
				value string
			}{
				{key: "path", value: `C:\Users\file.txt`}, // Backslashes preserved in unquoted value
				{key: "level", value: "info"},
			},
		},
		{
			name:          "multiple calls to Value() are idempotent",
			input:         `msg="test\nvalue"`,
			requestedKeys: []string{"msg"},
			expected: []struct {
				key   string
				value string
			}{
				{key: "msg", value: "test\nvalue"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer := NewLogfmtTokenizer(tt.input, tt.requestedKeys)

			var results []struct {
				key   string
				value string
			}

			for tokenizer.Next() {
				key := tokenizer.Key()
				value1 := tokenizer.Value()
				
				// For idempotent test case, verify multiple calls return same value
				if tt.name == "multiple calls to Value() are idempotent" {
					value2 := tokenizer.Value()
					require.Equal(t, value1, value2, "Multiple calls to Value() should return same result")
				}
				
				results = append(results, struct {
					key   string
					value string
				}{key: key, value: value1})
			}

			require.Equal(t, tt.expected, results)
		})
	}
}

// This is a design decsion that differes from the v1 tokenizer - we'll stop as soon as we find all requested keys
// While this feels like the right decision, as it will align with a future state where we pre-parse logftmt and can
// only pull the requested columns, it may cause inconsistencies with v1 query results.
func TestLogfmtTokenizerEarlyStopWithLastWins(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      map[string]string
		description   string
	}{
		{
			name:          "case 1: duplicate before all keys found",
			input:         "level=error level=debug status=done level=warn",
			requestedKeys: []string{"level", "status"},
			expected: map[string]string{
				"level":  "debug",
				"status": "done",
			},
			description: "Should stop after 'status=done', ignoring 'level=warn'",
		},
		{
			name:          "case 2: all keys found early, duplicates ignored",
			input:         "status=500 level=error status=200 level=info",
			requestedKeys: []string{"level", "status"},
			expected: map[string]string{
				"level":  "error",
				"status": "500",
			},
			description: "Should stop after 'level=error', ignoring later duplicates",
		},
		{
			name:          "case 3: mixed duplicates with non-requested keys",
			input:         "a=1 b=2 a=3 c=4 b=5",
			requestedKeys: []string{"a", "b", "c"},
			expected: map[string]string{
				"a": "3",
				"b": "2",
				"c": "4",
			},
			description: "Should stop after 'c=4', ignoring 'b=5'",
		},
		{
			name:          "case 4: non-requested keys don't affect stopping",
			input:         "level=debug msg='hello world' level=error foo=bar status=200 level=warn",
			requestedKeys: []string{"level", "status"},
			expected: map[string]string{
				"level":  "error",
				"status": "200",
			},
			description: "Should skip non-requested keys and stop after finding all requested",
		},
		{
			name:          "case 5: simple case - no duplicates",
			input:         "a=1 b=2 c=3 d=4 e=5 f=6",
			requestedKeys: []string{"a", "b"},
			expected: map[string]string{
				"a": "1",
				"b": "2",
			},
			description: "Should stop immediately after 'b=2'",
		},
		{
			name:          "case 6: all duplicates before finding all keys",
			input:         "level=debug level=info level=error status=200",
			requestedKeys: []string{"level", "status"},
			expected: map[string]string{
				"level":  "error",
				"status": "200",
			},
			description: "All level duplicates processed before status found",
		},
		{
			name:          "case 7: empty values with duplicates",
			input:         "level= level=error status= status=200",
			requestedKeys: []string{"level", "status"},
			expected: map[string]string{
				"level":  "error",
				"status": "",
			},
			description: "Empty value for status should be kept, status=200 never processed",
		},
		{
			name:          "case 8: only some requested keys present",
			input:         "a=1 b=2 c=3",
			requestedKeys: []string{"a", "d"},
			expected: map[string]string{
				"a": "1",
			},
			description: "Should return only found keys, 'd' is missing",
		},
		{
			name:          "case 9: quoted values with early stop",
			input:         `level="error message" status=500 level="warning message"`,
			requestedKeys: []string{"level", "status"},
			expected: map[string]string{
				"level":  "error message",
				"status": "500",
			},
			description: "Should handle quoted values and stop after status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)
			require.NoError(t, err, "Should not return error for valid input")
			require.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// Test that verifies early stopping and filtering actually happens
func TestLogfmtTokenizerCountsTokensProcessed(t *testing.T) {
	// Test that tokenizer only returns requested keys and stops early
	input := "a=1 b=2 c=3 d=4 e=5 f=6 g=7 h=8"
	requestedKeys := []string{"b", "d"}

	// Create a tokenizer that will filter and stop early
	tokenizer := NewLogfmtTokenizer(input, requestedKeys)
	result := make(map[string]string)
	tokenCount := 0

	for tokenizer.Next() {
		tokenCount++
		// The tokenizer only returns requested keys
		key := tokenizer.Key()
		value := tokenizer.Value()
		result[key] = value
	}

	// Should have returned exactly 2 tokens: b=2 and d=4
	// The tokenizer internally skips a, c, and stops after finding d
	require.Equal(t, 2, tokenCount, "Should only return requested keys: b and d")
	require.Equal(t, map[string]string{"b": "2", "d": "4"}, result)
}

func TestTokenizeLogfmtNoRequestedKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "empty requested keys returns all keys",
			input: "level=info status=200 msg=hello user=admin",
			expected: map[string]string{
				"level":  "info",
				"status": "200",
				"msg":    "hello",
				"user":   "admin",
			},
		},
		{
			name:  "nil requested keys returns all keys",
			input: "a=1 b=2 c=3",
			expected: map[string]string{
				"a": "1",
				"b": "2",
				"c": "3",
			},
		},
		{
			name:  "duplicate keys with no filter uses last-wins",
			input: "level=debug level=info level=error",
			expected: map[string]string{
				"level": "error", // Last value wins
			},
		},
		{
			name:  "mixed valid and malformed with no filter",
			input: "good=yes bad==no also_good=maybe",
			expected: map[string]string{
				"good":      "yes",
				"bad":       "", // Malformed but still included
				"also_good": "maybe",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (empty slice)", func(t *testing.T) {
			result, _ := TokenizeLogfmt(tt.input, []string{})
			require.Equal(t, tt.expected, result)
		})

		t.Run(tt.name+" (nil)", func(t *testing.T) {
			result, _ := TokenizeLogfmt(tt.input, nil)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenizerHandlesBareKeys(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      map[string]string
	}{
		{
			name:          "single bare key",
			input:         "error",
			requestedKeys: []string{"error"},
			expected: map[string]string{
				"error": "",
			},
		},
		{
			name:          "bare key followed by regular key",
			input:         "debug level=info",
			requestedKeys: []string{"debug", "level"},
			expected: map[string]string{
				"debug": "",
				"level": "info",
			},
		},
		{
			name:          "regular key followed by bare key",
			input:         "level=info error",
			requestedKeys: []string{"level", "error"},
			expected: map[string]string{
				"level": "info",
				"error": "",
			},
		},
		{
			name:          "multiple bare keys",
			input:         "debug verbose error",
			requestedKeys: []string{"debug", "verbose", "error"},
			expected: map[string]string{
				"debug":   "",
				"verbose": "",
				"error":   "",
			},
		},
		{
			name:          "mixed bare and valued keys",
			input:         "start level=info debug msg=\"test message\" end",
			requestedKeys: []string{"start", "level", "debug", "msg", "end"},
			expected: map[string]string{
				"start": "",
				"level": "info",
				"debug": "",
				"msg":   "test message",
				"end":   "",
			},
		},
		{
			name:          "bare key at end of line",
			input:         "level=info status=200 success",
			requestedKeys: []string{"level", "status", "success"},
			expected: map[string]string{
				"level":   "info",
				"status":  "200",
				"success": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)
			require.NoError(t, err, "Should not return error for bare keys")
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenizerHandlesEscapeSequences(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      map[string]string
	}{
		{
			name:          "newline escape in quoted value",
			input:         `msg="line1\nline2"`,
			requestedKeys: []string{"msg"},
			expected: map[string]string{
				"msg": "line1\nline2",
			},
		},
		{
			name:          "tab escape in quoted value",
			input:         `msg="col1\tcol2"`,
			requestedKeys: []string{"msg"},
			expected: map[string]string{
				"msg": "col1\tcol2",
			},
		},
		{
			name:          "escaped quote in quoted value",
			input:         `msg="He said \"hello\""`,
			requestedKeys: []string{"msg"},
			expected: map[string]string{
				"msg": `He said "hello"`,
			},
		},
		{
			name:          "escaped backslash",
			input:         `path="C:\\Users\\file.txt"`,
			requestedKeys: []string{"path"},
			expected: map[string]string{
				"path": `C:\Users\file.txt`,
			},
		},
		{
			name:          "multiple escape sequences",
			input:         `msg="line1\nline2\ttab\r\nwindows"`,
			requestedKeys: []string{"msg"},
			expected: map[string]string{
				"msg": "line1\nline2\ttab\r\nwindows",
			},
		},
		{
			name:          "carriage return escape",
			input:         `msg="before\rafter"`,
			requestedKeys: []string{"msg"},
			expected: map[string]string{
				"msg": "before\rafter",
			},
		},
		{
			name:          "escaped single quote in single-quoted value",
			input:         `msg='it\'s working'`,
			requestedKeys: []string{"msg"},
			expected: map[string]string{
				"msg": "it's working",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)
			require.NoError(t, err, "Should not return error for escape sequences")
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenizerHandlesInvalidUTF8(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      map[string]string
		wantErr       bool
	}{
		{
			name:          "invalid UTF-8 in unquoted value",
			input:         "key=" + string([]byte{0xff, 0xfe}) + " status=ok",
			requestedKeys: []string{"key", "status"},
			expected: map[string]string{
				"key":    "", // Invalid UTF-8 should be handled gracefully
				"status": "ok",
			},
			wantErr: true, // Should report an error about invalid UTF-8
		},
		{
			name:          "invalid UTF-8 in quoted value",
			input:         `msg="` + string([]byte{0xff, 0xfe}) + `" level=info`,
			requestedKeys: []string{"msg", "level"},
			expected: map[string]string{
				"msg":   "", // Invalid UTF-8 should be handled gracefully
				"level": "info",
			},
			wantErr: true,
		},
		{
			name:          "invalid UTF-8 in key - not requested",
			input:         string([]byte{0xff, 0xfe}) + "=value level=info",
			requestedKeys: []string{"level"},
			expected: map[string]string{
				"level": "info", // Should still parse valid parts
			},
			wantErr: false, // Error for non-requested key should be dropped
		},
		{
			name:          "invalid UTF-8 in key - all keys requested",
			input:         string([]byte{0xff, 0xfe}) + "=value level=info",
			requestedKeys: []string{}, // Empty means get all keys
			expected: map[string]string{
				"level": "info", // Should still parse valid parts
			},
			wantErr: true, // Should report error when getting all keys
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)

			if tt.wantErr {
				require.Error(t, err, "Should return error for invalid UTF-8")
				// v1 format uses "invalid key" for UTF-8 errors in keys, "invalid UTF-8" for values
				require.Contains(t, err.Error(), "invalid", "Error should mention invalid")
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenizerPreservesValidUTF8(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      map[string]string
	}{
		{
			name:          "emoji in value",
			input:         `msg="Hello 👋 World 🌍" level=info`,
			requestedKeys: []string{"msg", "level"},
			expected: map[string]string{
				"msg":   "Hello 👋 World 🌍",
				"level": "info",
			},
		},
		{
			name:          "Chinese characters",
			input:         `msg="你好世界" status=成功`,
			requestedKeys: []string{"msg", "status"},
			expected: map[string]string{
				"msg":    "你好世界",
				"status": "成功",
			},
		},
		{
			name:          "Japanese characters",
			input:         `greeting=こんにちは location=東京`,
			requestedKeys: []string{"greeting", "location"},
			expected: map[string]string{
				"greeting": "こんにちは",
				"location": "東京",
			},
		},
		{
			name:          "Arabic text",
			input:         `msg="مرحبا بالعالم" dir=rtl`,
			requestedKeys: []string{"msg", "dir"},
			expected: map[string]string{
				"msg": "مرحبا بالعالم",
				"dir": "rtl",
			},
		},
		{
			name:          "mixed scripts",
			input:         `text="Hello мир 世界 🌍" valid=true`,
			requestedKeys: []string{"text", "valid"},
			expected: map[string]string{
				"text":  "Hello мир 世界 🌍",
				"valid": "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)
			require.NoError(t, err, "Should not return error for valid UTF-8")
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenizerRejectsQuoteInKey(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      map[string]string
		wantErr       bool
	}{
		{
			name:          "double quote in key - not requested",
			input:         `ke"y=value status=ok`,
			requestedKeys: []string{"status"},
			expected: map[string]string{
				"status": "ok", // Should still parse valid parts
			},
			wantErr: false, // Error for non-requested key should be dropped
		},
		{
			name:          "single quote in key - not requested",
			input:         `ke'y=value level=info`,
			requestedKeys: []string{"level"},
			expected: map[string]string{
				"level": "info",
			},
			wantErr: false, // Error for non-requested key should be dropped
		},
		{
			name:          "quote at start of key - not requested",
			input:         `"key=value msg=hello`,
			requestedKeys: []string{"msg"},
			expected: map[string]string{
				"msg": "hello",
			},
			wantErr: false, // Error for non-requested key should be dropped
		},
		{
			name:          "quote at end of key - not requested",
			input:         `key"=value test=pass`,
			requestedKeys: []string{"test"},
			expected: map[string]string{
				"test": "pass",
			},
			wantErr: false, // Error for non-requested key should be dropped
		},
		{
			name:          "double quote in key - all keys requested",
			input:         `ke"y=value status=ok`,
			requestedKeys: []string{}, // Empty means get all keys
			expected: map[string]string{
				"status": "ok",
			},
			wantErr: true, // Should report error when getting all keys
		},
		{
			name:          "single quote in key - all keys requested",
			input:         `ke'y=value level=info`,
			requestedKeys: nil, // nil also means get all keys
			expected: map[string]string{
				"level": "info",
			},
			wantErr: true, // Should report error when getting all keys
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)

			if tt.wantErr {
				require.Error(t, err, "Should return error for quote in key")
				// v1 format says "unexpected '"'" or "invalid key"
				errStr := err.Error()
				require.True(t, strings.Contains(errStr, "unexpected") || strings.Contains(errStr, "invalid key"),
					"Error should mention unexpected quote or invalid key")
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenizerHandlesUnclosedQuotes(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expected      map[string]string
		errorContains []string
	}{
		{
			name:          "unclosed double quote",
			input:         `msg="hello world level=info`,
			requestedKeys: []string{"msg", "level"},
			expected: map[string]string{
				"msg": "hello world level=info", // Takes rest of line
			},
			errorContains: []string{"unterminated quoted value", "pos 4"},
		},
		{
			name:          "unclosed single quote",
			input:         `path='/usr/bin status=ok`,
			requestedKeys: []string{"path", "status"},
			expected: map[string]string{
				"path": "/usr/bin status=ok",
			},
			errorContains: []string{"unterminated quoted value", "pos 5"},
		},
		{
			name:          "unclosed quote at end",
			input:         `level=info msg="incomplete`,
			requestedKeys: []string{"level", "msg"},
			expected: map[string]string{
				"level": "info",
				"msg":   "incomplete",
			},
			errorContains: []string{"unterminated quoted value", "pos 15"},
		},
		{
			name:          "actual unclosed quote in middle",
			input:         `a="first b=second`,
			requestedKeys: []string{"a"},
			expected: map[string]string{
				"a": `first b=second`, // Parser takes everything until end of input
			},
			errorContains: []string{"unterminated quoted value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)

			require.Error(t, err, "Should return error for unclosed quote")
			for _, contains := range tt.errorContains {
				require.Contains(t, err.Error(), contains, "Error should contain '%s'", contains)
			}

			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenizerErrorHandling(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		requestedKeys []string
		expectedKVs   map[string]string
		errorChecks   []string // Strings that should appear in the error message
		description   string
	}{
		{
			name:          "single error on requested key - error kept",
			input:         "level=info key==value status=200",
			requestedKeys: []string{"level", "key", "status"},
			expectedKVs: map[string]string{
				"level":  "info",
				"status": "200",
			},
			errorChecks: []string{
				"logfmt syntax error",
				"pos 15",
				"unexpected '='",
			},
			description: "Error for requested key 'key' should be included",
		},
		{
			name:          "error on non-requested key - error dropped",
			input:         "level=info bad==input status=200",
			requestedKeys: []string{"level", "status"},
			expectedKVs: map[string]string{
				"level":  "info",
				"status": "200",
			},
			errorChecks: nil, // Error for non-requested key "bad" should be dropped
			description: "Should drop error for non-requested key 'bad' and continue parsing",
		},
		{
			name:          "error after finding all keys - stops early",
			input:         "level=info status=200 bad==error msg=ok",
			requestedKeys: []string{"level", "status"},
			expectedKVs: map[string]string{
				"level":  "info",
				"status": "200",
			},
			errorChecks: nil, // Error happens after early stop, so not collected
			description: "Should stop after finding all keys, not encountering the error",
		},
		{
			name:          "multiple non-requested key errors - all dropped",
			input:         "level=info bad1==err status=200 bad2==err bad3==err",
			requestedKeys: []string{"level", "status"},
			expectedKVs: map[string]string{
				"level":  "info",
				"status": "200",
			},
			errorChecks: nil, // All errors are for non-requested keys, should be dropped
			description: "Should drop all errors for non-requested keys",
		},
		{
			name:          "multiple errors on requested keys - all kept",
			input:         "level=info bad1==input status==200 good=value bad2==another",
			requestedKeys: []string{"level", "status", "good", "bad1", "bad2"},
			expectedKVs: map[string]string{
				"level": "info",
				"good":  "value",
			},
			errorChecks: []string{
				"pos 16",
				"unexpected '='",
				"pos 30",
				"pos 51",
			},
			description: "All errors for requested keys should be included",
		},
		{
			name:          "mixed errors - only requested key errors kept",
			input:         "level=info bad1==input status==200 good=value bad2==another",
			requestedKeys: []string{"level", "status", "good"}, // bad1 and bad2 not requested
			expectedKVs: map[string]string{
				"level": "info",
				"good":  "value",
			},
			errorChecks: []string{
				"pos 30",
				"unexpected '='",
				// bad1 and bad2 errors should be dropped
			},
			description: "Should only report error for requested key 'status', not for bad1 or bad2",
		},
		{
			name:          "unclosed quote (structural error) - always kept",
			input:         `level="info status=200 msg=hello`,
			requestedKeys: []string{"level", "status"},
			expectedKVs: map[string]string{
				"level": "info status=200 msg=hello",
			},
			errorChecks: []string{"unterminated quoted value"},
			description: "Structural error (unclosed quote) should always be reported",
		},
		{
			name:          "quote in non-requested key - dropped",
			input:         `bad"key=value level=info status=200`,
			requestedKeys: []string{"level", "status"},
			expectedKVs: map[string]string{
				"level":  "info",
				"status": "200",
			},
			errorChecks: nil, // Error for non-requested key with quote should be dropped
			description: "Should drop error for quote in non-requested key",
		},
		{
			name:          "invalid UTF-8 in non-requested key value - dropped",
			input:         "level=info bad=" + string([]byte{0xff, 0xfe}) + " status=ok",
			requestedKeys: []string{"level", "status"},
			expectedKVs: map[string]string{
				"level":  "info",
				"status": "ok",
			},
			errorChecks: nil, // UTF-8 error for non-requested key should be dropped
			description: "Should drop UTF-8 error for non-requested key",
		},
		{
			name:          "invalid UTF-8 in requested key value - kept",
			input:         "level=" + string([]byte{0xff, 0xfe}) + " status=ok",
			requestedKeys: []string{"level", "status"},
			expectedKVs: map[string]string{
				"level":  "", // Invalid UTF-8 cleared
				"status": "ok",
			},
			errorChecks: []string{"invalid UTF-8", "level"},
			description: "Should report UTF-8 error for requested key",
		},
		{
			name:          "no error for valid input",
			input:         "level=info status=200 msg=hello",
			requestedKeys: []string{"level", "status", "msg"},
			expectedKVs: map[string]string{
				"level":  "info",
				"status": "200",
				"msg":    "hello",
			},
			errorChecks: nil, // No error expected
			description: "Valid input should not produce errors",
		},
		{
			name:          "all errors when no keys requested",
			input:         "good=yes bad1==no ugly==maybe bad2==nope",
			requestedKeys: []string{}, // Empty means get all keys
			expectedKVs: map[string]string{
				"good": "yes",
				"bad1": "",
				"ugly": "",
				"bad2": "",
			},
			errorChecks: []string{
				"pos 14",
				"pos 23",
				"pos 35",
				"unexpected '='",
			},
			description: "When getting all keys, all errors should be reported",
		},
		{
			name:          "all errors when nil keys requested",
			input:         "good=yes bad1==no ugly==maybe",
			requestedKeys: nil, // nil also means get all keys
			expectedKVs: map[string]string{
				"good": "yes",
				"bad1": "",
				"ugly": "",
			},
			errorChecks: []string{
				"pos 14",
				"pos 23",
				"unexpected '='",
			},
			description: "When requestedKeys is nil, all errors should be reported",
		},
		{
			name:          "error at start of input (position 0)",
			input:         "==bad level=info",
			requestedKeys: []string{}, // Get all keys to see the error
			expectedKVs:   map[string]string{}, // Empty result since tokenizer stops at invalid start
			errorChecks:   nil, // No error is reported since tokenizer stops immediately
			description:   "Should stop immediately when encountering == at start",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TokenizeLogfmt(tt.input, tt.requestedKeys)

			// Check expected key-value pairs
			require.NotNil(t, result)
			for key, expectedValue := range tt.expectedKVs {
				actualValue, ok := result[key]
				require.True(t, ok, "Expected key %s not found in result", key)
				require.Equal(t, expectedValue, actualValue, "Value mismatch for key %s", key)
			}

			// Check error conditions
			if tt.errorChecks == nil {
				require.NoError(t, err, "Expected no error but got: %v", err)
			} else {
				require.Error(t, err, "Expected an error but got none")
				errStr := err.Error()
				for _, check := range tt.errorChecks {
					require.Contains(t, errStr, check, "Error message should contain '%s'", check)
				}
			}

			if tt.description != "" {
				t.Log(tt.description)
			}
		})
	}
}

func TestUnescapeValueFunction(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "fast path - no backslashes",
			input:    "simple text with no escapes",
			expected: "simple text with no escapes",
		},
		{
			name:     "newline escape",
			input:    `line1\nline2`,
			expected: "line1\nline2",
		},
		{
			name:     "tab escape",
			input:    `col1\tcol2`,
			expected: "col1\tcol2",
		},
		{
			name:     "carriage return",
			input:    `before\rafter`,
			expected: "before\rafter",
		},
		{
			name:     "escaped backslash",
			input:    `path\\to\\file`,
			expected: `path\to\file`,
		},
		{
			name:     "escaped double quote",
			input:    `say \"hello\"`,
			expected: `say "hello"`,
		},
		{
			name:     "escaped single quote",
			input:    `it\'s`,
			expected: `it's`,
		},
		{
			name:     "unknown escape preserved",
			input:    `unknown\x`,
			expected: `unknown\x`,
		},
		{
			name:     "backslash at end",
			input:    `ends with\`,
			expected: `ends with\`,
		},
		{
			name:     "multiple escapes",
			input:    `complex\n\t\r\\\"\'end`,
			expected: "complex\n\t\r\\\"'end",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unescapeValue(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
