package push

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func generateOTLPLogs(numResources, numScopes, numLogs int, options benchmarkOptions) plog.Logs {
	ld := plog.NewLogs()

	for i := 0; i < numResources; i++ {
		rl := ld.ResourceLogs().AppendEmpty()
		res := rl.Resource()

		if !options.emptyResourceAttrs {
			res.Attributes().PutStr("service.name", "test-service")
			res.Attributes().PutStr("k8s.namespace.name", "default")
			res.Attributes().PutStr("k8s.pod.name", "test-pod-12345")
			res.Attributes().PutStr("k8s.pod.ip", "10.200.200.200")
			res.Attributes().PutStr("cloud.region", "us-east-1")
			res.Attributes().PutStr("cloud.availability_zone", "us-east-1a")

			if options.nestedAttributes {
				nested := res.Attributes().PutEmptyMap("metadata")
				nested.PutStr("version", "1.0.0")
				nested.PutStr("environment", "production")

				if options.deeplyNested {
					level1 := res.Attributes().PutEmptyMap("level1")
					level2 := level1.PutEmptyMap("level2")
					level3 := level2.PutEmptyMap("level3")
					level3.PutStr("deeply_nested_value", "found me!")
				}
			}
		}

		for j := 0; j < numScopes; j++ {
			sl := rl.ScopeLogs().AppendEmpty()
			scope := sl.Scope()
			scope.SetName("test-scope")
			scope.SetVersion("v1.0.0")

			if !options.emptyScopeAttrs {
				scope.Attributes().PutStr("function", "ProcessRequest")
				scope.Attributes().PutStr("module", "api-handler")
			}

			for k := 0; k < numLogs; k++ {
				logRecord := sl.LogRecords().AppendEmpty()
				logRecord.Body().SetStr("This is a test log message with some content")
				logRecord.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
				logRecord.SetSeverityNumber(plog.SeverityNumberInfo)
				logRecord.SetSeverityText("INFO")

				if !options.emptyLogAttrs {
					logRecord.Attributes().PutStr("http.method", "GET")
					logRecord.Attributes().PutStr("http.status_code", "200")
					logRecord.Attributes().PutStr("user_id", "user-123")
					logRecord.Attributes().PutStr("request_id", "req-abc-def-ghi")

					if options.manyAttributes {
						for i := 0; i < 20; i++ {
							logRecord.Attributes().PutStr("custom_attr_"+string(rune('a'+i)), "value")
						}
					}

					if options.nestedAttributes {
						nested := logRecord.Attributes().PutEmptyMap("request_context")
						nested.PutStr("path", "/api/v1/users")
						nested.PutInt("duration_ms", 42)

						if options.deeplyNested {
							level1 := logRecord.Attributes().PutEmptyMap("trace")
							level2 := level1.PutEmptyMap("context")
							level3 := level2.PutEmptyMap("metadata")
							level4 := level3.PutEmptyMap("info")
							level4.PutStr("source", "application")
						}
					}
				}

				logRecord.SetTraceID([16]byte{0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78})
				logRecord.SetSpanID([8]byte{0x12, 0x23, 0xAD, 0x12, 0x23, 0xAD, 0x12, 0x23})
			}
		}
	}

	return ld
}

type benchmarkOptions struct {
	emptyResourceAttrs bool
	emptyScopeAttrs    bool
	emptyLogAttrs      bool
	nestedAttributes   bool
	deeplyNested       bool
	manyAttributes     bool
}

func BenchmarkOTLPProcessing_SmallBatch(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 10, benchmarkOptions{})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTLPProcessing_TypicalBatch(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 100, benchmarkOptions{})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTLPProcessing_LargeBatch(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 1000, benchmarkOptions{})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTLPProcessing_MultipleScopes(b *testing.B) {
	ld := generateOTLPLogs(1, 5, 50, benchmarkOptions{})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeFlattening_Flat(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 100, benchmarkOptions{
		nestedAttributes: false,
	})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeFlattening_2Levels(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 100, benchmarkOptions{
		nestedAttributes: true,
		deeplyNested:     false,
	})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAttributeFlattening_DeeplyNested(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 100, benchmarkOptions{
		nestedAttributes: true,
		deeplyNested:     true,
	})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEmptyAttributes(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 100, benchmarkOptions{
		emptyResourceAttrs: false, // Keep service.name for valid streams
		emptyScopeAttrs:    true,
		emptyLogAttrs:      true,
	})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkManyAttributes(b *testing.B) {
	ld := generateOTLPLogs(1, 1, 100, benchmarkOptions{
		manyAttributes: true,
	})
	otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	tracker := NewMockTracker()
	streamResolver := newMockStreamResolver("fake", &fakeLimits{})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats := NewPushStats()
		_, err := otlpToLokiPushRequest(
			context.Background(),
			ld,
			"test-user",
			otlpConfig,
			nil,
			[]string{},
			tracker,
			stats,
			log.NewNopLogger(),
			streamResolver,
			constants.OTLP,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLabelNamerConversion(b *testing.B) {
	attrs := []string{
		"service.name",
		"service.namespace",
		"k8s.pod.name",
		"k8s.namespace.name",
		"k8s.deployment.name",
		"http.method",
		"http.status_code",
		"http.url",
		"cloud.region",
		"cloud.availability_zone",
	}

	b.Run("direct_conversion", func(b *testing.B) {
		cache := newLabelNameCache()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, attr := range attrs {
				_, _ = attributeToLabels(attr, pcommon.NewValueStr("test"), "", cache)
			}
		}
	})
}

// BenchmarkAttributeToLabels_Simple tests simple attribute conversion
func BenchmarkAttributeToLabels_Simple(b *testing.B) {
	key := "service.name"
	value := pcommon.NewValueStr("test-service")
	cache := newLabelNameCache()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = attributeToLabels(key, value, "", cache)
	}
}

// BenchmarkAttributeToLabels_Nested tests nested attribute conversion
func BenchmarkAttributeToLabels_Nested(b *testing.B) {
	// Create a nested map
	value := pcommon.NewValueMap()
	nestedMap := value.Map()
	nestedMap.PutStr("foo", "bar")
	nestedMap.PutStr("baz", "qux")

	nestedLevel2 := nestedMap.PutEmptyMap("nested")
	nestedLevel2.PutStr("key1", "value1")
	nestedLevel2.PutStr("key2", "value2")

	cache := newLabelNameCache()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = attributeToLabels("metadata", value, "", cache)
	}
}

func BenchmarkStreamLookup(b *testing.B) {
	b.Run("single_stream", func(b *testing.B) {
		ld := generateOTLPLogs(1, 1, 100, benchmarkOptions{})
		otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
		tracker := NewMockTracker()
		streamResolver := newMockStreamResolver("fake", &fakeLimits{})

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			stats := NewPushStats()
			_, err := otlpToLokiPushRequest(
				context.Background(),
				ld,
				"test-user",
				otlpConfig,
				nil,
				[]string{},
				tracker,
				stats,
				log.NewNopLogger(),
				streamResolver,
				constants.OTLP,
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("multiple_streams", func(b *testing.B) {
		ld := generateOTLPLogs(10, 1, 10, benchmarkOptions{})
		otlpConfig := DefaultOTLPConfig(defaultGlobalOTLPConfig)
		tracker := NewMockTracker()
		streamResolver := newMockStreamResolver("fake", &fakeLimits{})

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			stats := NewPushStats()
			_, err := otlpToLokiPushRequest(
				context.Background(),
				ld,
				"test-user",
				otlpConfig,
				nil,
				[]string{},
				tracker,
				stats,
				log.NewNopLogger(),
				streamResolver,
				constants.OTLP,
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkTimestampConversion(b *testing.B) {
	now := time.Now()

	b.Run("with_timestamp", func(b *testing.B) {
		lr := plog.NewLogRecord()
		lr.SetTimestamp(pcommon.Timestamp(now.UnixNano()))

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = timestampFromLogRecord(lr)
		}
	})

	b.Run("with_observed_timestamp", func(b *testing.B) {
		lr := plog.NewLogRecord()
		lr.SetObservedTimestamp(pcommon.Timestamp(now.UnixNano()))

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = timestampFromLogRecord(lr)
		}
	})

	b.Run("no_timestamp", func(b *testing.B) {
		lr := plog.NewLogRecord()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = timestampFromLogRecord(lr)
		}
	})
}
