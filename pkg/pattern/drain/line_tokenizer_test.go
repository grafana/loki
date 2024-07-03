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

func TestJsonTokenizer(t *testing.T) {
	param := DefaultConfig().ParamString
	tests := []struct {
		name    string
		line    string
		pattern string
		want    []string
	}{
		{
			line:    `{"level":30,"time":1719998371869,"pid":17,"hostname":"otel-demo-ops-paymentservice-7c759bf575-55t4p","trace_id":"1425c6df5a4321cf6a7de254de5b8204","span_id":"2ac7a3fc800b80d4","trace_flags":"01","transactionId":"e501032b-3215-4e43-b1db-f4886a906fc5","cardType":"visa","lastFourDigits":"5647","amount":{"units":{"low":656,"high":0,"unsigned":false},"nanos":549999996,"currencyCode":"USD"},"msg":"Transaction complete."}`,
			want:    []string{"Transaction", "complete", "."},
			pattern: "<_>Transaction complete.<_>",
		},
		{
			line:    `{"event":{"actor":{"alternateId":"foo@grafana.com","displayName":"Foo bar","id":"dq23","type":"User"},"authenticationContext":{"authenticationStep":0,"externalSessionId":"123d"},"client":{"device":"Computer","geographicalContext":{"city":"Berlin","country":"DE","state":"Land Berlin"},"ipAddress":"0.0.0.0","userAgent":{"browser":"CHROME","os":"Mac OS X","rawUserAgent":"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"},"zone":"null"},"debugContext":{"debugData":{"authMethodFirstEnrollment":"123","authMethodFirstType":"foo","authMethodFirstVerificationTime":"2024-07-02T11:28:03.219Z","authMethodSecondEnrollment":"var","authMethodSecondType":"ddd","authMethodSecondVerificationTime":"2024-07-03T06:59:09.151Z","authnRequestId":"1","dtHash":"1","logOnlySecurityData":"{\"risk\":{\"level\":\"LOW\"},\"behaviors\":{\"New Geo-Location\":\"NEGATIVE\",\"New Device\":\"NEGATIVE\",\"New IP\":\"NEGATIVE\",\"New State\":\"NEGATIVE\",\"New Country\":\"NEGATIVE\",\"Velocity\":\"NEGATIVE\",\"New City\":\"NEGATIVE\"}}","requestId":"1","threatSuspected":"false","url":"/foo?"}},"displayMessage":"Evaluation of sign-on policy","eventType":"policy.evaluate_sign_on","legacyEventType":"app.oauth2.token.grant.refresh_token_success","outcome":{"reason":"Sign-on policy evaluation resulted in AUTHENTICATED","result":"ALLOW"},"published":"2024-07-03T09:19:59.973Z","request":{"ipChain":[{"geographicalContext":{"city":"Berlin","country":"Germany","geolocation":{"lat":52.5363,"lon":13.4169},"postalCode":"10435","state":"Land Berlin"},"ip":"95.90.234.241","version":"V4"}]},"securityContext":{"asNumber":3209,"asOrg":"kabel deutschland breitband customer 19","domain":"kabel-deutschland.de","isProxy":false,"isp":"vodafone gmbh"},"severity":"INFO","target":[{"alternateId":"Salesforce.com","detailEntry":{"signOnModeEvaluationResult":"AUTHENTICATED","signOnModeType":"SAML_2_0"},"displayName":"Salesforce.com","id":"0oa5sfmj3hz0mTgoW357","type":"AppInstance"},{"alternateId":"unknown","detailEntry":{"policyRuleFactorMode":"2FA"},"displayName":"Catch-all Rule","id":"1","type":"Rule"}],"transaction":{"detail":{},"id":"1","type":"WEB"},"uuid":"1","version":"0"},"level":"info","msg":"received event","time":"2024-07-03T09:19:59Z"}`,
			want:    []string{"received", "event"},
			pattern: "<_>received event<_>",
		},
		{
			line:    `{"code":200,"message":"OK","data":{"id":"1","name":"foo"}}`,
			want:    []string{"OK"},
			pattern: "<_>OK<_>",
		},
	}

	tokenizer := newJSONTokenizer(param)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, state := tokenizer.Tokenize(tt.line)
			require.Equal(t, tt.want, got)
			pattern := tokenizer.Join(got, state)
			require.Equal(t, tt.pattern, pattern)
		})
	}
}
