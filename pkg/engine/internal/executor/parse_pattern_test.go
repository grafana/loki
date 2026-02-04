package executor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatternParser_Process(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		line    string
		want    map[string]string
	}{
		{
			name:    "single named capture",
			pattern: "foo <foo> bar",
			line:    "foo buzz bar",
			want:    map[string]string{"foo": "buzz"},
		},
		{
			name:    "multiple named captures",
			pattern: "foo <first> bar <second> baz",
			line:    "foo hello bar world baz",
			want:    map[string]string{"first": "hello", "second": "world"},
		},
		{
			name:    "capture at beginning",
			pattern: "<start> bar baz",
			line:    "hello bar baz",
			want:    map[string]string{"start": "hello"},
		},
		{
			name:    "capture at end",
			pattern: "foo bar <end>",
			line:    "foo bar baz",
			want:    map[string]string{"end": "baz"},
		},
		{
			name:    "unnamed captures are not included",
			pattern: "<path>?<_>",
			line:    "/api/plugins/versioncheck?slugIn=test&version=1.0",
			want:    map[string]string{"path": "/api/plugins/versioncheck"},
		},
		{
			name:    "mixed named and unnamed captures",
			pattern: "<_> <level> <_> <message>",
			line:    "2023-01-01 INFO app Starting server",
			want:    map[string]string{"level": "INFO", "message": "Starting server"},
		},
		{
			name:    "non-matching line returns empty map",
			pattern: "foo <foo> bar",
			line:    "this does not match",
			want:    map[string]string{},
		},
		{
			name:    "empty line returns empty map",
			pattern: "foo <foo> bar",
			line:    "",
			want:    map[string]string{},
		},
		{
			name:    "partial match extracts available fields",
			pattern: "foo <foo> bar<fuzz>",
			line:    "foo buzz bar",
			want:    map[string]string{"foo": "buzz", "fuzz": ""},
		},
		{
			name:    "partial match with missing literal",
			pattern: "<path>?<params>",
			line:    "/api/plugins/status",
			want:    map[string]string{"path": "/api/plugins/status"},
		},
		{
			name:    "common log format",
			pattern: `<ip> <userid> <user> [<_>] "<method> <path> <_>" <status> <size>`,
			line:    `127.0.0.1 user-identifier frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
			want: map[string]string{
				"ip":     "127.0.0.1",
				"userid": "user-identifier",
				"user":   "frank",
				"method": "GET",
				"path":   "/apache_pb.gif",
				"status": "200",
				"size":   "2326",
			},
		},
		{
			name:    "unicode emoji capture",
			pattern: "unicode <emoji> character",
			line:    "unicode 🤷 character",
			want:    map[string]string{"emoji": "🤷"},
		},
		{
			name:    "unicode literal in pattern",
			pattern: "unicode ▶ <what>",
			line:    "unicode ▶ character",
			want:    map[string]string{"what": "character"},
		},
		{
			name:    "unicode value in capture",
			pattern: "user=<user> level=<level>",
			line:    "user=José level=信息",
			want:    map[string]string{"user": "José", "level": "信息"},
		},
		{
			name:    "mysql log format",
			pattern: "<_> <id> [<level>] [<no>] [<component>] ",
			line:    "2020-08-06T14:25:02.835618Z 0 [Note] [MY-012487] [InnoDB] DDL log recovery : begin",
			want:    map[string]string{"id": "0", "level": "Note", "no": "MY-012487", "component": "InnoDB"},
		},
		{
			name:    "cortex distributor log format",
			pattern: `<_> msg="<method> <path> (<status>) <duration>"`,
			line:    `level=debug ts=2021-05-19T07:54:26.864644382Z caller=logging.go:66 traceID=7fbb92fd0eb9c65d msg="POST /loki/api/v1/push (204) 1.238734ms"`,
			want:    map[string]string{"method": "POST", "path": "/loki/api/v1/push", "status": "204", "duration": "1.238734ms"},
		},
		{
			name:    "etcd log format",
			pattern: `<_> <_> <level> | <component>: <_> peer <peer_id> <_> tcp <ip>:<_>`,
			line:    `2021-05-19 08:16:50.181436 W | rafthttp: health check for peer fd8275e521cfb532 could not connect: dial tcp 10.32.85.85:2380: connect: connection refused`,
			want:    map[string]string{"level": "W", "component": "rafthttp", "peer_id": "fd8275e521cfb532", "ip": "10.32.85.85"},
		},
		{
			name:    "kafka log format",
			pattern: `<_>] <level> [Log partition=<part>, dir=<dir>] `,
			line:    `[2021-05-19 08:35:28,681] INFO [Log partition=p-636-L-fs-117, dir=/data/kafka-logs] Deleting segment 455976081 (kafka.log.Log)`,
			want:    map[string]string{"level": "INFO", "part": "p-636-L-fs-117", "dir": "/data/kafka-logs"},
		},
		{
			name:    "envoy log format",
			pattern: `<_> "<method> <path> <_>" <status> <_> <received_bytes> <sent_bytes> <duration> <upstream_time> "<forward_for>" "<agent>" <_> <_> "<upstream>"`,
			line:    `[2016-04-15T20:17:00.310Z] "POST /api/v1/locations HTTP/2" 204 - 154 0 226 100 "10.0.35.28" "nsq2http" "cc21d9b0-cf5c-432b-8c7e-98aeb7988cd2" "locations" "tcp://10.0.2.1:80"`,
			want: map[string]string{
				"method":         "POST",
				"path":           "/api/v1/locations",
				"status":         "204",
				"received_bytes": "154",
				"sent_bytes":     "0",
				"duration":       "226",
				"upstream_time":  "100",
				"forward_for":    "10.0.35.28",
				"agent":          "nsq2http",
				"upstream":       "tcp://10.0.2.1:80",
			},
		},
		{
			name:    "single capture for entire line",
			pattern: "<all>",
			line:    "the entire line content",
			want:    map[string]string{"all": "the entire line content"},
		},
		{
			name:    "whitespace handling in captures",
			pattern: "<foo>bar<baz>",
			line:    " bar ",
			want:    map[string]string{"foo": " ", "baz": " "},
		},
		{
			name:    "capture with empty result due to adjacent literals",
			pattern: "<foo> bar<baz>",
			line:    " bar ",
			want:    map[string]string{"foo": "", "baz": " "},
		},
		{
			name:    "special characters in literals",
			pattern: `[<date>] <level>: <message>`,
			line:    `[2023-01-01] ERROR: something went wrong`,
			want:    map[string]string{"date": "2023-01-01", "level": "ERROR", "message": "something went wrong"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := newPatternParser(tt.pattern)
			require.NoError(t, err)

			result, err := parser.process(tt.line)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestPatternParser_InvalidExpression(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		errContains string
	}{
		{
			name:        "empty pattern",
			pattern:     "",
			errContains: "syntax error",
		},
		{
			name:        "only unnamed captures",
			pattern:     "<_>",
			errContains: "at least one capture is required",
		},
		{
			name:        "multiple unnamed captures only",
			pattern:     "foo <_> bar <_>",
			errContains: "at least one capture is required",
		},
		{
			name:        "no captures at all",
			pattern:     "foo bar buzz",
			errContains: "at least one capture is required",
		},
		{
			name:        "consecutive captures",
			pattern:     "<f><f>",
			errContains: "consecutive capture",
		},
		{
			name:        "consecutive captures mixed",
			pattern:     "<f> f<d><b>",
			errContains: "consecutive capture",
		},
		{
			name:        "duplicate capture names",
			pattern:     "<f> f<f>",
			errContains: "duplicate capture name",
		},
		{
			name:        "consecutive named and unnamed",
			pattern:     "f<f><_>",
			errContains: "consecutive capture",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newPatternParser(tt.pattern)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestPatternParser_ValidExpression(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		names   []string
	}{
		{
			name:    "single capture",
			pattern: "<f>",
			names:   []string{"f"},
		},
		{
			name:    "multiple captures",
			pattern: "<f> <a>",
			names:   []string{"f", "a"},
		},
		{
			name:    "named with unnamed",
			pattern: "<f> <_> <a>",
			names:   []string{"f", "a"},
		},
		{
			name:    "complex pattern",
			pattern: `<ip> - - [<_>] "<method> <path> <_>" <status> <size>`,
			names:   []string{"ip", "method", "path", "status", "size"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := newPatternParser(tt.pattern)
			require.NoError(t, err)
			require.NotNil(t, parser)
			require.Equal(t, tt.names, parser.names)
		})
	}
}

func TestPatternParser_MultipleRows(t *testing.T) {
	// Test that parsing multiple lines with the same parser works correctly
	pattern := "level=<level> msg=<message>"
	parser, err := newPatternParser(pattern)
	require.NoError(t, err)

	lines := []struct {
		line string
		want map[string]string
	}{
		{"level=INFO msg=Starting server", map[string]string{"level": "INFO", "message": "Starting server"}},
		{"level=DEBUG msg=Processing request", map[string]string{"level": "DEBUG", "message": "Processing request"}},
		{"level=ERROR msg=Failed to connect", map[string]string{"level": "ERROR", "message": "Failed to connect"}},
		{"level=WARN msg=Low memory", map[string]string{"level": "WARN", "message": "Low memory"}},
		{"", map[string]string{}},                          // empty line returns empty map
		{"no match here", map[string]string{}},             // no match (doesn't start with "level=")
		{"level=INFO", map[string]string{"level": "INFO"}}, // partial match (missing "msg=")
	}

	for _, tt := range lines {
		result, err := parser.process(tt.line)
		require.NoError(t, err)
		require.Equal(t, tt.want, result)
	}
}

func BenchmarkPatternParser_Process(b *testing.B) {
	testCases := []struct {
		name    string
		pattern string
		line    string
	}{
		{
			name:    "simple",
			pattern: "foo <foo> bar",
			line:    "foo buzz bar",
		},
		{
			name:    "common_log_format",
			pattern: `<ip> <userid> <user> [<_>] "<method> <path> <_>" <status> <size>`,
			line:    `127.0.0.1 user-identifier frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
		},
		{
			name:    "envoy_log_format",
			pattern: `<_> "<method> <path> <_>" <status> <_> <received_bytes> <sent_bytes> <duration> <upstream_time> "<forward_for>" "<agent>" <_> <_> "<upstream>"`,
			line:    `[2016-04-15T20:17:00.310Z] "POST /api/v1/locations HTTP/2" 204 - 154 0 226 100 "10.0.35.28" "nsq2http" "cc21d9b0-cf5c-432b-8c7e-98aeb7988cd2" "locations" "tcp://10.0.2.1:80"`,
		},
		{
			name:    "no_match",
			pattern: "foo <foo> bar",
			line:    "this line does not match the pattern",
		},
		{
			name:    "unicode",
			pattern: "user=<user> level=<level> msg=<msg>",
			line:    "user=José level=信息 msg=こんにちは世界",
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			parser, err := newPatternParser(tc.pattern)
			if err != nil {
				b.Fatal(err)
			}

			for b.Loop() {
				_, err := parser.process(tc.line)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
