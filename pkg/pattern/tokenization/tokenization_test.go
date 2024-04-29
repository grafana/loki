package tokenization

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokenizationTestCase struct {
	line      string
	expResult []string
}

var tokenizationCornerTestCases = []tokenizationTestCase{
	{
		"",
		[]string{},
	},
	{
		" foo  ",
		[]string{"foo"},
	},
	{
		"foo bar baz",
		[]string{"foo", "bar", "baz"},
	},
	{
		"\nfoo\t  bar baz\r\n",
		// TOFIX: remove empty tokens?
		[]string{"foo", "", "", "bar", "baz"},
	},
	{
		"ends single char C",
		[]string{"ends", "single", "char", "C"},
	},
	{
		"0 1 2 3 4 5 6 7 8 9 a b c d e f g h i j k l m n o p q r s t u v w x y z A B C D E F G H I J K L M N O P Q R S T U V W X Y Z Z Y X W V U T S R Q P O N M L K J I H G F E D C B A z y x w v u t s r q p o n m l k j i h g f e d c b a 9 8 7 6 5 4 3 2 1 0",
		// The tail end of strings longer than maxTokens is returned as a single token
		[]string{"<NUM>", "<NUM>", "<NUM>", "<NUM>", "<NUM>", "<NUM>", "<NUM>", "<NUM>", "<NUM>", "<NUM>", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z", "Z", "Y", "X", "W", "V", "U", "T", "S", "R", "Q", "P", "O", "N", "M", "L", "K", "J", "I", "H", "G", "F", "E", "D", "C", "B", "A", "z", "y", "x", "w", "v", "u", "t", "s", "r", "q", "p", "<...>"},
	},
	{
		`a "quoted string"`,
		[]string{"a", `"quoted string"`},
	},
	{
		`a "quoted string with \" escapes!"`,
		[]string{"a", `"quoted string with \" escapes!"`},
	},
	{
		`a 'singly quoted string"'`,
		[]string{"a", `'singly quoted string"'`},
	},
	{
		`a 'singly quoted string" \''`,
		[]string{"a", `'singly quoted string" \''`},
	},
	{
		`a 'singly quoted string" \\\''`,
		[]string{"a", `'singly quoted string" \\\''`},
	},
	{
		`a'twisted excappe\\' "with an unterminated quote" 'at_the_end`,
		[]string{`a'twisted excappe\\'`, `"with an unterminated quote"`, `'`, "at_the_end"},
	},
	{
		`a "quoted string 'inception'"!` + "`woot`'much`wow'",
		[]string{"a", `"quoted string 'inception'"!` + "`woot`'much`wow'"},
	},
	{
		`unterminated"quote`,
		[]string{`unterminated"`, `quote`},
	},
	{
		"`mix`" + ` "and" 'match'`,
		[]string{"`mix`", `"and"`, `'match'`},
	},
	{
		"`mix`" + ` "and" 'match'`,
		[]string{"`mix`", `"and"`, `'match'`},
	},
	{
		`{"json":"uninterrupted \"logline\"","foo":"bar"}`,
		[]string{`{"json":`, `"uninterrupted \"logline\""`, `,"foo":`, `"bar"}`},
	},
	{
		`{"weirdo1":`,
		[]string{`{"weirdo1":`},
	},
	{
		// This and the corner case below are not optimal, though they're
		// unlikely to be encountered in the real world, so it's hopefully a
		// nice tradeoff for the performance of not needing a full JSON parser.
		`{"weirdo2":}`,
		[]string{`{"weirdo2":`, `}`},
	},
	{
		`,"weirdo3":}`,
		[]string{`,"weirdo3":`, `}`},
	},
	{
		// We deliberately do treat "escaped" whitespaces outside of quotes as
		// delimiters, i.e. whitespaces outside of strings cannot be escaped.
		`weird\ escape`,
		[]string{`weird\`, `escape`},
	},
	{
		"-3.14-foo 0.0.0.0/24-0.0.0.1-255.255.255.255-256.255.255.255 1337-ber 0.12-ber n0tnumb3er 12faux -123.0.1.123 -123 -1231.11 333. 123.456. 123.45-",
		[]string{"<NUM>-foo", "<IP>/<NUM>-<IP>-<IP>-<NUM>.<NUM>.<NUM>.<NUM>", "<NUM>-ber", "<NUM>-ber", "n0tnumb3er", "12faux", "-<IP>", "<NUM>", "<NUM>", "<NUM>.", "<NUM>.<NUM>.", "<NUM>-"},
	},
	{
		"2022-12-31 12:12:31 3022-12-31 12:12:31-Jul  1 00:21:28",
		[]string{"<TIMESTAMP>", "<NUM>-<NUM>-<NUM>", "<NUM>:<NUM>:<NUM>-<TIMESTAMP>"},
	},
	{
		"2022/12/01 12:12:31 - 2022/13/32 12:12:31",
		[]string{"<TIMESTAMP>", "-", "<NUM>/<NUM>/<NUM>", "<NUM>:<NUM>:<NUM>"},
	},
	{
		"UUIDS: 123e4567-e89b-12d3-a456-426614174000, 550E8400-E29B-41D4-A716-446655440000, -00000000-0000-0000-0000-000000000000, 12345678-dead-beef-1337-000000000000 {c6ad1a63-10b5-460e-ab2c-05c13604539d} ''<A3AE4842-E9AA-4C27-9509-772DB3CC3190>''",
		[]string{"UUIDS:", "<UUID>,", "<UUID>,", "-<UUID>,", "<UUID>", "{<UUID>}", "''<<UUID>>''"},
	},
	// Mixed case UUID and hex strings are ignored, to limit false positives
	{
		"Not UUIDS: 123e4567-E89B-12d3-a456-426614174000, 1234567-dead-beef-1337-00000000000a",
		[]string{"Not", "UUIDS:", "123e4567-E89B-12d3-a456-<NUM>,", "<NUM>-dead-beef-<NUM>-<HEX>"},
	},
	{
		"Hexes: 0x0123456789 0xabcdef0123 deadbeef1337-ABCDEF0123456?0123456789ab:FFFFAAAAFFFF Curses: 0x012345678 dEaDbeef1337 abcdefabcde ABCDEFABCDE 0xASDFASDFASDF abcdef0123456NOT",
		[]string{"Hexes:", "<HEX>", "<HEX>", "<HEX>-<HEX>?<HEX>:<HEX>", "Curses:", "0x012345678", "dEaDbeef1337", "abcdefabcde", "ABCDEFABCDE", "0xASDFASDFASDF", "abcdef0123456NOT"},
	},
	{
		"30546354_3313121680 0_123_456_789 foo_123",
		[]string{"<NUM>_<NUM>", "<NUM>_<NUM>_<NUM>_<NUM>", "foo_<NUM>"},
	},
	{
		`3.31ms/1h2m|-12h2m6.1s 31m "165m2.1s(6h0m12.05us)" -451325.31µs 6m23μs 123h21m3.4124561s/0s/-0.0123ms`,
		[]string{"<DURATION>/<DURATION>|<DURATION>", "<DURATION>", `"<DURATION>(<DURATION>)"`, "<DURATION>", "<DURATION>", "<DURATION>/<DURATION>/<DURATION>"},
	},
	{
		// Invalid duration values
		"3.31.1ms 3h121m3.4124561s 1h0.12s 100usa 0.12msa",
		[]string{"<NUM>.<DURATION>", "3h121m3.<DURATION>", "1h0.<DURATION>", "100usa", "0.12msa"},
	},
	{
		"2Mib 0.12KB-5GB 3.12kb 123Gbps 124mbit:512Tbit",
		[]string{"<BYTESIZE>", "<BYTESIZE>-<BYTESIZE>", "<BYTESIZE>", "<BYTESIZE>", "<BYTESIZE>:<BYTESIZE>"},
	},
	{
		`status=123 status_code:500 status 200 status="-1" status_code:"404" httpStatus=200`,
		[]string{"status=123", "status_code:500", "status", "200", `status="-1"`, `status_code:"404"`, "httpStatus=200"},
	},
	{
		`status_code_foo=123 status_code:500.1 status 2023-09-06T00:59:59.98 status:"404KiB"`,
		[]string{"status_code_foo=<NUM>", "status_code:<NUM>", "status", "<TIMESTAMP>", `status:"<BYTESIZE>"`},
	},
}

// TODO: add these to the benchmark tests
var tokenizationRealisticTestCases = []tokenizationTestCase{
	// logfmt from loki, metrics.go with a lot of number values of various types
	{
		`level=info ts=2023-09-06T00:59:59.982171323Z caller=metrics.go:160 component=frontend org_id=29 traceID=4b93729ff3efabd0 latency=fast query="{stream=\"stdout\",pod=\"loki-canary-nl54q\"} " query_hash=1280418884 query_type=limited range_type=range length=20s start_delta=2h54m30.690801022s end_delta=2h54m10.690801238s step=1s duration=13.926955ms status=200 limit=1000 returned_lines=0 throughput=16MB total_bytes=219kB total_bytes_non_indexed_labels=2.1kB lines_per_second=14935 total_lines=208 post_filter_lines=208 total_entries=41 store_chunks_download_time=1.592805ms queue_time=127µs splits=0 shards=0 chunk_refs_fetch_time=3.599883ms cache_chunk_req=1 cache_chunk_hit=1 cache_chunk_bytes_stored=0 cache_chunk_bytes_fetched=480079 cache_chunk_download_time=1.307396ms cache_index_req=0 cache_index_hit=0 cache_index_download_time=0s cache_stats_results_req=1 cache_stats_results_hit=1 cache_stats_results_download_time=361.913µs cache_result_req=0 cache_result_hit=0 cache_result_download_time=0s token_id=gcom-1234`,

		[]string{
			"level=info", "ts=<TIMESTAMP>", "caller=metrics.go:<NUM>", "component=frontend", "org_id=<NUM>", "traceID=<HEX>", "latency=fast", `query="{stream=\"stdout\",pod=\"loki-canary-nl54q\"} "`, "query_hash=<NUM>", "query_type=limited", "range_type=range", "length=<DURATION>", "start_delta=<DURATION>", "end_delta=<DURATION>", "step=<DURATION>", "duration=<DURATION>", "status=200", "limit=<NUM>", "returned_lines=<NUM>", "throughput=<BYTESIZE>", "total_bytes=<BYTESIZE>", "total_bytes_non_indexed_labels=<BYTESIZE>", "lines_per_second=<NUM>", "total_lines=<NUM>", "post_filter_lines=<NUM>", "total_entries=<NUM>", "store_chunks_download_time=<DURATION>", "queue_time=<DURATION>", "splits=<NUM>", "shards=<NUM>", "chunk_refs_fetch_time=<DURATION>", "cache_chunk_req=<NUM>", "cache_chunk_hit=<NUM>", "cache_chunk_bytes_stored=<NUM>", "cache_chunk_bytes_fetched=<NUM>", "cache_chunk_download_time=<DURATION>", "cache_index_req=<NUM>", "cache_index_hit=<NUM>", "cache_index_download_time=<DURATION>", "cache_stats_results_req=<NUM>", "cache_stats_results_hit=<NUM>", "cache_stats_results_download_time=<DURATION>", "cache_result_req=<NUM>", "cache_result_hit=<NUM>", "cache_result_download_time=<DURATION>", "token_id=gcom-<NUM>",
		},
	},
	// logfmt from loki, with string multi-word messages
	{
		`level=debug ts=2023-09-06T00:59:59.98214402Z caller=shard_resolver.go:114 bytes=205kB chunks=2 streams=2 entries=200 msg="queried index" type=single matchers="{stream=\"stdout\", pod=\"loki-canary-v75j4\"}" duration=9.498885ms from=2023-09-06T00:48:53.138Z through=2023-09-06T00:49:43.138Z length=50s`,
		[]string{
			"level=debug", "ts=<TIMESTAMP>", "caller=shard_resolver.go:<NUM>", "bytes=<BYTESIZE>", "chunks=<NUM>", "streams=<NUM>", "entries=<NUM>", `msg="queried index"`, "type=single", `matchers="{stream=\"stdout\", pod=\"loki-canary-v75j4\"}"`, "duration=<DURATION>", "from=<TIMESTAMP>", "through=<TIMESTAMP>", "length=<DURATION>",
		},
	},
	// random JSON logs
	{
		`{"timestamp": "2022-12-23T12:34:56Z", "level": "debug", "message": "Server starting", "server_id": "abcdefghij", "start_time": "2022-12-23T12:30:00Z"}`,
		[]string{`{"timestamp":`, `"<TIMESTAMP>",`, `"level":`, `"debug",`, `"message":`, `"Server starting",`, `"server_id":`, `"abcdefghij",`, `"start_time":`, `"<TIMESTAMP>"}`},
	},
	{
		// JSON logs without spaces between elements, like how JavaScript's JSON.Stringify() produces:
		`{"context":{"taskId":1},"message":"starting task ID 1","sequence":0,"time":1506776210000,"version":"1.0.0"}`,
		[]string{`{"context":`, `{"taskId":`, `<NUM>}`, `,"message":`, `"starting task ID <NUM>"`, `,"sequence":`, `<NUM>`, `,"time":`, `<NUM>`, `,"version":`, `"<NUM>.<NUM>.<NUM>"}`},
	},
	// Android logs from https://github.com/logpai/logparser/blob/main/data/loghub_2k/Android/Android_2k.log
	{
		// This test case has an unterminated double quote
		//
		// TOFIX:
		//  - timestamp is not correctly detected
		//  - empty "" token?
		`03-17 16:13:40.345  1702 14638 D PowerManagerService: release:lock=166121161, flg=0x0, tag="RILJ_ACK_WL", name=com.android.phone", ws=null, uid=1001, pid=2626`,
		[]string{"<NUM>-<NUM>", "<NUM>:<NUM>:<NUM>", "", "<NUM>", "<NUM>", "D", "PowerManagerService:", "release:lock=<NUM>,", "flg=0x0,", `tag="RILJ_ACK_WL",`, `name=com.android.phone"`, ",", "ws=null,", "uid=<NUM>,", "pid=<NUM>"},
	},
	{
		// TOFIX:
		//  - timestamp is not correctly detected
		//  - empty "" tokens
		`03-17 16:13:47.518  1702  8671 D ActivityManager: Skipping, withExcluded: false, tr.intent:Intent { act=android.intent.action.VIEW dat=file:///storage/emulated/0/Tencent/QQfile_recv/b.apk typ=application/vnd.android.package-archive flg=0x10800000 cmp=com.android.packageinstaller/.PackageInstallerActivity (has extras) }`,
		[]string{
			"<NUM>-<NUM>", "<NUM>:<NUM>:<NUM>", "", "<NUM>", "", "<NUM>", "D", "ActivityManager:", "Skipping,", "withExcluded:", "false,", "tr.intent:Intent", "{", "act=android.intent.action.VIEW", "dat=file:///storage/emulated/<NUM>/Tencent/QQfile_recv/b.apk", "typ=application/vnd.android.package-archive", "flg=0x10800000", "cmp=com.android.packageinstaller/.PackageInstallerActivity", "(has", "extras)", "}",
		},
	},
	// Apache logs from https://github.com/logpai/logparser/blob/main/data/loghub_2k/Apache/Apache_2k.log
	{
		`[Mon Dec 05 13:16:27 2005] [notice] jk2_init() Found child 5877 in scoreboard slot 9`,
		[]string{"[<TIMESTAMP>]", "[notice]", "jk2_init()", "Found", "child", "<NUM>", "in", "scoreboard", "slot", "<NUM>"},
	},
	{
		`[Mon Dec 05 19:14:11 2005] [notice] workerEnv.init() ok /etc/httpd/conf/workers2.properties`,
		[]string{"[<TIMESTAMP>]", "[notice]", "workerEnv.init()", "ok", "/etc/httpd/conf/workers2.properties"},
	},
	// nginx logs by running `docker run -p 80:80 -v $(pwd):/usr/share/nginx/html nginx` locally
	{
		`2024/03/27 14:31:42 [error] 29#29: *1 directory index of "/usr/share/nginx/html/" is forbidden, client: 172.17.0.1, server: localhost, request: "GET / HTTP/1.1", host: "127.0.0.1"`,
		[]string{"<TIMESTAMP>", "[error]", "<NUM>#<NUM>:", "*<NUM>", "directory", "index", "of", "\"/usr/share/nginx/html/\"", "is", "forbidden,", "client:", "<IP>,", "server:", "localhost,", "request:", `"GET / HTTP/<NUM>",`, "host:", "\"<IP>\""},
	},
	{
		// TOFIX:
		//  - probably not all numbers should be replaced with <NUM>, e.g. for "*1", "(2:", "HTTP/1.1" it's definitely a worse UX
		`2024/03/27 14:34:37 [error] 29#29: *1 open() "/usr/share/nginx/html/test url with spaces" failed (2: No such file or directory), client: 172.17.0.1, server: localhost, request: "GET /test%20url%20with%20spaces HTTP/1.1", host: "127.0.0.1"
		172.17.0.1 - - [31/Mar/2024:14:34:37 +0000] "GET /test%20url%20with%20spaces HTTP/1.1" 404 153 "-" "Mozilla/5.0 (X11; Linux x86_64; rv:123.0) Gecko/20100101 Firefox/123.0" "-"`,
		[]string{"<TIMESTAMP>", "[error]", "<NUM>#<NUM>:", "*<NUM>", "open()", `"/usr/share/nginx/html/test url with spaces"`, "failed", "(<NUM>:", "No", "such", "file", "or", "directory),", "client:", "<IP>,", "server:", "localhost,", "request:", `"GET /test%20url%20with%20spaces HTTP/<NUM>",`, "host:", "\"<IP>\"", "", "", "<IP>", "-", "-", "[<TIMESTAMP>]", `"GET /test%20url%20with%20spaces HTTP/<NUM>"`, "<NUM>", "<NUM>", "\"-\"", `"Mozilla/<NUM> (X11; Linux x86_<NUM>; rv:<NUM>) Gecko/<NUM> Firefox/<NUM>"`, `"-"`},
	},
	// Linux systemd (journalctl) logs
	{
		`Mar 27 11:52:21 hostname systemd-networkd[2043]: enp6s0: LLDP Rx: Invoking callback for 'refreshed' event.`,
		[]string{"<TIMESTAMP>", "hostname", "systemd-networkd[<NUM>]:", "enp6s0:", "LLDP", "Rx:", "Invoking", "callback", "for", "'refreshed'", "event."},
	},
	{
		`Feb 29 23:00:14 nixos dbus-daemon[11432]: [system] Activating via systemd: service name='org.opensuse.Snapper' unit='snapperd.service' requested by ':1.324' (uid=0 pid=22089 comm="/nix/store/7rgimysvkczzyiaq4fkfymyjad4vbd9c-snappe" label="kernel")`,
		[]string{"<TIMESTAMP>", "nixos", "dbus-daemon[<NUM>]:", "[system]", "Activating", "via", "systemd:", "service", "name='org.opensuse.Snapper'", "unit='snapperd.service'", "requested", "by", "':<NUM>'", "(uid=<NUM>", "pid=<NUM>", "comm=\"/nix/store/7rgimysvkczzyiaq4fkfymyjad4vbd9c-snappe\"", "label=\"kernel\")"},
	},
	// Random slack logs:
	{
		`Apr-10 23:43:46.807 [API-Q] (T02S4RCS0) c37dfd20-1712781826.804 conversations.suggestions is ACTIVE`,
		[]string{"<TIMESTAMP>", "[API-Q]", "(T02S4RCS0)", "c37dfd20-<NUM>", "conversations.suggestions", "is", "ACTIVE"},
	},
	{
		`Apr-11 00:01:57.743 [DEVICE-PERMISSIONS-MA] Permissions saved to local storage: {"permissions":{"microphone":"granted","camera":"prompt","screen":"prompt"},"timestamp":1712782917742}`,
		[]string{"<TIMESTAMP>", "[DEVICE-PERMISSIONS-MA]", "Permissions", "saved", "to", "local", "storage:", `{"permissions":`, `{"microphone":`, `"granted"`, `,"camera":`, `"prompt"`, `,"screen":`, `"prompt"}`, `,"timestamp":`, `<NUM>}`},
	},
	// Another weird log from loki:
	{
		`ts=2023-09-06T00:59:59.900879737Z caller=spanlogger.go:86 user=29 level=debug msg="querying ingester" params="selector={stream=\"stdout\", pod=\"loki-canary-t98wq\"}, direction=BACKWARD, start=2023-09-05 23:20:28.030285153 +0000 UTC, end=2023-09-05 23:20:48.030285153 +0000 UTC, limit=1000, shards="`,
		[]string{"ts=<TIMESTAMP>", "caller=spanlogger.go:<NUM>", "user=<NUM>", "level=debug", `msg="querying ingester"`, `params="selector={stream=\"stdout\", pod=\"loki-canary-t98wq\"}, direction=BACKWARD, start=<TIMESTAMP>, end=<TIMESTAMP>, limit=<NUM>, shards="`},
	},
	// {``, []string{}},
}

func getTimestampTests(t *testing.T) []tokenizationTestCase {
	timeStamps := []string{
		"2023-01-03T15:04:05.999999999Z",
		"2024-02-29T11:12:03.33+02:00",
		"2022-03-02T23:59:59Z",
		"2024-04-01T01:33:59+03:00",
		"2021-05-07T00:00:00-04:30",
		"1999-06-12T13:37:31.123+03:00",
		"2005-07-17T12:00:00Z",
		"1988-08-03T00:00:00Z",
		"2020-09-30T00:00:59.9999+03:00",
		"2034-10-31T12:34:56.7890+11:23",
		"2034-11-01T01:23:45.67890Z",
		"2025-12-31T01:23:45.67890Z",
	}

	timestampConst := string(placeholderTimestamp)
	layouts := []struct {
		layout    string
		expResult []string
	}{
		// TOFIX: all of the commented out lines.

		//{time.Layout, []string{timestampConst}},
		{time.ANSIC, []string{timestampConst}},
		{time.UnixDate, []string{timestampConst}},
		{"Mon _2 Jan 15:04:05 MST 2006", []string{timestampConst}}, // linux `date`
		{time.RubyDate, []string{timestampConst}},
		{time.RFC822, []string{timestampConst}},
		{time.RFC822Z, []string{timestampConst}},
		{time.RFC850, []string{timestampConst}},
		{time.RFC1123, []string{timestampConst}},
		{time.RFC1123Z, []string{timestampConst}},
		{time.RFC3339, []string{timestampConst}},
		{time.RFC3339Nano, []string{timestampConst}},
		//{time.Stamp, []string{timestampConst}},
		//{time.StampMilli, []string{timestampConst}},
		//{time.StampMicro, []string{timestampConst}},
		//{time.StampNano, []string{timestampConst}},
		{time.DateTime, []string{timestampConst}},

		// TOFIX: maybe consider these timestamps?
		{time.DateOnly, []string{"<NUM>-<NUM>-<NUM>"}},
		{time.TimeOnly, []string{"<NUM>:<NUM>:<NUM>"}},
	}

	result := make([]tokenizationTestCase, 0, len(timeStamps)*len(layouts))
	for _, tss := range timeStamps {
		ts, err := time.Parse(time.RFC3339Nano, tss)
		require.NoError(t, err)
		for _, layout := range layouts {
			result = append(result, tokenizationTestCase{
				line:      ts.Format(layout.layout),
				expResult: layout.expResult,
			})
		}
	}
	return result
}

type tokenizationTestCasePack struct {
	name  string
	cases []tokenizationTestCase
}

func TestTokenization(t *testing.T) {
	packs := []tokenizationTestCasePack{
		{"cornerTestCases", tokenizationCornerTestCases},
		{"timestamps", getTimestampTests(t)},
		{"realisticTestCases", tokenizationRealisticTestCases},
	}

	for i, pack := range packs {
		pack := pack
		t.Run(fmt.Sprintf("test-pack-%d-%s", i, pack.name), func(t *testing.T) {
			for j, tc := range pack.cases {
				tc := tc
				t.Run(fmt.Sprintf("case-%d", j), func(t *testing.T) {
					result := PreprocessAndTokenize([]byte(tc.line))
					assert.Equal(
						t, tc.expResult, result,
						fmt.Sprintf("Log line: %q\nActual result slice: %#v", tc.line, result),
					)
				})
			}
		})
	}
}

func TestTokenizationMemcpy(t *testing.T) {
	buf := make([]byte, 0, 1000)
	for i := 0; i < 300; i++ {
		buf = append(buf, 'a', ' ')
	}
	tokenizer := tokenizer{rawLine: buf, maxTokens: 10}
	tokenizer.tokenize()
	require.Less(t, len(tokenizer.line), 100)
}

// Useful for running single test cases in isolation
func TestTokenizationPlayground(t *testing.T) {
	tc := tokenizationTestCase{
		"foo 121113.21231 bar 123.0.1.123 -123 -1231.11",
		[]string{"foo", "<NUM>", "bar", "<IP>", "<NUM>", "<NUM>"},
	}
	result := PreprocessAndTokenize([]byte(tc.line))
	assert.Equal(
		t, tc.expResult, result,
		fmt.Sprintf("Log line: %q\nActual result slice: %#v", tc.line, result),
	)
}

func TestReplacementPlayground(t *testing.T) {
	line := "Jul  1 00:21:28"
	exp := "<TIMESTAMP>"
	r := replacer{source: []byte(line)}
	r.replaceWithPlaceholders()

	assert.Equal(
		t, exp, string(r.dest),
		fmt.Sprintf("Log line: %q", line),
	)
}
