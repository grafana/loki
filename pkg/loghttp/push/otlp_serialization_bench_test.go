package push

// Benchmarks for the OTLP log serialization path:
//   1. plogotlp.ExportRequest.MarshalProto / UnmarshalProto   — OTel wire format (vendored pdata)
//   2. otlpToLokiPushRequest                                    — OTLP → Loki push.PushRequest conversion
//   3. push.PushRequest.Marshal / Unmarshal                    — distributor → ingester gRPC frame
//
// Payload: 5 resources × 2 scopes × 150 log records = 1500 entries per request.
// Each resource carries 6 attributes typical of a Kubernetes pod (service.name, k8s.*).
// Each log record has a body, severity, timestamp, and 5 log-level attributes.
// This approximates a realistic batch from a multi-service OTel agent pushing to Loki.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

const (
	benchResources  = 5
	benchScopes     = 2
	benchLogRecords = 150 // per scope; total = 5×2×150 = 1500 entries
)

// makeOTLPLogs builds a plog.Logs payload with realistic K8s-style attributes.
func makeOTLPLogs(resources, scopesPerResource, recordsPerScope int) plog.Logs {
	ld := plog.NewLogs()
	ts := pcommon.Timestamp(time.Now().UnixNano())

	for r := 0; r < resources; r++ {
		rl := ld.ResourceLogs().AppendEmpty()
		res := rl.Resource()
		attrs := res.Attributes()
		attrs.PutStr("service.name", fmt.Sprintf("service-%d", r))
		attrs.PutStr("k8s.namespace.name", fmt.Sprintf("ns-%d", r))
		attrs.PutStr("k8s.pod.name", fmt.Sprintf("pod-%d-%d", r, r*3))
		attrs.PutStr("k8s.container.name", fmt.Sprintf("container-%d", r))
		attrs.PutStr("k8s.node.name", fmt.Sprintf("node-%d", r%3))
		attrs.PutStr("k8s.deployment.name", fmt.Sprintf("deploy-%d", r))

		for s := 0; s < scopesPerResource; s++ {
			sl := rl.ScopeLogs().AppendEmpty()
			sl.Scope().SetName(fmt.Sprintf("io.opentelemetry.scope.%d", s))
			sl.Scope().SetVersion("1.0.0")

			for k := 0; k < recordsPerScope; k++ {
				lr := sl.LogRecords().AppendEmpty()
				lr.SetTimestamp(ts)
				lr.SetObservedTimestamp(ts)
				lr.SetSeverityNumber(plog.SeverityNumberInfo)
				lr.SetSeverityText("INFO")
				lr.Body().SetStr(fmt.Sprintf("request completed method=GET path=/api/v1/resource/%d duration_ms=42 status=200", k))
				logAttrs := lr.Attributes()
				logAttrs.PutStr("http.method", "GET")
				logAttrs.PutStr("http.url", fmt.Sprintf("/api/v1/resource/%d", k))
				logAttrs.PutInt("http.status_code", 200)
				logAttrs.PutDouble("duration_ms", 42.0)
				logAttrs.PutBool("sampled", k%5 == 0)
			}
		}
	}
	return ld
}

// globalOTLPCfg is initialized once for benchmarks.
var globalOTLPCfg GlobalOTLPConfig

func init() {
	flagext.DefaultValues(&globalOTLPCfg)
}

// BenchmarkOTLPExportRequest_MarshalProto measures OTel-format proto serialization.
// This is the format written by OTLP clients (OTel SDKs, collectors) and read by Loki.
func BenchmarkOTLPExportRequest_MarshalProto(b *testing.B) {
	req := plogotlp.NewExportRequestFromLogs(makeOTLPLogs(benchResources, benchScopes, benchLogRecords))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := req.MarshalProto()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOTLPExportRequest_UnmarshalProto measures OTel-format proto deserialization.
// This is the hottest decode in the OTLP ingest path: every ExportLogsServiceRequest
// goes through here before conversion to Loki types. Uses vendored pdata with []*ResourceLogs.
func BenchmarkOTLPExportRequest_UnmarshalProto(b *testing.B) {
	src := plogotlp.NewExportRequestFromLogs(makeOTLPLogs(benchResources, benchScopes, benchLogRecords))
	wire, err := src.MarshalProto()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := plogotlp.NewExportRequest()
		if err := req.UnmarshalProto(wire); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOTLPToLokiConversion measures the OTLP→Loki conversion step:
// the otlpToLokiPushRequest function that builds push.PushRequest from pdata.Logs.
// This is pure CPU/alloc in Loki-owned code after the OTel decode.
func BenchmarkOTLPToLokiConversion(b *testing.B) {
	ld := makeOTLPLogs(benchResources, benchScopes, benchLogRecords)
	otlpCfg := DefaultOTLPConfig(globalOTLPCfg)
	limits := &fakeLimits{}
	streamResolver := newMockStreamResolver("bench-tenant", limits)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"bench-tenant",
			otlpCfg,
			nil, // tenantConfigs
			limits.DiscoverServiceName("bench-tenant"),
			nil, // UsageTracker
			NewPushStats(),
			nil, // logger
			streamResolver,
			"otlp",
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOTLPFullIngestPath combines UnmarshalProto + conversion in sequence,
// matching the real codepath in pkg/loghttp/push.ParseOTLPRequest.
func BenchmarkOTLPFullIngestPath(b *testing.B) {
	src := plogotlp.NewExportRequestFromLogs(makeOTLPLogs(benchResources, benchScopes, benchLogRecords))
	wire, err := src.MarshalProto()
	if err != nil {
		b.Fatal(err)
	}

	otlpCfg := DefaultOTLPConfig(globalOTLPCfg)
	limits := &fakeLimits{}
	streamResolver := newMockStreamResolver("bench-tenant", limits)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := plogotlp.NewExportRequest()
		if err := req.UnmarshalProto(wire); err != nil {
			b.Fatal(err)
		}
		_, err := otlpToLokiPushRequest(
			context.Background(),
			req.Logs(),
			"bench-tenant",
			otlpCfg,
			nil,
			limits.DiscoverServiceName("bench-tenant"),
			nil,
			NewPushStats(),
			nil,
			streamResolver,
			"otlp",
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}
