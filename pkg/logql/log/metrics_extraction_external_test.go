package log_test

import (
	"runtime"
	"testing"
	"weak"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// TestStreamSampleExtractor_LineRetention checks that no stage of a query keeps a reference to the
// line after Process returns. The caller can reuse the memory of the line, or release it, right
// after. A stage that kept the line would pin that memory, even if it never read it again.
//
// The test covers the stages that the query shapes use. A new stage that reads the line needs a
// shape in the table.
func TestStreamSampleExtractor_LineRetention(t *testing.T) {
	// Each case is a query and the lines that the test runs through it. The lines hold the values
	// and the syntax that make each stage of the query do work.
	tests := []struct {
		query string
		lines []string
	}{
		{
			query: `sum by (msg) (count_over_time({app="foo"} | json [1m]))`,
			lines: []string{`{"msg":"hello world AAAA","a":{"b":"nested BBBB"}}`, `{"msg":"second CCCC \"q\" é"}`},
		},
		{
			query: `sum by (a_b, msg) (count_over_time({app="foo"} | json [1m]))`,
			lines: []string{`{"msg":"hello world AAAA","a":{"b":"nested BBBB"}}`, `{"msg":"second CCCC","a":{"b":"n2"}}`},
		},
		{
			query: `count_over_time({app="foo"} | json [1m])`,
			lines: []string{`{"msg":"hello world AAAA","a":{"b":"nested BBBB"},"k key":"v v"}`, `{"msg":"second CCCC","other":"x"}`},
		},
		{
			query: `sum by (msg, level) (count_over_time({app="foo"} | logfmt [1m]))`,
			lines: []string{`level=info msg="hello world AAAA" x=y`, `level=warn msg=second_CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | logfmt [1m])`,
			lines: []string{`level=info msg="hello world AAAA" x=y`, `level=warn msg=second_CCCC k2=v2`},
		},
		{
			query: `count_over_time({app="foo"} | logfmt --strict [1m])`,
			lines: []string{`level=info msg="unterminated AAAA`, `level=warn msg=second_CCCC =oops`},
		},
		{
			query: `sum by (lvl, msg) (count_over_time({app="foo"} | regexp "(?P<lvl>\\w+) (?P<msg>.*)" [1m]))`,
			lines: []string{`info hello world AAAA`, `warn second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | regexp "(?P<lvl>\\w+) (?P<msg>.*)" [1m])`,
			lines: []string{`info hello world AAAA`, `warn second CCCC`},
		},
		{
			query: `sum by (lvl, msg) (count_over_time({app="foo"} | pattern "<lvl> <msg>" [1m]))`,
			lines: []string{`info hello world AAAA`, `warn second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | pattern "<lvl> <msg>" [1m])`,
			lines: []string{`info hello world AAAA`, `warn second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | unpack [1m])`,
			lines: []string{`{"lvl":"info","_entry":"packed entry AAAA"}`, `{"lvl":"warn","_entry":"second CCCC \"q\""}`},
		},
		{
			query: `sum by (lvl) (count_over_time({app="foo"} | unpack [1m]))`,
			lines: []string{`{"lvl":"info","_entry":"packed entry AAAA"}`, `{"lvl":"warn","_entry":"second CCCC"}`},
		},
		{
			query: `sum by (m) (count_over_time({app="foo"} | json m="msg" [1m]))`,
			lines: []string{`{"msg":"hello world AAAA"}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `count_over_time({app="foo"} | json m="msg", n="a.b" [1m])`,
			lines: []string{`{"msg":"hello world AAAA","a":{"b":"nested BBBB"}}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `count_over_time({app="foo"} | logfmt m="msg", l="level" [1m])`,
			lines: []string{`level=info msg="hello world AAAA"`, `level=warn msg=second_CCCC`},
		},
		{
			query: `sum by (m) (count_over_time({app="foo"} | json | line_format "{{.msg}} {{ __line__ }}" | logfmt m="msg" [1m]))`,
			lines: []string{`{"msg":"hello world AAAA"}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `count_over_time({app="foo"} | json | label_format l="{{ __line__ }}", t="{{ __line__ | trunc 6 }}" [1m])`,
			lines: []string{`{"msg":"hello world AAAA"}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `sum by (l) (count_over_time({app="foo"} | label_format l="{{ __line__ }}" [1m]))`,
			lines: []string{`plain line AAAA`, `second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | label_format l="{{ __line__ | urldecode }}" [1m])`,
			lines: []string{`bad%zz escape AAAA`, `second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | label_format l="{{ unixToTime __line__ }}" [1m])`,
			lines: []string{`notanumber AAAA`, `1673798889`},
		},
		{
			query: `count_over_time({app="foo"} | label_format l="{{ __line__ | b64dec }}" [1m])`,
			lines: []string{`!!!notbase64 AAAA`, `aGVsbG8=`},
		},
		{
			query: `count_over_time({app="foo"} | label_format l="{{ toDateInZone \"2006\" __line__ \"2020\" }}" [1m])`,
			lines: []string{`Not/AZone AAAA`, `UTC`},
		},
		{
			query: `count_over_time({app="foo"} | label_format l="{{ regexReplaceAll __line__ \"x\" \"y\" }}" [1m])`,
			lines: []string{`(((unbalanced AAAA`, `second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | line_format "{{ __line__ }}" [1m])`,
			lines: []string{`plain line AAAA`, `second CCCC`},
		},
		{
			query: `sum by (m) (count_over_time({app="foo"} | json | line_format "{{.msg}}" | label_format l="{{ __line__ }}" [1m]))`,
			lines: []string{`{"msg":"hello world AAAA"}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `sum by (n) (sum_over_time({app="foo"} | json | unwrap n [1m]))`,
			lines: []string{`{"n":"abcAAAA"}`, `{"n":"3"}`},
		},
		{
			query: `sum by (__error__, __error_details__) (sum_over_time({app="foo"} | json | unwrap n [1m]))`,
			lines: []string{`{"n":"abcAAAA"}`, `{"n":"3"}`},
		},
		{
			query: `sum by (__error__, __error_details__) (sum_over_time({app="foo"} | json | unwrap bytes(n) [1m]))`,
			lines: []string{`{"n":"abcAAAA"}`, `{"n":"3kb"}`},
		},
		{
			query: `sum by (__error__, __error_details__) (sum_over_time({app="foo"} | json | unwrap duration(n) [1m]))`,
			lines: []string{`{"n":"abcAAAA"}`, `{"n":"3s"}`},
		},
		{
			query: `count_over_time({app="foo"} | json [1m])`,
			lines: []string{`{"msg": `, `{"msg":"BBBB" `},
		},
		{
			query: `sum by (__error__, __error_details__) (count_over_time({app="foo"} | json [1m]))`,
			lines: []string{`not json AAAA`, `{"msg":`},
		},
		{
			query: `count_over_time({app="foo"} | json | msg=~"hel.*" [1m])`,
			lines: []string{`{"msg":"hello world AAAA"}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `count_over_time({app="foo"} | json | n > 1 [1m])`,
			lines: []string{`{"n":"abcAAAA"}`, `{"n":"3"}`},
		},
		{
			query: `count_over_time({app="foo"} | json | n > 1s [1m])`,
			lines: []string{`{"n":"abcAAAA"}`, `{"n":"3s"}`},
		},
		{
			query: `count_over_time({app="foo"} | json | n > 1KB [1m])`,
			lines: []string{`{"n":"abcAAAA"}`, `{"n":"3kb"}`},
		},
		{
			query: `count_over_time({app="foo"} | json | drop msg [1m])`,
			lines: []string{`{"msg":"hello world AAAA","k":"v"}`, `{"msg":"second CCCC","k":"w"}`},
		},
		{
			query: `count_over_time({app="foo"} | json | keep msg [1m])`,
			lines: []string{`{"msg":"hello world AAAA","k":"v"}`, `{"msg":"second CCCC","k":"w"}`},
		},
		{
			query: `count_over_time({app="foo"} | decolorize | json [1m])`,
			lines: []string{"{\"msg\":\"\x1b[31mred AAAA\x1b[0m\"}", `{"msg":"plain CCCC"}`},
		},
		{
			query: `count_over_time({app="foo"} | json | addr = ip("10.0.0.0/8") [1m])`,
			lines: []string{`{"addr":"10.1.2.3","msg":"AAAA"}`, `{"addr":"11.1.2.3"}`},
		},
		{
			query: `sum without (x) (count_over_time({app="foo"} | json [1m]))`,
			lines: []string{`{"msg":"hello world AAAA"}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `sum by (msg) (count_over_time({app="foo"} | json | json msg="msg" [1m]))`,
			lines: []string{`{"msg":"hello world AAAA"}`, `{"msg":"second CCCC"}`},
		},
		{
			query: `sum by (app_extracted, trace_extracted, msg) (count_over_time({app="foo"} | json [1m]))`,
			lines: []string{`{"app":"hello world AAAA","trace":"tttt","msg":"m"}`, `{"app":"x","trace":"y","msg":"CCCC"}`},
		},
		{
			query: `count_over_time({app="foo"} | logfmt | label_format x=msg [1m])`,
			lines: []string{`msg="hello world AAAA"`, `msg=second_CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | json | line_format "{{ .msg }}" | json [1m])`,
			lines: []string{`{"msg":"{\"in\":\"inner AAAA\"}"}`, `{"msg":"{\"in\":\"inner CCCC\"}"}`},
		},
		{
			query: `count_over_time({app="foo"} | label_format l="{{ __line__ | fromJson }}" [1m])`,
			lines: []string{`{"msg":"AAAA"}`, `{"msg":"CCCC"}`},
		},
		{
			query: `count_over_time({app="foo"} [1m])`,
			lines: []string{`plain line AAAA`, `second CCCC`},
		},
		{
			query: `bytes_over_time({app="foo"} [1m])`,
			lines: []string{`plain line AAAA`, `second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} |= "AAAA" != "BBBB" [1m])`,
			lines: []string{`plain line AAAA`, `second CCCC`, `third AAAA BBBB`},
		},
		{
			query: `count_over_time({app="foo"} |~ "A+" !~ "C{4}" [1m])`,
			lines: []string{`plain line AAAA`, `second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} |= ip("10.0.0.0/8") [1m])`,
			lines: []string{`from 10.1.2.3 AAAA`, `from 11.1.2.3 CCCC`},
		},
		{
			query: `count_over_time({app="foo"} |> "<_> AAAA" !> "<_> CCCC" [1m])`,
			lines: []string{`plain AAAA`, `second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | pattern "<lvl> <msg>" | lvl="info" [1m])`,
			lines: []string{`info hello world AAAA`, `warn second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | regexp "(?P<lvl>\\w+) (?P<msg>.*)" | lvl="info" [1m])`,
			lines: []string{`info hello world AAAA`, `warn second CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | json | __error__="" [1m])`,
			lines: []string{`{"msg":"hello world AAAA"}`, `not json CCCC`},
		},
		{
			query: `count_over_time({app="foo"} | logfmt --keep-empty [1m])`,
			lines: []string{`msg= x=y AAAA`, `level=warn msg=second_CCCC`},
		},
	}

	// processInFreshBuffer copies the line into a new buffer, runs it through the extractor, and
	// returns a weak pointer to the buffer. Nothing outside the extractor keeps the buffer, so the
	// pointer is nil after a GC unless a stage retains the line. The buffer has spare capacity, so
	// the allocator does not place it in a shared block.
	processInFreshBuffer := func(extractor log.StreamSampleExtractor, line string) weak.Pointer[byte] {
		buf := make([]byte, len(line), len(line)+128)
		copy(buf, line)
		ref := weak.Make(&buf[0])

		sample, ok := extractor.Process(1, buf, labels.FromStrings("sm", "meta"))
		if ok {
			_ = sample.Labels.String()
		}
		return ref
	}

	for _, tt := range tests {
		t.Run(tt.query+" retains no reference to the line", func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(tt.query)
			require.NoError(t, err)
			extractor, err := expr.Extractor()
			require.NoError(t, err)
			streamExtractor := extractor.ForStream(labels.FromStrings("app", "foo", "trace", "tstream"))

			// Run two rounds, so the second round exercises the stages with the state that the first
			// left, such as warm field caches.
			for range 2 {
				for _, line := range tt.lines {
					ref := processInFreshBuffer(streamExtractor, line)
					runtime.GC()
					require.Nil(t, ref.Value(), "line %q", line)
				}
			}
		})
	}
}
