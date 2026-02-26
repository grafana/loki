package matchutil

import (
	"bytes"
	"testing"
)

func TestContainsUpper(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		substr string
		want   bool
	}{
		// Basic ASCII cases
		{
			name:   "exact match uppercase",
			line:   "error",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "exact match uppercase line",
			line:   "ERROR",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "exact match mixed case line",
			line:   "ErRoR",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "substring at start",
			line:   "error occurred",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "substring in middle",
			line:   "an error occurred",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "substring at end",
			line:   "this is an error",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "substring uppercase line",
			line:   "AN ERROR OCCURRED",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "substring mixed case line",
			line:   "An ErRoR Occurred",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "no match",
			line:   "warning message",
			substr: "ERROR",
			want:   false,
		},
		{
			name:   "partial match not enough",
			line:   "err",
			substr: "ERROR",
			want:   false,
		},
		{
			name:   "empty substr always matches",
			line:   "anything",
			substr: "",
			want:   true,
		},
		{
			name:   "empty line with empty substr",
			line:   "",
			substr: "",
			want:   true,
		},
		{
			name:   "empty line with non-empty substr",
			line:   "",
			substr: "ERROR",
			want:   false,
		},
		{
			name:   "substr longer than line",
			line:   "err",
			substr: "ERROR",
			want:   false,
		},
		{
			name:   "single character match",
			line:   "ABC",
			substr: "B",
			want:   true,
		},

		// Unicode cases
		{
			name:   "unicode uppercase",
			line:   "münchen",
			substr: "MÜNCHEN",
			want:   true,
		},
		{
			name:   "unicode uppercase line",
			line:   "MÜNCHEN",
			substr: "MÜNCHEN",
			want:   true,
		},
		{
			name:   "unicode mixed case",
			line:   "MüNcHeN",
			substr: "MÜNCHEN",
			want:   true,
		},
		{
			name:   "unicode in substring",
			line:   "I love MÜNCHEN",
			substr: "MÜNCHEN",
			want:   true,
		},
		{
			name:   "unicode no match",
			line:   "berlin",
			substr: "MÜNCHEN",
			want:   false,
		},
		{
			name:   "unicode greek",
			line:   "ΑΒΓΔ",
			substr: "ΑΒΓΔ",
			want:   true,
		},
		{
			name:   "unicode cyrillic",
			line:   "ПРИВЕТ",
			substr: "ПРИВЕТ",
			want:   true,
		},

		// Mixed ASCII and Unicode
		{
			name:   "ascii then unicode",
			line:   "Error in München",
			substr: "MÜNCHEN",
			want:   true,
		},
		{
			name:   "unicode then ascii",
			line:   "münchen ERROR",
			substr: "ERROR",
			want:   true,
		},

		// Edge cases
		{
			name:   "multiple occurrences",
			line:   "error error error",
			substr: "ERROR",
			want:   true,
		},
		{
			name:   "overlapping pattern",
			line:   "aaaa",
			substr: "AAA",
			want:   true,
		},
		{
			name:   "case difference only",
			line:   "AbCdEf",
			substr: "ABCDEF",
			want:   true,
		},
		{
			name:   "numbers are case-insensitive match",
			line:   "error123",
			substr: "ERROR123",
			want:   true,
		},
		{
			name:   "special characters",
			line:   "error: message!",
			substr: "ERROR:",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := []byte(tt.line)
			substr := []byte(tt.substr)

			got := ContainsUpper(line, substr)
			if got != tt.want {
				t.Errorf("ContainsUpper(%q, %q) = %v, want %v", tt.line, tt.substr, got, tt.want)
			}
		})
	}
}

// Benchmark tests to verify performance characteristics
func BenchmarkContainsUpper(b *testing.B) {
	line := []byte("This is a test error message with some content")
	substr := []byte("ERROR")

	for b.Loop() {
		ContainsUpper(line, substr)
	}
}

func BenchmarkContainsUpperUnicode(b *testing.B) {
	line := []byte("This is a test MÜNCHEN message with some content")
	substr := []byte("MÜNCHEN")

	for b.Loop() {
		ContainsUpper(line, substr)
	}
}

func BenchmarkEqualUpper(b *testing.B) {
	line := []byte("ERROR")
	match := []byte("ERROR")

	for b.Loop() {
		EqualUpper(line, match)
	}
}

// naiveContainsUpper is a naive implementation that converts the entire line to uppercase
// before checking if it contains the substring. This is used for comparison benchmarking.
func naiveContainsUpper(line, substr []byte) bool {
	upperLine := bytes.ToUpper(line)
	return bytes.Contains(upperLine, substr)
}

// Benchmark comparison tests to measure the performance benefit of the optimized approach
// versus a naive bytes.ToUpper + bytes.Contains implementation.
func BenchmarkContainsUpperNaive(b *testing.B) {
	line := []byte("This is a test error message with some content")
	substr := []byte("ERROR")

	for i := 0; i < b.N; i++ {
		naiveContainsUpper(line, substr)
	}
}

func BenchmarkContainsUpperUnicodeNaive(b *testing.B) {
	line := []byte("This is a test MÜNCHEN message with some content")
	substr := []byte("MÜNCHEN")

	for i := 0; i < b.N; i++ {
		naiveContainsUpper(line, substr)
	}
}

// Benchmark with longer lines to amplify the difference
func BenchmarkContainsUpperLongLine(b *testing.B) {
	line := []byte("This is a much longer log line with many words and characters that will test the performance of our case-insensitive matching algorithm. We want to see how well it scales with longer input. The error keyword appears somewhere in the middle of this very long line.")
	substr := []byte("ERROR")

	for i := 0; i < b.N; i++ {
		ContainsUpper(line, substr)
	}
}

func BenchmarkContainsUpperLongLineNaive(b *testing.B) {
	line := []byte("This is a much longer log line with many words and characters that will test the performance of our case-insensitive matching algorithm. We want to see how well it scales with longer input. The error keyword appears somewhere in the middle of this very long line.")
	substr := []byte("ERROR")

	for i := 0; i < b.N; i++ {
		naiveContainsUpper(line, substr)
	}
}
