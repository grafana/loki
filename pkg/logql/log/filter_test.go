package log

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_SimplifiedRegex(t *testing.T) {
	fixtures := []string{
		"foo", "foobar", "bar", "foobuzz", "buzz", "f", "  ", "fba", "foofoofoo", "b", "foob", "bfoo", "FoO",
		"foo, 世界", allunicode(), "fooÏbar",
	}
	for _, test := range []struct {
		re string

		// Simplified is true when the regex is converted to non-regex filters
		// or when the regex is rewritten to be non-greedy
		simplified bool

		// Expected != nil when the regex is converted to non-regex filters
		expected Filterer
		match    bool
	}{
		// regex we intend to support.
		{"foo", true, newContainsFilter([]byte("foo"), false), true},
		{"not", true, NewNotFilter(newContainsFilter([]byte("not"), false)), false},
		{"(foo)", true, newContainsFilter([]byte("foo"), false), true},
		{"(foo|ba)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("ba"), false)), true},
		{"(foo|ba|ar)", true, newOrFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("ba"), false)), newContainsFilter([]byte("ar"), false)), true},
		{"(foo|(ba|ar))", true, newOrFilter(newContainsFilter([]byte("foo"), false), newOrFilter(newContainsFilter([]byte("ba"), false), newContainsFilter([]byte("ar"), false))), true},
		{"foo.*", true, newContainsFilter([]byte("foo"), false), true},
		{".*foo", true, NewNotFilter(newContainsFilter([]byte("foo"), false)), false},
		{".*foo.*", true, newContainsFilter([]byte("foo"), false), true},
		{"(.*)(foo).*", true, newContainsFilter([]byte("foo"), false), true},
		{"(foo.*|.*ba)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("ba"), false)), true},
		{"(foo.*|.*bar.*)", true, NewNotFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false))), false},
		{".*foo.*|bar", true, NewNotFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false))), false},
		{".*foo|bar", true, NewNotFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false))), false},
		// This construct is similar to (...), but won't create a capture group.
		{"(?:.*foo.*|bar)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false)), true},
		// named capture group
		{"(?P<foo>.*foo.*|bar)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false)), true},
		// parsed as (?-s:.)*foo(?-s:.)*|b(?:ar|uzz)
		{".*foo.*|bar|buzz", true, newOrFilter(newContainsFilter([]byte("foo"), false), newOrFilter(newContainsFilter([]byte("bar"), false), newContainsFilter([]byte("buzz"), false))), true},
		// parsed as (?-s:.)*foo(?-s:.)*|bar|uzz
		{".*foo.*|bar|uzz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false)), newContainsFilter([]byte("uzz"), false)), true},
		// parsed as foo|b(?:ar|(?:)|uzz)|zz
		{"foo|bar|b|buzz|zz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newOrFilter(newOrFilter(newContainsFilter([]byte("bar"), false), newContainsFilter([]byte("b"), false)), newContainsFilter([]byte("buzz"), false))), newContainsFilter([]byte("zz"), false)), true},
		// parsed as f(?:(?:)|oo(?:(?:)|bar))
		{"f|foo|foobar", true, newOrFilter(newContainsFilter([]byte("f"), false), newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("foobar"), false))), true},
		// parsed as f(?:(?-s:.)*|oobar(?-s:.)*)|(?-s:.)*buzz
		{"f.*|foobar.*|.*buzz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("f"), false), newContainsFilter([]byte("foobar"), false)), newContainsFilter([]byte("buzz"), false)), true},
		// parsed as ((f(?-s:.)*)|foobar(?-s:.)*)|(?-s:.)*buzz
		{"((f.*)|foobar.*)|.*buzz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("f"), false), newContainsFilter([]byte("foobar"), false)), newContainsFilter([]byte("buzz"), false)), true},
		{".*", true, TrueFilter, true},
		{".*|.*", true, TrueFilter, true},
		{".*||||", true, TrueFilter, true},
		{"", true, TrueFilter, true},
		{"(?i)foo", true, newContainsFilter([]byte("foo"), true), true},
		{"(?i)界", true, newContainsFilter([]byte("界"), true), true},
		{"(?i)ïB", true, newContainsFilter([]byte("ïB"), true), true},
		{"(?:)foo|fatal|exception", true, newOrFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("fatal"), false)), newContainsFilter([]byte("exception"), false)), true},
		{"(?i)foo|fatal|exception", true, newOrFilter(newOrFilter(newContainsFilter([]byte("FOO"), true), newContainsFilter([]byte("FATAL"), true)), newContainsFilter([]byte("exception"), true)), true},
		{"(?i)f|foo|foobar", true, newOrFilter(newContainsFilter([]byte("F"), true), newOrFilter(newContainsFilter([]byte("FOO"), true), newContainsFilter([]byte("FOOBAR"), true))), true},
		{"(?i)f|fatal|e.*", true, newOrFilter(newOrFilter(newContainsFilter([]byte("F"), true), newContainsFilter([]byte("FATAL"), true)), newContainsFilter([]byte("E"), true)), true},
		{"(?i).*foo.*", true, newContainsFilter([]byte("FOO"), true), true},
		{".+", true, ExistsFilter, true},

		// These regexes are rewritten to be non-greedy but no new
		// filter is generated.
		{"[a-z]+foo", true, nil, true},
		{".+foo", true, nil, true},
		{".*fo.*o", true, nil, true},
		{`\d`, true, nil, true},
		{`\sfoo`, true, nil, true},
		{`foo?`, false, nil, true},
		{`foo{1,2}bar{2,3}`, true, nil, true},
		{`foo|\d*bar`, true, nil, true},
		{`foo|fo{1,2}`, true, nil, true},
		{`foo|fo\d*`, true, nil, true},
		{`foo|fo\d+`, true, nil, true},
		{`(\w\d+)`, true, nil, true},
		{`.*f.*oo|fo{1,2}`, true, nil, true},
		{"f|f(?i)oo", true, nil, true},
		{".foo+", true, nil, true},
	} {
		t.Run(test.re, func(t *testing.T) {
			d, err := newRegexpFilter(test.re, test.re, test.match)
			require.NoError(t, err, "invalid regex")

			f, err := parseRegexpFilter(test.re, test.match, false)
			require.NoError(t, err)

			// if we don't expect simplification then the filter should be the same as the default one.
			if !test.simplified {
				require.Equal(t, d, f)
				return
			}

			// otherwise ensure we have different filter
			require.NotEqual(t, f, d)
			if test.expected != nil {
				require.Equal(t, test.expected, f)
			} else {
				reFilter, ok := f.(regexpFilter)
				require.True(t, ok)
				require.Equal(t, test.re, reFilter.String())
			}

			// tests all lines with both filter, they should have the same result.
			for _, line := range fixtures {
				l := []byte(line)
				require.Equal(t, d.Filter(l), f.Filter(l), "regexp %s failed line: %s re:%v simplified:%v", test.re, line, d.Filter(l), f.Filter(l))
			}
		})
	}
}

func allunicode() string {
	var b []byte
	for i := 0x00; i < 0x10FFFF; i++ {
		b = append(b, byte(i))
	}
	return string(b)
}

func Test_TrueFilter(t *testing.T) {
	empty := []byte("")
	for _, test := range []struct {
		name       string
		f          Filterer
		expectTrue bool
	}{
		{"empty match", newContainsFilter(empty, false), true},
		{"not empty match", NewNotFilter(newContainsFilter(empty, true)), false},
		{"match", newContainsFilter([]byte("foo"), false), false},
		{"empty match and", NewAndFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)), true},
		{"empty match or", newOrFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)), true},
		{"nil right and", NewAndFilter(newContainsFilter(empty, false), nil), true},
		{"nil left or", newOrFilter(nil, newContainsFilter(empty, false)), true},
		{"nil right and not empty", NewAndFilter(newContainsFilter([]byte("foo"), false), nil), false},
		{"nil left or not empty", newOrFilter(nil, newContainsFilter([]byte("foo"), false)), false},
		{"nil both and", NewAndFilter(nil, nil), false}, // returns nil
		{"nil both or", newOrFilter(nil, nil), false},   // returns nil
		{"empty match and chained", NewAndFilter(newContainsFilter(empty, false), NewAndFilter(newContainsFilter(empty, false), NewAndFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)))), true},
		{"empty match or chained", newOrFilter(newContainsFilter(empty, false), newOrFilter(newContainsFilter(empty, true), newOrFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)))), true},
		{"empty match and", NewNotFilter(NewAndFilter(newContainsFilter(empty, false), newContainsFilter(empty, false))), false},
		{"empty match or", NewNotFilter(newOrFilter(newContainsFilter(empty, false), newContainsFilter(empty, false))), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.expectTrue {
				require.Equal(t, TrueFilter, test.f)
			} else {
				require.NotEqual(t, TrueFilter, test.f)
			}
		})
	}
}

func Benchmark_LineFilter(b *testing.B) {
	b.ReportAllocs()
	logline := `level=bar ts=2020-02-22T14:57:59.398312973Z caller=logging.go:44 traceID=2107b6b551458908 msg="GET /buzz (200) 4.599635ms`
	for _, test := range []struct {
		re string
	}{
		{"foo.*"},
		{"(?i)foo"},
		{".*foo.*"},
		{".*foo"},
		{"foo|bar"},
		{"foo|bar|buzz"},
		{"foo|(bar|buzz)"},
		{"foo|bar.*|buzz"},
		{".*foo.*|bar|uzz"},
		{"((f.*)|foobar.*)|.*buzz"},
		{"(?P<foo>.*foo.*|bar)"},
		{"(\\s|\")+(?i)bar"},
		{"(node:24) buzz*"},
		{"(HTTP/.*\\\"|HEAD|GET) (2..|5..)"},
		{"\"@l\":\"(Warning|Error|Fatal)\""},
		{"(?:)foo|fatal|exception"},
	} {
		benchmarkRegex(b, test.re, logline, true)
		benchmarkRegex(b, test.re, logline, false)
	}
}

// see https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go
// A note on compiler optimizations
var res bool

func benchmarkRegex(b *testing.B, re, line string, match bool) {
	var m bool
	l := []byte(line)
	d, err := newRegexpFilter(re, re, match)
	if err != nil {
		b.Fatal(err)
	}
	s, err := parseRegexpFilter(re, match, false)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.Run(fmt.Sprintf("default_%v_%s", match, re), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m = d.Filter(l)
		}
	})
	b.Run(fmt.Sprintf("simplified_%v_%s", match, re), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m = s.Filter(l)
		}
	})
	res = m
}

func Test_rune(t *testing.T) {
	require.True(t, newContainsFilter([]byte("foo"), true).Filter([]byte("foo")))
}

var cases = []struct {
	name     string
	line     string
	substr   string
	expected bool
}{
	{
		name:     "short_line_no_match",
		line:     "this is a short log line",
		substr:   "missing",
		expected: false,
	},
	{
		name:     "short_line_no_match_special_chars",
		line:     "this contains a \\ character",
		substr:   "|",
		expected: false,
	},
	{
		name:     "short_line_no_match_special_chars_match",
		line:     "this contains a | character",
		substr:   "|",
		expected: true,
	},
	{
		name:     "short_line_with_match",
		line:     "this is a shorT log line",
		substr:   "short",
		expected: true,
	},
	{
		name:     "long_line_no_match",
		line:     "2023-06-14T12:34:56.789Z INFO  [service_name] This is a much longer log line with timestamps, levels and other information that typically appears in production logs. RequestID=123456 UserID=789 Action=GetUser Duration=123ms Status=200",
		substr:   "nonexistent",
		expected: false,
	},
	{
		name:     "long_line_match_start",
		line:     "2023-06-14T12:34:56.789Z INFO  [service_name] This is a much longer log line with timestamps, levels and other information that typically appears in production logs. RequestID=123456 UserID=789 Action=GetUser Duration=123ms Status=200",
		substr:   "2023",
		expected: true,
	},
	{
		name:     "long_line_match_middle",
		line:     "2023-06-14T12:34:56.789Z INFO  [service_name] This is a much longer log line with timestamps, leVelS and other information that typically appears in production logs. RequestID=123456 UserID=789 Action=GetUser Duration=123ms Status=200",
		substr:   "levels",
		expected: true,
	},
	{
		name:     "long_line_match_end",
		line:     "2023-06-14T12:34:56.789Z INFO  [service_name] This is a much longer log line with timestamps, levels and other information that typically appears in production logs. RequestID=123456 UserID=789 Action=GetUser Duration=123ms Status=200",
		substr:   "status",
		expected: true,
	},
	{
		name:     "short_unicode_line_no_match",
		line:     "🌟 Unicode line with emojis 🎉 and special chars ñ é ß",
		substr:   "missing",
		expected: false,
	},
	{
		name:     "short_unicode_line_with_match",
		line:     "🌟 Unicode line with eMojiS 🎉 and special chars ñ é ß",
		substr:   "emojis",
		expected: true,
	},
	{
		name:     "long_unicode_line_no_match",
		line:     "2023-06-14T12:34:56.789Z 🚀 [микросервис] Длинное сообщение с Unicode символами 统一码 が大好き! エラー分析: システムは正常に動作しています。RequestID=123456 状態=良好 Résultat=Succès ß=γ 🎯 τέλος",
		substr:   "nonexistent",
		expected: false,
	},
	{
		name:     "long_unicode_line_match_start",
		line:     "2023-06-14T12:34:56.789Z 🚀[МИКРОСервис] Длинное сообщение с Unicode символами 统一码 が大好き! エラー分析: システムは正常に動作しています。RequestID=123456 状態=良好 Résultat=Succès ß=γ 🎯 τέλος",
		substr:   "микросервис",
		expected: true,
	},
	{
		name:     "long_unicode_line_match_middle",
		line:     "2023-06-14T12:34:56.789Z 🚀 [микросервис] Длинное сообщение с Unicode символами 统一码 が大好き! エラー分析: システムは正常に動作しています。RequestID=123456 状態=良好 Résultat=Succès ß=γ 🎯 τέλος",
		substr:   "unicode",
		expected: true,
	},
	{
		name:     "long_unicode_line_match_end",
		line:     "2023-06-14T12:34:56.789Z 🚀 [микросервис] Длинное сообщение с Unicode символами 统一码 が大好き! エラー分析: システムは正常に動作しています。RequestID=123456 状態=良好 Résultat=Succès ß=γ 🎯 τέλος",
		substr:   "τέλος",
		expected: true,
	},
	{
		name:     "utf8_case_insensitive_match_middle",
		line:     "ΣΑΣ ΓΕΙΑ ΚΟΣΜΕ", // "WORLD HELLO WORLD" in Greek uppercase
		substr:   "γεια",           // "hello" in Greek lowercase
		expected: true,
	},
	{
		name:     "utf8_case_insensitive_no_match",
		line:     "ΣΑΣ ΚΟΣΜΕ", // "WORLD WORLD" in Greek uppercase
		substr:   "γεια",      // "hello" in Greek lowercase
		expected: false,
	},
	{
		name:     "empty_substr",
		line:     "any line",
		substr:   "",
		expected: true,
	},
	{
		name:     "empty_line",
		line:     "",
		substr:   "something",
		expected: false,
	},
	{
		name:     "both_empty",
		line:     "",
		substr:   "",
		expected: true,
	},
	{
		name:     "substr_longer_than_line",
		line:     "short",
		substr:   "longer than line",
		expected: false,
	},
	{
		name:     "invalid_utf8_in_line",
		line:     string([]byte{0xFF, 0xFE, 0xFD}),
		substr:   "test",
		expected: false,
	},
	{
		name:     "partial_utf8_match",
		line:     "Hello 世界", // "Hello World" with CJK characters
		substr:   "世",        // Just "World"
		expected: true,
	},
}

func Test_containsLower(t *testing.T) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := []byte(c.line)
			substr := []byte(c.substr)
			m := containsLower(line, substr)
			require.Equal(t, c.expected, m, "line: %s substr: %s", c.line, c.substr)
		})
	}
}

func BenchmarkContainsLower(b *testing.B) {
	var m bool
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			line := []byte(c.line)
			substr := []byte(c.substr)
			for i := 0; i < b.N; i++ {
				m = containsLower(line, substr)
			}
			if m != c.expected {
				b.Fatalf("expected %v but got %v", c.expected, m)
			}
		})
	}
	res = m // Avoid compiler optimization
}
