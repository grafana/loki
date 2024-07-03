package drain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type TestCase struct {
	name string
	line string
	want map[string][]string
}

const typePunctuation = "punctuation"
const typeSplitting = "splitting"

var testCases = []TestCase{
	{
		name: "Test with equals sign",
		line: "key1=value1 key2=value2",
		want: map[string][]string{
			typePunctuation: {"key1", "=", "value1", "key2", "=", "value2"},
			typeSplitting:   {"key1=", "value1", "key2=", "value2"},
		},
	},
	{
		name: "Test with colon",
		line: "key1:value1 key2:value2",
		want: map[string][]string{
			typePunctuation: {"key1:value1", "key2:value2"},
			typeSplitting:   {"key1:", "value1", "key2:", "value2"},
		},
	},
	{
		name: "Test with mixed delimiters, more = than :",
		line: "key1=value1 key2:value2 key3=value3",
		want: map[string][]string{
			typePunctuation: {"key1", "=", "value1", "key2:value2", "key3", "=", "value3"},
			typeSplitting:   {"key1=", "value1", "key2:value2", "key3=", "value3"},
		},
	},
	{
		name: "Test with mixed delimiters, more : than =",
		line: "key1:value1 key2:value2 key3=value3",
		want: map[string][]string{
			typePunctuation: {"key1:value1", "key2:value2", "key3", "=", "value3"},
			typeSplitting:   {"key1:", "value1", "key2:", "value2", "key3=value3"},
		},
	},
	{
		name: "Dense json",
		line: `{"key1":"value1","key2":"value2","key3":"value3"}`,
		want: map[string][]string{
			typePunctuation: {`{`, `"`, `key1`, `"`, `:`, `"`, `value1`, `"`, `,`, `"`, `key2`, `"`, `:`, `"`, `value2`, `"`, `,`, `"`, `key3`, `"`, `:`, `"`, `value3`, `"`, `}`},
			typeSplitting:   {`{"key1":`, `"value1","key2":`, `"value2","key3":`, `"value3"}`},
		},
	},
	{
		name: "json with spaces",
		line: `{"key1":"value1", "key2":"value2", "key3":"value3"}`,
		want: map[string][]string{
			typePunctuation: {`{`, `"`, `key1`, `"`, `:`, `"`, `value1`, `"`, `,`, `"`, `key2`, `"`, `:`, `"`, `value2`, `"`, `,`, `"`, `key3`, `"`, `:`, `"`, `value3`, `"`, `}`},
			typeSplitting:   {`{"key1":`, `"value1",`, `"key2":`, `"value2",`, `"key3":`, `"value3"}`},
		},
	},
	{
		name: "logfmt multiword values",
		line: `key1=value1 key2=value2 msg="this is a message"`,
		want: map[string][]string{
			typePunctuation: {"key1", "=", "value1", "key2", "=", "value2", "msg", "=", `"`, `this`, "is", "a", `message`, `"`},
			typeSplitting:   {"key1=", "value1", "key2=", "value2", "msg=", `"this`, "is", "a", `message"`},
		},
	},
	{
		name: "longer line",
		line: "09:17:38.033366 ▶ INFO  route ops sending to dest https://graphite-cortex-ops-blocks-us-east4.grafana.net/graphite/metrics: service_is_carbon-relay-ng.instance_is_carbon-relay-ng-c665b7b-j2trk.mtype_is_counter.dest_is_https_graphite-cortex-ops-blocks-us-east4_grafana_netgraphitemetrics.unit_is_Metric.action_is_drop.reason_is_queue_full 0 1717060658",
		want: map[string][]string{
			typePunctuation: {`09:17:38.033366`, `▶`, `INFO`, `route`, `ops`, `sending`, `to`, `dest`, `https:`, `/`, `/`, `graphite-cortex-ops-blocks-us-east4.grafana.net`, `/`, `graphite`, `/`, `metrics:`, `service_is_carbon-relay-ng.instance_is_carbon-relay-ng-c665b7b-j2trk.mtype_is_counter.dest_is_https_graphite-cortex-ops-blocks-us-east4_grafana_netgraphitemetrics.unit_is_Metric.action_is_drop.reason_is_queue_full`, `0`, `1717060658`},
			typeSplitting:   {`09:`, `17:`, `38.033366`, `▶`, `INFO`, ``, `route`, `ops`, `sending`, `to`, `dest`, `https:`, `//graphite-cortex-ops-blocks-us-east4.grafana.net/graphite/metrics:`, ``, `service_is_carbon-relay-ng.instance_is_carbon-relay-ng-c665b7b-j2trk.mtype_is_counter.dest_is_https_graphite-cortex-ops-blocks-us-east4_grafana_netgraphitemetrics.unit_is_Metric.action_is_drop.reason_is_queue_full`, `0`, `1717060658`},
		},
	},
	{
		name: "Consecutive splits points: equals followed by space",
		line: `ts=2024-05-30T12:50:36.648377186Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
		want: map[string][]string{
			typePunctuation: {`ts`, `=`, `2024-05-30T12:50:36.648377186Z`, `caller`, `=`, `scheduler_processor.go:143`, `level`, `=`, `warn`, `msg`, `=`, `"`, `error`, `contacting`, `scheduler`, `"`, `err`, `=`, `"`, `rpc`, `error:`, `code`, `=`, `Unavailable`, `desc`, `=`, `connection`, `error:`, `desc`, `=`, `\`, `"`, `error`, `reading`, `server`, `preface:`, `EOF`, `\`, `"`, `"`, `addr`, `=`, `10.0.151.101:9095`},
			typeSplitting:   {"ts=", "2024-05-30T12:50:36.648377186Z", "caller=", "scheduler_processor.go:143", "level=", "warn", "msg=", "\"error", "contacting", "scheduler\"", "err=", "\"rpc", "error:", "code", "=", ``, "Unavailable", "desc", "=", ``, "connection", "error:", "desc", "=", ``, `\"error`, "reading", "server", "preface:", `EOF\""`, "addr=", "10.0.151.101:9095"},
		},
	},
	{
		name: "Exactly 128 tokens are not combined",
		line: strings.Repeat(`A `, 126) + "127 128",
		want: map[string][]string{
			typePunctuation: append(strings.Split(strings.Repeat(`A `, 126), " ")[:126], "127", "128"),
			typeSplitting:   append(strings.Split(strings.Repeat(`A `, 126), " ")[:126], "127", "128"),
		},
	},
	{
		name: "More than 128 tokens combined suffix into one token",
		line: strings.Repeat(`A `, 126) + "127 128 129",
		want: map[string][]string{
			typePunctuation: append(strings.Split(strings.Repeat(`A `, 126), " ")[:126], "127", "128 129"),
			typeSplitting:   append(strings.Split(strings.Repeat(`A `, 126), " ")[:126], "127", "128", "129"),
		},
	},
	{
		name: "Only punctation",
		line: `!@£$%^&*()`,
		want: map[string][]string{
			typePunctuation: {`!`, `@`, `£$`, `%`, `^`, `&`, `*`, `(`, `)`},
			typeSplitting:   {`!@£$%^&*()`},
		},
	},
}

func TestTokenizer_Tokenize(t *testing.T) {
	tests := []struct {
		name      string
		tokenizer LineTokenizer
	}{
		{
			name:      typePunctuation,
			tokenizer: newPunctuationTokenizer(),
		},
		{
			name:      typeSplitting,
			tokenizer: splittingTokenizer{},
		},
	}

	for _, tt := range tests {
		for _, tc := range testCases {
			t.Run(tt.name+":"+tc.name, func(t *testing.T) {
				got, _ := tt.tokenizer.Tokenize(tc.line)
				require.Equal(t, tc.want[tt.name], got)
			})
		}
	}
}

func TestTokenizer_TokenizeAndJoin(t *testing.T) {
	tests := []struct {
		name      string
		tokenizer LineTokenizer
	}{
		{
			name:      typePunctuation,
			tokenizer: newPunctuationTokenizer(),
		},
		{
			name:      typeSplitting,
			tokenizer: splittingTokenizer{},
		},
	}

	for _, tt := range tests {
		for _, tc := range testCases {
			t.Run(tt.name+":"+tc.name, func(t *testing.T) {
				got := tt.tokenizer.Join(tt.tokenizer.Tokenize(tc.line))
				require.Equal(t, tc.line, got)
			})
		}
	}
}

func BenchmarkSplittingTokenizer(b *testing.B) {
	tokenizer := newPunctuationTokenizer()

	for _, tt := range testCases {
		tc := tt
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				tokenizer.Tokenize(tc.line)
			}
		})
	}
}
