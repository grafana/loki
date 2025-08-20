package push

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/v3/pkg/runtime"
)

type mockLimits struct{}

func (m mockLimits) OTLPConfig(userID string) OTLPConfig {
	return DefaultOTLPConfig(GlobalOTLPConfig{})
}

func (m mockLimits) DiscoverServiceName(userID string) []string {
	return []string{"service.name"}
}

type benchmarkStreamResolver struct{}

func (m benchmarkStreamResolver) RetentionPeriodFor(lbs labels.Labels) time.Duration {
	return 24 * time.Hour
}

func (m benchmarkStreamResolver) RetentionHoursFor(lbs labels.Labels) string {
	return "24h"
}

func (m benchmarkStreamResolver) PolicyFor(lbs labels.Labels) string {
	return "default"
}

type mockUsageTracker struct{}

func (m mockUsageTracker) ReceivedBytesAdd(ctx context.Context, tenant string, retentionPeriod time.Duration, labels labels.Labels, value float64, format string) {
}

func (m mockUsageTracker) DiscardedBytesAdd(ctx context.Context, tenant, reason string, labels labels.Labels, value float64, format string) {
}

func createOTLPLogs(numLogs int) plog.Logs {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()

	// Set resource attributes
	resource := resourceLogs.Resource()
	resource.Attributes().PutStr("service.name", "test-service")
	resource.Attributes().PutStr("service.namespace", "test-namespace")
	resource.Attributes().PutStr("k8s.pod.name", "test-pod")
	resource.Attributes().PutStr("k8s.namespace.name", "test-namespace")

	// Create scope logs
	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()
	scope := scopeLogs.Scope()
	scope.SetName("test-scope")
	scope.SetVersion("1.0.0")

	// Create log records
	logRecords := scopeLogs.LogRecords()
	for i := 0; i < numLogs; i++ {
		logRecord := logRecords.AppendEmpty()
		logRecord.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
		logRecord.SetObservedTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
		logRecord.SetSeverityText("INFO")
		logRecord.SetSeverityNumber(plog.SeverityNumberInfo)
		logRecord.Body().SetStr("This is a test log message for benchmarking")

		// Add some attributes
		attrs := logRecord.Attributes()
		attrs.PutStr("http.method", "GET")
		attrs.PutStr("http.url", "/api/test")
		attrs.PutInt("http.status_code", 200)
		attrs.PutStr("user.id", "user123")
		attrs.PutBool("error", false)
	}

	return logs
}

func createHTTPRequest(logs plog.Logs) *http.Request {
	req := plogotlp.NewExportRequestFromLogs(logs)
	protoBytes, _ := req.MarshalProto()
	httpReq, _ := http.NewRequest("POST", "/otlp/v1/logs", bytes.NewReader(protoBytes))

	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "identity")

	return httpReq
}

func BenchmarkParseOTLPRequest(b *testing.B) {
	limits := mockLimits{}
	streamResolver := benchmarkStreamResolver{}
	usageTracker := mockUsageTracker{}
	logger := log.NewNopLogger()

	logs := createOTLPLogs(10000)
	req := createHTTPRequest(logs)

	protoBytes, _ := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	req.Body = io.NopCloser(bytes.NewReader(protoBytes))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = ParseOTLPRequest(
			"test-user",
			req,
			limits,
			runtime.DefaultTenantConfigs(),
			10*1024*1024, // 10MB max message size
			usageTracker,
			streamResolver,
			logger,
		)
	}
}

func BenchmarkParseOTLPRequestWithGzip(b *testing.B) {
	logs := createOTLPLogs(100)
	req := createHTTPRequest(logs)

	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	protoBytes, _ := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	gzipWriter.Write(protoBytes)
	gzipWriter.Close()

	req.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")

	limits := mockLimits{}
	streamResolver := benchmarkStreamResolver{}
	usageTracker := mockUsageTracker{}
	logger := log.NewNopLogger()
	req.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = ParseOTLPRequest(
			"test-user",
			req,
			limits,
			runtime.DefaultTenantConfigs(),
			10*1024*1024, // 10MB max message size
			usageTracker,
			streamResolver,
			logger,
		)
	}
}

func BenchmarkParseOTLPRequestJSON(b *testing.B) {
	logs := createOTLPLogs(100)
	req := plogotlp.NewExportRequestFromLogs(logs)

	jsonBytes, err := req.MarshalJSON()
	if err != nil {
		b.Fatalf("Failed to marshal JSON: %v", err)
	}

	httpReq, err := http.NewRequest("POST", "/otlp/v1/logs", bytes.NewReader(jsonBytes))
	if err != nil {
		b.Fatalf("Failed to create request: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Content-Encoding", "identity")

	limits := mockLimits{}
	streamResolver := benchmarkStreamResolver{}
	usageTracker := mockUsageTracker{}
	logger := log.NewNopLogger()
	httpReq.Body = io.NopCloser(bytes.NewReader(jsonBytes))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = ParseOTLPRequest(
			"test-user",
			httpReq,
			limits,
			runtime.DefaultTenantConfigs(),
			10*1024*1024, // 10MB max message size
			usageTracker,
			streamResolver,
			logger,
		)
	}
}
