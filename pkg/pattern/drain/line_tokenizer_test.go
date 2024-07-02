package drain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type TestCase struct {
	name string
	line string
	want map[string][]string
}

const (
	typePunctuation = "punctuation"
	typeSplitting   = "splitting"
)

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
			typePunctuation: {"key1", ":", "value1", "key2", ":", "value2"},
			typeSplitting:   {"key1:", "value1", "key2:", "value2"},
		},
	},
	{
		name: "Test with mixed delimiters, more = than :",
		line: "key1=value1 key2:value2 key3=value3",
		want: map[string][]string{
			typePunctuation: {"key1", "=", "value1", "key2", ":", "value2", "key3", "=", "value3"},
			typeSplitting:   {"key1=", "value1", "key2:value2", "key3=", "value3"},
		},
	},
	{
		name: "Test with mixed delimiters, more : than =",
		line: "key1:value1 key2:value2 key3=value3",
		want: map[string][]string{
			typePunctuation: {"key1", ":", "value1", "key2", ":", "value2", "key3", "=", "value3"},
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
			typePunctuation: {`09`, `:`, `17`, `:`, `38`, `.`, `033366`, `▶`, `INFO`, `route`, `ops`, `sending`, `to`, `dest`, `https`, `:`, `/`, `/`, `graphite-cortex-ops-blocks-us-east4`, `.`, `grafana`, `.`, `net`, `/`, `graphite`, `/`, `metrics`, `:`, `service_is_carbon-relay-ng`, `.`, `instance_is_carbon-relay-ng-c665b7b-j2trk`, `.`, `mtype_is_counter`, `.`, `dest_is_https_graphite-cortex-ops-blocks-us-east4_grafana_netgraphitemetrics`, `.`, `unit_is_Metric`, `.`, `action_is_drop`, `.`, `reason_is_queue_full`, `0`, `1717060658`},
			typeSplitting:   {`09:`, `17:`, `38.033366`, `▶`, `INFO`, ``, `route`, `ops`, `sending`, `to`, `dest`, `https:`, `//graphite-cortex-ops-blocks-us-east4.grafana.net/graphite/metrics:`, ``, `service_is_carbon-relay-ng.instance_is_carbon-relay-ng-c665b7b-j2trk.mtype_is_counter.dest_is_https_graphite-cortex-ops-blocks-us-east4_grafana_netgraphitemetrics.unit_is_Metric.action_is_drop.reason_is_queue_full`, `0`, `1717060658`},
		},
	},
	{
		name: "Consecutive splits points: equals followed by space",
		line: `ts=2024-05-30T12:50:36.648377186Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
		want: map[string][]string{
			typePunctuation: {`ts`, `=`, `2024-05-30T12`, `:`, `50`, `:`, `36`, `.`, `648377186Z`, `caller`, `=`, `scheduler_processor`, `.`, `go`, `:`, `143`, `level`, `=`, `warn`, `msg`, `=`, `"`, `error`, `contacting`, `scheduler`, `"`, `err`, `=`, `"`, `rpc`, `error`, `:`, `code`, `=`, `Unavailable`, `desc`, `=`, `connection`, `error`, `:`, `desc`, `=`, `\`, `"`, `error`, `reading`, `server`, `preface`, `:`, `EOF`, `\`, `"`, `"`, `addr`, `=`, `10`, `.`, `0`, `.`, `151`, `.`, `101`, `:`, `9095`},
			typeSplitting:   {"ts=", "2024-05-30T12:50:36.648377186Z", "caller=", "scheduler_processor.go:143", "level=", "warn", "msg=", "\"error", "contacting", "scheduler\"", "err=", "\"rpc", "error:", "code", "=", ``, "Unavailable", "desc", "=", ``, "connection", "error:", "desc", "=", ``, `\"error`, "reading", "server", "preface:", `EOF\""`, "addr=", "10.0.151.101:9095"},
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

func TestLogFmtTokenizer(t *testing.T) {
	param := DefaultConfig().ParamString
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			line: `foo=bar baz="this is a message"`,
			want: []string{"foo", "bar", "baz", "this is a message"},
		},
		{
			line: `foo baz="this is a message"`,
			want: []string{"foo", "", "baz", "this is a message"},
		},
		{
			line: `foo= baz="this is a message"`,
			want: []string{"foo", "", "baz", "this is a message"},
		},
		{
			line: `foo baz`,
			want: []string{"foo", "", "baz", ""},
		},
		{
			line: `ts=2024-05-30T12:50:36.648377186Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
			want: []string{"ts", param, "caller", "scheduler_processor.go:143", "level", "warn", "msg", "error contacting scheduler", "err", "rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"", "addr", "10.0.151.101:9095"},
		},
	}

	tokenizer := newLogfmtTokenizer(param)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := tokenizer.Tokenize(tt.line)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLogFmtTokenizerJoin(t *testing.T) {
	tests := []struct {
		tokens []string
		want   string
	}{
		{
			want:   ``,
			tokens: []string{},
		},
		{
			want:   `foo=bar baz="this is a message"`,
			tokens: []string{"foo", "bar", "baz", "this is a message"},
		},
		{
			want:   `foo= baz="this is a message"`,
			tokens: []string{"foo", "", "baz", "this is a message"},
		},
		{
			want:   `foo= baz="this is a message"`,
			tokens: []string{"foo", "", "baz", "this is a message"},
		},
		{
			want:   `foo= baz=`,
			tokens: []string{"foo", "", "baz", ""},
		},
		{
			want:   `foo=`,
			tokens: []string{"foo"},
		},
		{
			want:   `foo= bar=`,
			tokens: []string{"foo", "", "bar"},
		},
		{
			want:   `ts=2024-05-30T12:50:36.648377186Z caller=scheduler_processor.go:143 level=warn msg="error contacting scheduler" err="rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"" addr=10.0.151.101:9095`,
			tokens: []string{"ts", "2024-05-30T12:50:36.648377186Z", "caller", "scheduler_processor.go:143", "level", "warn", "msg", "error contacting scheduler", "err", "rpc error: code = Unavailable desc = connection error: desc = \"error reading server preface: EOF\"", "addr", "10.0.151.101:9095"},
		},
	}

	tokenizer := newLogfmtTokenizer("")

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := tokenizer.Join(tt.tokens, nil)
			require.Equal(t, tt.want, got)
		})
	}
}
