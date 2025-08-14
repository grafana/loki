package push

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/otlptranslator"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// Simple wrapper to make bytes.Reader implement io.ReadCloser
type readCloser struct {
	*bytes.Reader
}

func (rc *readCloser) Close() error {
	return nil
}

// Benchmark data generators
func createSimpleOTLPLogs(recordCount int) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "test-service")
	rl.Resource().Attributes().PutStr("pod.name", "test-pod-123")
	rl.Resource().Attributes().PutStr("namespace", "default")

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("test-scope")
	sl.Scope().Attributes().PutStr("scope.attr", "scope-value")

	for i := 0; i < recordCount; i++ {
		logRecord := sl.LogRecords().AppendEmpty()
		logRecord.Body().SetStr("test log message")
		logRecord.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
		logRecord.SetSeverityText("INFO")
		logRecord.Attributes().PutStr("user_id", "user123")
		logRecord.Attributes().PutStr("request_id", "req456")
	}
	return ld
}

func createComplexOTLPLogs(resourceCount, scopeCount, recordCount int) plog.Logs {
	ld := plog.NewLogs()

	for r := 0; r < resourceCount; r++ {
		rl := ld.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "test-service")
		rl.Resource().Attributes().PutStr("pod.name", "test-pod-123")
		rl.Resource().Attributes().PutStr("namespace", "default")
		rl.Resource().Attributes().PutStr("node.name", "node-1")
		rl.Resource().Attributes().PutStr("cluster", "prod")

		// Add nested attributes
		nestedMap := rl.Resource().Attributes().PutEmptyMap("metadata")
		nestedMap.PutStr("version", "v1.0.0")
		nestedMap.PutInt("replicas", 3)

		for s := 0; s < scopeCount; s++ {
			sl := rl.ScopeLogs().AppendEmpty()
			sl.Scope().SetName("test-scope")
			sl.Scope().SetVersion("1.0.0")
			sl.Scope().Attributes().PutStr("scope.attr", "scope-value")
			sl.Scope().Attributes().PutStr("component", "api-server")

			for i := 0; i < recordCount; i++ {
				logRecord := sl.LogRecords().AppendEmpty()
				logRecord.Body().SetStr("test log message with some additional content to make it larger")
				logRecord.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
				logRecord.SetSeverityText("INFO")
				logRecord.SetSeverityNumber(plog.SeverityNumberInfo)
				logRecord.Attributes().PutStr("user_id", "user123")
				logRecord.Attributes().PutStr("request_id", "req456")
				logRecord.Attributes().PutStr("endpoint", "/api/v1/users")
				logRecord.Attributes().PutInt("status_code", 200)
				logRecord.Attributes().PutDouble("response_time", 0.123)

				// Add trace context
				logRecord.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
				logRecord.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
			}
		}
	}
	return ld
}

func createOTLPRequest(logs plog.Logs) (*http.Request, []byte) {
	req := plogotlp.NewExportRequestFromLogs(logs)
	protoBytes, _ := req.MarshalProto()

	httpReq := httptest.NewRequest("POST", "/v1/logs", &readCloser{bytes.NewReader(protoBytes)})
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	return httpReq, protoBytes
}

// Mock implementations for benchmarking
type benchmarkStreamResolver struct{}

func (m *benchmarkStreamResolver) PolicyFor(lbs labels.Labels) string {
	return "default-policy"
}

func (m *benchmarkStreamResolver) RetentionPeriodFor(lbs labels.Labels) time.Duration {
	return time.Hour
}

func (m *benchmarkStreamResolver) RetentionHoursFor(lbs labels.Labels) string {
	return "1"
}

type benchmarkLimits struct{}

func (m *benchmarkLimits) OTLPConfig(userID string) OTLPConfig {
	return DefaultOTLPConfig(defaultGlobalOTLPConfig)
}

func (m *benchmarkLimits) DiscoverServiceName(userID string) []string {
	return []string{"service.name", "app.name"}
}

// Benchmark tests
func BenchmarkOTLPParseRequest_Simple(b *testing.B) {
	logs := createSimpleOTLPLogs(100)
	req, protoBytes := createOTLPRequest(logs)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req.Body = &readCloser{bytes.NewReader(protoBytes)}
		_, _, err := ParseOTLPRequest(
			"test-user",
			req,
			&benchmarkLimits{},
			nil,
			100<<20,
			nil,
			&benchmarkStreamResolver{},
			log.NewNopLogger(),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTLPParseRequest_Complex(b *testing.B) {
	logs := createComplexOTLPLogs(5, 3, 20) // 5 resources, 3 scopes each, 20 logs each = 300 total logs
	req, protoBytes := createOTLPRequest(logs)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req.Body = &readCloser{bytes.NewReader(protoBytes)}
		_, _, err := ParseOTLPRequest(
			"test-user",
			req,
			&benchmarkLimits{},
			nil,
			100<<20,
			nil,
			&benchmarkStreamResolver{},
			log.NewNopLogger(),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTLPToLokiPushRequest(b *testing.B) {
	logs := createComplexOTLPLogs(5, 3, 20)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := otlpToLokiPushRequest(
			context.Background(),
			logs,
			"test-user",
			DefaultOTLPConfig(defaultGlobalOTLPConfig),
			nil,
			[]string{"service.name", "app.name"},
			nil,
			NewPushStats(),
			log.NewNopLogger(),
			&benchmarkStreamResolver{},
			constants.OTLP,
			otlptranslator.LabelNamer{},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTLPLogToPushEntry(b *testing.B) {
	logs := createComplexOTLPLogs(1, 1, 1)
	logRecord := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	labelNamer := otlptranslator.LabelNamer{UTF8Allowed: false} // Reuse LabelNamer

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, err := otlpLogToPushEntry(
			logRecord,
			DefaultOTLPConfig(defaultGlobalOTLPConfig),
			false,
			nil,
			labelNamer, // Use reused instance
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeToLabels(b *testing.B) {
	labelNamer := otlptranslator.LabelNamer{UTF8Allowed: false}
	attrs := pcommon.NewMap()
	attrs.PutStr("string_attr", "string_value")
	attrs.PutInt("int_attr", 42)
	attrs.PutDouble("double_attr", 3.14)
	attrs.PutBool("bool_attr", true)

	// Add nested attributes
	nestedMap := attrs.PutEmptyMap("nested")
	nestedMap.PutStr("nested_key", "nested_value")
	nestedMap.PutInt("nested_int", 100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := attributesToLabels(attrs, "prefix", labelNamer)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Baseline benchmark that creates LabelNamer each iteration (original approach)
func BenchmarkAttributeToLabels_Baseline(b *testing.B) {
	attrs := pcommon.NewMap()
	attrs.PutStr("string_attr", "string_value")
	attrs.PutInt("int_attr", 42)
	attrs.PutDouble("double_attr", 3.14)
	attrs.PutBool("bool_attr", true)

	// Add nested attributes
	nestedMap := attrs.PutEmptyMap("nested")
	nestedMap.PutStr("nested_key", "nested_value")
	nestedMap.PutInt("nested_int", 100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		labelNamer := otlptranslator.LabelNamer{UTF8Allowed: false} // Create new instance each time
		_, err := attributesToLabels(attrs, "prefix", labelNamer)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkModelLabelsSetToLabelsList(b *testing.B) {
	labelSet := model.LabelSet{
		"service_name": "test-service",
		"pod_name":     "test-pod-123",
		"namespace":    "default",
		"cluster":      "prod",
		"node_name":    "node-1",
		"version":      "v1.0.0",
		"component":    "api-server",
		"endpoint":     "/api/v1/users",
		"status_code":  "200",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = modelLabelsSetToLabelsList(labelSet)
	}
}

func BenchmarkLabelsSize(b *testing.B) {
	labels := push.LabelsAdapter{
		{Name: "service_name", Value: "test-service"},
		{Name: "pod_name", Value: "test-pod-123"},
		{Name: "namespace", Value: "default"},
		{Name: "cluster", Value: "prod"},
		{Name: "node_name", Value: "node-1"},
		{Name: "version", Value: "v1.0.0"},
		{Name: "component", Value: "api-server"},
		{Name: "endpoint", Value: "/api/v1/users"},
		{Name: "status_code", Value: "200"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = labelsSize(labels)
	}
}

// Benchmark different payload sizes
func BenchmarkOTLPParseRequest_PayloadSizes(b *testing.B) {
	sizes := []struct {
		name          string
		resourceCount int
		scopeCount    int
		recordCount   int
		expectedLogs  int
	}{
		{"Small", 1, 1, 10, 10},
		{"Medium", 3, 2, 50, 300},
		{"Large", 5, 3, 100, 1500},
		{"XLarge", 10, 5, 200, 10000},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			logs := createComplexOTLPLogs(size.resourceCount, size.scopeCount, size.recordCount)
			req, protoBytes := createOTLPRequest(logs)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				req.Body = &readCloser{bytes.NewReader(protoBytes)}
				_, _, err := ParseOTLPRequest(
					"test-user",
					req,
					&benchmarkLimits{},
					nil,
					100<<20,
					nil,
					&benchmarkStreamResolver{},
					log.NewNopLogger(),
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
