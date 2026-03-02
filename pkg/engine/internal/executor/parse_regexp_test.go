package executor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegexpParser_Process(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		pattern string
		want    map[string]string
	}{
		{
			"single named capture group",
			`level=info msg=hello`,
			`level=(?P<level>\w+)`,
			map[string]string{"level": "info"},
		},
		{
			"multiple named capture groups",
			`2023-01-01 INFO this is a message`,
			`(?P<timestamp>\d{4}-\d{2}-\d{2}) (?P<level>\w+) (?P<message>.*)`,
			map[string]string{"timestamp": "2023-01-01", "level": "INFO", "message": "this is a message"},
		},
		{
			"pattern with special characters",
			`[2023-01-01] ERROR: something went wrong`,
			`\[(?P<date>[^\]]+)\] (?P<level>\w+): (?P<message>.*)`,
			map[string]string{"date": "2023-01-01", "level": "ERROR", "message": "something went wrong"},
		},
		{
			"partial match returns matched groups",
			`prefix level=warn suffix`,
			`level=(?P<level>\w+)`,
			map[string]string{"level": "warn"},
		},
		{
			"no match returns empty",
			`this line does not match`,
			`level=(?P<level>\w+)`,
			map[string]string{},
		},
		{
			"ip address and port extraction",
			`connection from 192.168.1.100:8080 established`,
			`(?P<ip>\d+\.\d+\.\d+\.\d+):(?P<port>\d+)`,
			map[string]string{"ip": "192.168.1.100", "port": "8080"},
		},
		{
			"log format with json-like structure",
			`{"time":"2023-01-01","level":"info","msg":"test"}`,
			`"level":"(?P<level>[^"]+)".*"msg":"(?P<message>[^"]+)"`,
			map[string]string{"level": "info", "message": "test"},
		},
		{
			"apache common log format",
			`127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
			`(?P<ip>[\d.]+) - (?P<user>\S+) \[(?P<timestamp>[^\]]+)\] "(?P<method>\w+) (?P<path>[^"]+)" (?P<status>\d+) (?P<size>\d+)`,
			map[string]string{
				"ip":        "127.0.0.1",
				"user":      "frank",
				"timestamp": "10/Oct/2000:13:55:36 -0700",
				"method":    "GET",
				"path":      "/apache_pb.gif HTTP/1.0",
				"status":    "200",
				"size":      "2326",
			},
		},
		{
			"empty capture group value",
			`key= value=test`,
			`key=(?P<key>\w*) value=(?P<value>\w+)`,
			map[string]string{"key": "", "value": "test"},
		},
		{
			"unicode in log line",
			`user=José level=信息 msg=こんにちは`,
			`user=(?P<user>\S+) level=(?P<level>\S+)`,
			map[string]string{"user": "José", "level": "信息"},
		},
		{
			"nested groups - only named captured",
			`error: [code:123] message`,
			`error: \[code:(?P<code>\d+)\]`,
			map[string]string{"code": "123"},
		},
		{
			"multiple occurrences - first match only",
			`level=info level=warn level=error`,
			`level=(?P<level>\w+)`,
			map[string]string{"level": "info"},
		},
		{
			"case insensitive matching",
			`LEVEL=INFO msg=test`,
			`(?i)level=(?P<level>\w+)`,
			map[string]string{"level": "INFO"},
		},
		{
			"multiline content in single entry",
			"first line\nsecond line\nlevel=debug",
			`level=(?P<level>\w+)`,
			map[string]string{"level": "debug"},
		},
		{
			"escaped characters in log",
			`path="/var/log/app.log" level=info`,
			`path="(?P<path>[^"]+)" level=(?P<level>\w+)`,
			map[string]string{"path": "/var/log/app.log", "level": "info"},
		},
		{
			"empty line returns empty",
			``,
			`level=(?P<level>\w+)`,
			map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := newRegexpParser(tt.pattern)
			require.NoError(t, err)

			result, err := parser.process(tt.line)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestRegexpParser_NoNamedCaptures(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"no capture groups", `level=\w+`},
		{"only unnamed capture groups", `level=(\w+) msg=(\w+)`},
		{"mixed but no named", `(level)=(\w+)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRegexpParser(tt.pattern)
			require.Error(t, err)
			require.Contains(t, err.Error(), "at least one named capture must be supplied")
		})
	}
}

func TestRegexpParser_InvalidPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"unclosed group", `(?P<level>\w+`},
		{"invalid escape", `(?P<level>[\`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newRegexpParser(tt.pattern)
			require.Error(t, err)
		})
	}
}

func BenchmarkRegexpParser_Process(b *testing.B) {
	testCases := []struct {
		name    string
		line    string
		pattern string
	}{
		{
			"simple",
			`level=info msg=test user=admin`,
			`level=(?P<level>\w+) msg=(?P<msg>\w+)`,
		},
		{
			"complex_log_format",
			`127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
			`(?P<ip>[\d.]+) - (?P<user>\S+) \[(?P<timestamp>[^\]]+)\] "(?P<method>\w+) (?P<path>[^"]+)" (?P<status>\d+) (?P<size>\d+)`,
		},
		{
			"many_capture_groups",
			`a=1 b=2 c=3 d=4 e=5 f=6 g=7 h=8`,
			`a=(?P<a>\d+) b=(?P<b>\d+) c=(?P<c>\d+) d=(?P<d>\d+) e=(?P<e>\d+) f=(?P<f>\d+) g=(?P<g>\d+) h=(?P<h>\d+)`,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			parser, err := newRegexpParser(tc.pattern)
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
