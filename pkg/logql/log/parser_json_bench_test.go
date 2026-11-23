package log

import (
	"encoding/json"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

// jsonBenchLines are JSON log lines of the shapes that Loki users commonly query with
// `| json`, `| json a="b.c"` and `| unpack`.
var jsonBenchLines = []struct {
	name string
	line string
	// exprs are the expressions for `| json <exprs>`, written as label=expression pairs.
	exprs [][2]string
	// hint is a label that a query downstream of the parser might require.
	hint string
}{
	{
		// A small structured application log, as written by zap, logrus, slog and friends.
		name:  "app-flat",
		line:  `{"level":"info","ts":1700000000.123456,"caller":"server/handler.go:142","msg":"request completed","method":"GET","path":"/api/v1/users/42","status":200,"duration_ms":12.5,"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"}`,
		exprs: [][2]string{{"level", "level"}, {"msg", "msg"}, {"status", "status"}, {"duration", "duration_ms"}},
		hint:  "status",
	},
	{
		// What the container runtime (CRI-O / docker json-file) writes: the application line is a
		// string field, full of escapes.
		name:  "container-wrapped",
		line:  `{"log":"level=info ts=2024-05-01T12:00:00.123456789Z caller=metrics.go:159 org_id=29 msg=\"query stats\" query=\"sum by (object_name) (rate({container=\\\"metrictank\\\"} |= \\\"PANIC\\\" [5m]))\" duration=322.623724ms\n","stream":"stderr","time":"2024-05-01T12:00:00.123456789Z"}`,
		exprs: [][2]string{{"log", "log"}, {"stream", "stream"}},
		hint:  "stream",
	},
	{
		// The nginx line from Benchmark_Parser, with two nested objects.
		name:  "nginx",
		line:  `{"proxy_protocol_addr": "","remote_addr": "3.112.221.14","remote_user": "","upstream_addr": "10.12.15.234:5000","the_real_ip": "3.112.221.14","timestamp": "2020-12-11T16:20:07+00:00","protocol": "HTTP/1.1","upstream_name": "hosted-grafana-hosted-grafana-api-80","request": {"id": "c8eacb6053552c0cd1ae443bc660e140","time": "0.001","method" : "GET","host": "hg-api-qa-us-central1.grafana.net","uri": "/","size" : "128","user_agent": "worldping-api-","referer": ""},"response": {"status": 200,"upstream_status": "200","size": "1155","size_sent": "265","latency_seconds": "0.001"}}`,
		exprs: [][2]string{{"method", "request.method"}, {"status", "response.status"}, {"uri", "request.uri"}},
		hint:  "response_status",
	},
	{
		// Nested service context with arrays, null and booleans.
		name:  "nested-arrays",
		line:  `{"timestamp":"2024-05-01T12:00:00.123Z","severity":"ERROR","service":{"name":"checkout","version":"1.42.0","instance":{"id":"i-0abc123def","zone":"us-east-1a"}},"message":"payment failed: card declined","error":{"type":"CardError","code":"card_declined","stack":["payments.go:101","checkout.go:87","handler.go:42"]},"tags":["payments","retry"],"user":{"id":"u_1234","email":null,"plan":"pro"},"retry":false,"attempt":3}`,
		exprs: [][2]string{{"svc", "service.name"}, {"code", "error.code"}, {"frame", "error.stack[1]"}, {"user", "user.id"}},
		hint:  "error_code",
	},
	{
		// An exception with a multi-line stack trace in one field: long, with many escapes.
		name:  "stacktrace",
		line:  `{"time":"2024-05-01T12:00:00.123Z","level":"error","logger":"com.example.orders.OrderService","thread":"http-nio-8080-exec-7","message":"Unhandled exception processing order 8812\n\tat com.example.orders.OrderService.process(OrderService.java:214)\n\tat com.example.orders.OrderController.create(OrderController.java:88)\n\tat jdk.internal.reflect.GeneratedMethodAccessor112.invoke(Unknown Source)\n\tat java.base/java.lang.reflect.Method.invoke(Method.java:566)\n\tat org.springframework.web.method.support.InvocableHandlerMethod.doInvoke(InvocableHandlerMethod.java:205)\n\tat org.apache.catalina.core.StandardWrapperValve.invoke(StandardWrapperValve.java:197)\nCaused by: java.sql.SQLTransientConnectionException: HikariPool-1 - Connection is not available, request timed out after 30000ms.\n\t... 42 common frames omitted\n","mdc":{"orderId":"8812","tenant":"acme"}}`,
		exprs: [][2]string{{"level", "level"}, {"logger", "logger"}, {"tenant", "mdc.tenant"}},
		hint:  "logger",
	},
	{
		// Non-ASCII text, \u escapes and a literal emoji.
		name:  "unicode",
		line:  `{"msg":"café ☃ \"quoted\" 👌","lang":"fr","emoji":"👌","user":"José Ñandú","len":24}`,
		exprs: [][2]string{{"msg", "msg"}, {"lang", "lang"}},
		hint:  "lang",
	},
	{
		// A Kubernetes audit event: bigger, with several levels of nesting.
		name:  "k8s-audit",
		line:  `{"kind":"Event","apiVersion":"audit.k8s.io/v1","level":"Metadata","auditID":"7d8f0f43-6b0c-4b0e-9f3b-0a6d1d5b8d11","stage":"ResponseComplete","requestURI":"/api/v1/namespaces/prod/pods/checkout-7745855568-blq6t/log?container=app&follow=true","verb":"get","user":{"username":"system:serviceaccount:monitoring:promtail","uid":"2b8f1c3e-5d77-4f0a-8c4e-0e3a9b7c1d22","groups":["system:serviceaccounts","system:serviceaccounts:monitoring","system:authenticated"]},"sourceIPs":["10.12.15.234"],"userAgent":"promtail/2.9.1 (linux/amd64)","objectRef":{"resource":"pods","namespace":"prod","name":"checkout-7745855568-blq6t","apiVersion":"v1","subresource":"log"},"responseStatus":{"metadata":{},"code":200},"requestReceivedTimestamp":"2024-05-01T12:00:00.123456Z","stageTimestamp":"2024-05-01T12:00:00.234567Z","annotations":{"authorization.k8s.io/decision":"allow","authorization.k8s.io/reason":"RBAC: allowed by ClusterRoleBinding \"promtail\" of ClusterRole \"promtail\" to ServiceAccount \"promtail/monitoring\""}}`,
		exprs: [][2]string{{"verb", "verb"}, {"ns", "objectRef.namespace"}, {"code", "responseStatus.code"}, {"user", "user.username"}},
		hint:  "verb",
	},
}

// packedBenchLines are lines as written by the promtail pack stage, for `| unpack`.
var packedBenchLines = []struct {
	name string
	line string
	hint string
}{
	{
		name: "packed",
		line: `{"app":"checkout","pod":"checkout-7745855568-blq6t","namespace":"prod","container":"app","_entry":"level=info ts=2024-05-01T12:00:00.123Z msg=\"request completed\" method=GET path=/api/v1/users/42 status=200 duration=12.5ms"}`,
		hint: "pod",
	},
	{
		name: "packed-json-entry",
		line: `{"app":"checkout","pod":"checkout-7745855568-blq6t","namespace":"prod","_entry":"{\"level\":\"info\",\"msg\":\"hello \\\"world\\\"\",\"status\":200}"}`,
		hint: "app",
	},
	{
		name: "not-packed",
		line: `{"level":"info","ts":1700000000.123456,"msg":"request completed","status":200}`,
		hint: "level",
	},
}

var jsonBenchStreamLabels = labels.FromStrings("cluster", "qa-us-central1",
	"namespace", "qa",
	"job", "ingress-nginx/nginx-ingress-controller",
	"pod", "nginx-ingress-controller-7745855568-blq6t",
	"stream", "stdout",
)

// runParserBench runs stage over each line in turn, as a query would over a stream of mixed lines.
func runParserBench(b *testing.B, newStage func() Stage, lines [][]byte, hints *ParserHints) {
	b.Helper()
	var total int
	for _, l := range lines {
		total += len(l)
	}
	b.ReportAllocs()
	stage := newStage()
	builder := NewBaseLabelsBuilder().ForLabels(jsonBenchStreamLabels, labels.StableHash(jsonBenchStreamLabels))
	if hints != nil {
		builder.parserKeyHints = hints
	}
	for n := 0; b.Loop(); n++ {
		builder.Reset()
		_, _ = stage.Process(0, UnsafeBytes{lines[n%len(lines)]}, builder)
		// We don't extract labels from the builder, as that would detract from the JSON-parsing time.
	}
}

// BenchmarkJSONParserRealistic measures `| json` on typical log lines.
func BenchmarkJSONParserRealistic(b *testing.B) {
	var all [][]byte
	for _, tc := range jsonBenchLines {
		all = append(all, []byte(tc.line))
	}
	newStage := func() Stage { return NewJSONParser(false) }

	b.Run("mixed/all-labels", func(b *testing.B) {
		runParserBench(b, newStage, all, nil)
	})
	b.Run("mixed/hint-level", func(b *testing.B) {
		runParserBench(b, newStage, all, NewParserHint(nil, []string{"level", "msg"}, false, false, ""))
	})
	for _, tc := range jsonBenchLines {
		line := [][]byte{[]byte(tc.line)}
		b.Run(tc.name+"/all-labels", func(b *testing.B) {
			runParserBench(b, newStage, line, nil)
		})
		b.Run(tc.name+"/one-label", func(b *testing.B) {
			runParserBench(b, newStage, line, NewParserHint(nil, []string{tc.hint}, false, false, ""))
		})
	}
	b.Run("mixed/capture-json-path", func(b *testing.B) {
		runParserBench(b, func() Stage { return NewJSONParser(true) }, all, nil)
	})
}

// BenchmarkJSONExpressionParserRealistic measures `| json label="path"` on typical log lines.
func BenchmarkJSONExpressionParserRealistic(b *testing.B) {
	for _, tc := range jsonBenchLines {
		var exprs []LabelExtractionExpr
		for _, e := range tc.exprs {
			exprs = append(exprs, NewLabelExtractionExpr(e[0], e[1]))
		}
		line := [][]byte{[]byte(tc.line)}
		b.Run(tc.name, func(b *testing.B) {
			runParserBench(b, func() Stage { return mustStage(NewJSONExpressionParser(exprs)) }, line, nil)
		})
	}
	// Expressions that match nothing have to scan the whole line.
	b.Run("no-match", func(b *testing.B) {
		line := [][]byte{[]byte(jsonBenchLines[6].line)}
		exprs := []LabelExtractionExpr{NewLabelExtractionExpr("nope", "does.not.exist"), NewLabelExtractionExpr("nope2", "nor[3].this")}
		runParserBench(b, func() Stage { return mustStage(NewJSONExpressionParser(exprs)) }, line, nil)
	})
}

// BenchmarkUnpackParserRealistic measures `| unpack` on packed and unpacked lines.
func BenchmarkUnpackParserRealistic(b *testing.B) {
	for _, tc := range packedBenchLines {
		line := [][]byte{[]byte(tc.line)}
		b.Run(tc.name+"/all-labels", func(b *testing.B) {
			runParserBench(b, func() Stage { return NewUnpackParser() }, line, nil)
		})
		b.Run(tc.name+"/one-label", func(b *testing.B) {
			runParserBench(b, func() Stage { return NewUnpackParser() }, line, NewParserHint(nil, []string{tc.hint}, false, false, ""))
		})
	}
}

// TestJSONBenchLinesValid guards the benchmark corpus: a line that is not valid JSON would make
// the benchmarks measure the error path.
func TestJSONBenchLinesValid(t *testing.T) {
	for _, tc := range jsonBenchLines {
		require.True(t, json.Valid([]byte(tc.line)), tc.name)
	}
	for _, tc := range packedBenchLines {
		require.True(t, json.Valid([]byte(tc.line)), tc.name)
	}
}
