package pattern

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixtures = []struct {
	expr     string
	in       string
	expected []string
	matches  bool
}{
	{
		"foo <foo> bar",
		"foo buzz bar",
		[]string{"buzz"},
		true,
	},
	{
		"foo <foo> bar<fuzz>",
		"foo buzz bar",
		[]string{"buzz", ""},
		false,
	},
	{
		"<foo>foo <bar> bar",
		"foo buzz bar",
		[]string{"", "buzz"},
		false,
	},
	{
		"<foo> bar<fuzz>",
		" bar",
		[]string{"", ""},
		false,
	},
	{
		"<foo>bar<baz>",
		" bar ",
		[]string{" ", " "},
		true,
	},
	{
		"<foo> bar<baz>",
		" bar ",
		[]string{"", " "},
		false,
	},
	{
		"<foo>bar <baz>",
		" bar ",
		[]string{" ", ""},
		false,
	},
	{
		"<foo>",
		" bar ",
		[]string{" bar "},
		true,
	},
	{
		"<path>?<_>",
		`/api/plugins/versioncheck?slugIn=snuids-trafficlights-panel,input,gel&grafanaVersion=7.0.0-beta1`,
		[]string{"/api/plugins/versioncheck"},
		true,
	},
	{
		"<path>?<_>",
		`/api/plugins/status`,
		[]string{"/api/plugins/status"},
		false,
	},
	{
		// Common Log Format
		`<ip> <userid> <user> [<_>] "<method> <path> <_>" <status> <size>`,
		`127.0.0.1 user-identifier frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
		[]string{"127.0.0.1", "user-identifier", "frank", "GET", "/apache_pb.gif", "200", "2326"},
		true,
	},
	{
		// Combined Log Format
		`<ip> - - [<_>] "<method> <path> <_>" <status> <size> `,
		`35.191.8.106 - - [19/May/2021:07:21:49 +0000] "GET /api/plugins/versioncheck?slugIn=snuids-trafficlights-panel,input,gel&grafanaVersion=7.0.0-beta1 HTTP/1.1" 200 107 "-" "Go-http-client/2.0" "80.153.74.144, 34.120.177.193" "TLSv1.3" "DE" "DEBW"`,
		[]string{"35.191.8.106", "GET", "/api/plugins/versioncheck?slugIn=snuids-trafficlights-panel,input,gel&grafanaVersion=7.0.0-beta1", "200", "107"},
		false,
	},
	{
		// MySQL
		`<_> <id> [<level>] [<no>] [<component>] `,
		`2020-08-06T14:25:02.835618Z 0 [Note] [MY-012487] [InnoDB] DDL log recovery : begin`,
		[]string{"0", "Note", "MY-012487", "InnoDB"},
		false,
	},
	{
		// MySQL
		`<_> <id> [<level>] `,
		`2021-05-19T07:40:12.215792Z 42761518 [Note] Aborted connection 42761518 to db: 'hosted_grafana' user: 'hosted_grafana' host: '10.36.4.122' (Got an error reading communication packets)`,
		[]string{"42761518", "Note"},
		false,
	},
	{
		// Kubernetes api-server
		`<id> <_>       <_> <line>] `,
		`W0519 07:46:47.647050       1 clientconn.go:1223] grpc: addrConn.createTransport failed to connect to {https://kubernetes-etcd-1.kubernetes-etcd:2379  <nil> 0 <nil>}. Err :connection error: desc = "transport: Error while dialing dial tcp 10.32.85.85:2379: connect: connection refused". Reconnecting...`,
		[]string{"W0519", "clientconn.go:1223"},
		false,
	},
	{
		// Cassandra
		`<level>  [<component>]<_> in <duration>.<_>`,
		`INFO  [Service Thread] 2021-05-19 07:40:12,130 GCInspector.java:284 - ParNew GC in 248ms.  CMS Old Gen: 5043436640 -> 5091062064; Par Eden Space: 671088640 -> 0; Par Survivor Space: 70188280 -> 60139760`,
		[]string{"INFO", "Service Thread", "248ms"},
		true,
	},
	{
		// Cortex & Loki distributor
		`<_> msg="<method> <path> (<status>) <duration>"`,
		`level=debug ts=2021-05-19T07:54:26.864644382Z caller=logging.go:66 traceID=7fbb92fd0eb9c65d msg="POST /loki/api/v1/push (204) 1.238734ms"`,
		[]string{"POST", "/loki/api/v1/push", "204", "1.238734ms"},
		true,
	},
	{
		// Etcd
		`<_> <_> <level> | <component>: <_> peer <peer_id> <_> tcp <ip>:<_>`,
		`2021-05-19 08:16:50.181436 W | rafthttp: health check for peer fd8275e521cfb532 could not connect: dial tcp 10.32.85.85:2380: connect: connection refused`,
		[]string{"W", "rafthttp", "fd8275e521cfb532", "10.32.85.85"},
		true,
	},
	{
		// Kafka
		`<_>] <level> [Log partition=<part>, dir=<dir>] `,
		`[2021-05-19 08:35:28,681] INFO [Log partition=p-636-L-fs-117, dir=/data/kafka-logs] Deleting segment 455976081 (kafka.log.Log)`,
		[]string{"INFO", "p-636-L-fs-117", "/data/kafka-logs"},
		false,
	},
	{
		// Elastic
		`<_>][<level>][<component>] [<id>] [<index>]`,
		`[2021-05-19T06:54:06,994][INFO ][o.e.c.m.MetaDataMappingService] [1f605d47-8454-4bfb-a67f-49f318bf837a] [usage-stats-2021.05.19/O2Je9IbmR8CqFyUvNpTttA] update_mapping [report]`,
		[]string{"INFO ", "o.e.c.m.MetaDataMappingService", "1f605d47-8454-4bfb-a67f-49f318bf837a", "usage-stats-2021.05.19/O2Je9IbmR8CqFyUvNpTttA"},
		false,
	},
	{
		// Envoy
		`<_> "<method> <path> <_>" <status> <_> <received_bytes> <sent_bytes> <duration> <upstream_time> "<forward_for>" "<agent>" <_> <_> "<upstream>"`,
		`[2016-04-15T20:17:00.310Z] "POST /api/v1/locations HTTP/2" 204 - 154 0 226 100 "10.0.35.28" "nsq2http" "cc21d9b0-cf5c-432b-8c7e-98aeb7988cd2" "locations" "tcp://10.0.2.1:80"`,
		[]string{"POST", "/api/v1/locations", "204", "154", "0", "226", "100", "10.0.35.28", "nsq2http", "tcp://10.0.2.1:80"},
		true,
	},
	{
		// UTF-8: Matches a unicode character
		`unicode <emoji> character`,
		`unicode 🤷 character`,
		[]string{`🤷`},
		true,
	},
	{
		// UTF-8: Parses unicode character as literal
		"unicode ▶ <what>",
		"unicode ▶ character",
		[]string{"character"},
		true,
	},
}

func Test_BytesIndexUnicode(t *testing.T) {
	data := []byte("Hello ▶ World")
	index := bytes.Index(data, []byte("▶"))
	require.Equal(t, 6, index)
}

func Test_matcher_Matches(t *testing.T) {
	for _, tt := range fixtures {
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()
			m, err := New(tt.expr)
			require.NoError(t, err)
			line := []byte(tt.in)
			assert.Equal(t, tt.matches, m.Test(line))
			actual := m.Matches(line)
			var actualStrings []string
			for _, a := range actual {
				actualStrings = append(actualStrings, string(a))
			}
			assert.Equal(t, tt.expected, actualStrings)
		})
	}
}

var res [][]byte

func Benchmark_matcher_Matches(b *testing.B) {
	for _, tt := range fixtures {
		b.Run(tt.expr, func(b *testing.B) {
			b.ReportAllocs()
			m, err := New(tt.expr)
			require.NoError(b, err)
			b.ResetTimer()
			l := []byte(tt.in)
			for n := 0; n < b.N; n++ {
				res = m.Matches(l)
			}
		})
	}
}

func Test_Error(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"<f>", nil},
		{"<f> <a>", nil},
		{"", newParseError("syntax error: unexpected $end, expecting IDENTIFIER or LITERAL", 1, 1)},
		{"<_>", ErrNoCapture},
		{"foo <_> bar <_>", ErrNoCapture},
		{"foo bar buzz", ErrNoCapture},
		{"<f><f>", fmt.Errorf("found consecutive capture '<f><f>': %w", ErrInvalidExpr)},
		{"<f> f<d><b>", fmt.Errorf("found consecutive capture '<d><b>': %w", ErrInvalidExpr)},
		{"<f> f<f>", fmt.Errorf("duplicate capture name (f): %w", ErrInvalidExpr)},
		{`f<f><_>`, fmt.Errorf("found consecutive capture '<f><_>': %w", ErrInvalidExpr)},
		{`<f>f<f><_>`, fmt.Errorf("found consecutive capture '<f><_>': %w", ErrInvalidExpr)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.name)
			require.Equal(t, tt.err, err)
		})
	}
}

func Test_ParseLineFilter(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"<_>", nil}, // Meaningless, but valid: matches everything.
		{"", nil},    // Empty pattern matches empty lines.
		{"foo <_> bar <_>", nil},
		{"<foo <foo> bar <_>", fmt.Errorf("%w: found '<foo>'", ErrCaptureNotAllowed)},
		{"<foo>", fmt.Errorf("%w: found '<foo>'", ErrCaptureNotAllowed)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseLineFilter([]byte(tt.name))
			require.Equal(t, tt.err, err)
		})
	}
}

func Test_ParseLiterals(t *testing.T) {
	for _, tt := range []struct {
		pattern string
		lit     [][]byte
		err     error
	}{
		{"<_>", [][]byte{}, nil},
		{"", nil, newParseError("syntax error: unexpected $end, expecting IDENTIFIER or LITERAL", 1, 1)},
		{"foo <_> bar <_>", [][]byte{[]byte("foo "), []byte(" bar ")}, nil},
		{"<foo>", [][]byte{}, nil},
	} {
		t.Run(tt.pattern, func(t *testing.T) {
			lit, err := ParseLiterals(tt.pattern)
			require.Equal(t, tt.err, err)
			require.Equal(t, tt.lit, lit)
		})
	}
}
