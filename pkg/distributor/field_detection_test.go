package distributor

import (
	"fmt"
	"testing"

	"github.com/grafana/dskit/flagext"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"

	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/grafana/loki/pkg/push"
)

func Test_DetectLogLevels(t *testing.T) {
	setup := func(discoverLogLevels bool) (*validation.Limits, *mockIngester) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)

		limits.DiscoverLogLevels = discoverLogLevels
		limits.AllowStructuredMetadata = true
		return limits, &mockIngester{}
	}

	t.Run("log level detection disabled", func(t *testing.T) {
		limits, ingester := setup(false)
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar"}`}, false, false, false)
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
		require.Len(t, topVal.Streams[0].Entries[0].StructuredMetadata, 0)
	})

	t.Run("log level detection enabled but level cannot be detected", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar"}`}, false, false, false)
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
		require.Len(t, topVal.Streams[0].Entries[0].StructuredMetadata, 1)
	})

	t.Run("log level detection enabled and warn logs", func(t *testing.T) {
		for _, level := range []string{"warn", "Wrn", "WARNING"} {
			limits, ingester := setup(true)
			distributors, _ := prepare(
				t,
				1,
				5,
				limits,
				func(_ string) (ring_client.PoolClient, error) { return ingester, nil },
			)

			writeReq := makeWriteRequestWithLabelsWithLevel(1, 10, []string{`{foo="bar"}`}, level)
			_, err := distributors[0].Push(ctx, writeReq)
			require.NoError(t, err)
			topVal := ingester.Peek()
			require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
			require.Equal(t, push.LabelsAdapter{
				{
					Name:  constants.LevelLabel,
					Value: constants.LogLevelWarn,
				},
			}, topVal.Streams[0].Entries[0].StructuredMetadata, fmt.Sprintf("level: %s", level))
		}
	})

	t.Run("log level detection enabled but log level already present in stream", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar", level="debug"}`}, false, false, false)
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar", level="debug"}`, topVal.Streams[0].Labels)
		sm := topVal.Streams[0].Entries[0].StructuredMetadata
		require.Len(t, sm, 1)
		require.Equal(t, sm[0].Name, constants.LevelLabel)
		require.Equal(t, sm[0].Value, constants.LogLevelDebug)
	})

	t.Run("log level detection enabled but log level already present as structured metadata", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar"}`}, false, false, false)
		writeReq.Streams[0].Entries[0].StructuredMetadata = push.LabelsAdapter{
			{
				Name:  "severity",
				Value: constants.LogLevelWarn,
			},
		}
		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar"}`, topVal.Streams[0].Labels)
		sm := topVal.Streams[0].Entries[0].StructuredMetadata
		require.Equal(t, push.LabelsAdapter{
			{
				Name:  "severity",
				Value: constants.LogLevelWarn,
			}, {
				Name:  constants.LevelLabel,
				Value: constants.LogLevelWarn,
			},
		}, sm)
	})
}

func Test_detectLogLevelFromLogEntry(t *testing.T) {
	ld := newFieldDetector(
		validationContext{
			discoverLogLevels:       true,
			allowStructuredMetadata: true,
			logLevelFields:          []string{"level", "LEVEL", "Level", "severity", "SEVERITY", "Severity", "lvl", "LVL", "Lvl"},
		})

	for _, tc := range []struct {
		name             string
		entry            logproto.Entry
		expectedLogLevel string
	}{
		{
			name: "use severity number from otlp logs",
			entry: logproto.Entry{
				Line: "error",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  loghttp_push.OTLPSeverityNumber,
						Value: fmt.Sprintf("%d", plog.SeverityNumberDebug3),
					},
				},
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "invalid severity number should not cause any issues",
			entry: logproto.Entry{
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  loghttp_push.OTLPSeverityNumber,
						Value: "foo",
					},
				},
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "non otlp without any of the log level keywords in log line",
			entry: logproto.Entry{
				Line: "foo",
			},
			expectedLogLevel: constants.LogLevelUnknown,
		},
		{
			name: "non otlp with log level keywords in log line",
			entry: logproto.Entry{
				Line: "this is a warning log",
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword error but it should not get picked up","level":"critical"}`,
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"FOO":"bar","MSG":"message with keyword error but it should not get picked up","LEVEL":"Critical"}`,
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","level":"warn"}`,
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","SEVERITY":"FATAL"}`,
			},
			expectedLogLevel: constants.LogLevelFatal,
		},
		{
			name: "json log line with an error in block case",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","level":"ERR"}`,
			},
			expectedLogLevel: constants.LogLevelError,
		},
		{
			name: "json log line with an INFO in block case",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword INFO get picked up"}`,
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "logfmt log line with an INFO and not level returns info log level",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with info and not level should get picked up"`,
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "logfmt log line with a warn",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=warn`,
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "logfmt log line with a warn with camel case",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=Warn`,
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "logfmt log line with a trace",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=Trace`,
			},
			expectedLogLevel: constants.LogLevelTrace,
		},
		{
			name: "logfmt log line with some other level returns unknown log level",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword but it should not get picked up" level=NA`,
			},
			expectedLogLevel: constants.LogLevelUnknown,
		},
		{
			name: "logfmt log line with label Severity is allowed for level detection",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword but it should not get picked up" severity=critical`,
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "logfmt log line with label Severity with camelcase is allowed for level detection",
			entry: logproto.Entry{
				Line: `Foo=bar MSG="Message with keyword but it should not get picked up" Severity=critical`,
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "logfmt log line with a info with non standard case",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=inFO`,
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "logfmt log line with a info with non block case for level",
			entry: logproto.Entry{
				Line: `FOO=bar MSG="message with keyword error but it should not get picked up" LEVEL=inFO`,
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detectedLogLevel := ld.detectLogLevelFromLogEntry(tc.entry, logproto.FromLabelAdaptersToLabels(tc.entry.StructuredMetadata))
			require.Equal(t, tc.expectedLogLevel, detectedLogLevel)
		})
	}
}

func Test_detectLogLevelFromLogEntryWithCustomLabels(t *testing.T) {
	ld := newFieldDetector(
		validationContext{
			discoverLogLevels:       true,
			allowStructuredMetadata: true,
			logLevelFields:          []string{"log_level", "logging_level", "LOGGINGLVL", "lvl"},
		})

	for _, tc := range []struct {
		name             string
		entry            logproto.Entry
		expectedLogLevel string
	}{
		{
			name: "use severity number from otlp logs",
			entry: logproto.Entry{
				Line: "error",
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  loghttp_push.OTLPSeverityNumber,
						Value: fmt.Sprintf("%d", plog.SeverityNumberDebug3),
					},
				},
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "invalid severity number should not cause any issues",
			entry: logproto.Entry{
				StructuredMetadata: push.LabelsAdapter{
					{
						Name:  loghttp_push.OTLPSeverityNumber,
						Value: "foo",
					},
				},
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "non otlp without any of the log level keywords in log line",
			entry: logproto.Entry{
				Line: "foo",
			},
			expectedLogLevel: constants.LogLevelUnknown,
		},
		{
			name: "non otlp with log level keywords in log line",
			entry: logproto.Entry{
				Line: "this is a warning log",
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword error but it should not get picked up","log_level":"critical"}`,
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"FOO":"bar","MSG":"message with keyword error but it should not get picked up","LOGGINGLVL":"Critical"}`,
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","lvl":"warn"}`,
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","LOGGINGLVL":"FATAL"}`,
			},
			expectedLogLevel: constants.LogLevelFatal,
		},
		{
			name: "json log line with an error in block case",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","logging_level":"ERR"}`,
			},
			expectedLogLevel: constants.LogLevelError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detectedLogLevel := ld.detectLogLevelFromLogEntry(tc.entry, logproto.FromLabelAdaptersToLabels(tc.entry.StructuredMetadata))
			require.Equal(t, tc.expectedLogLevel, detectedLogLevel)
		})
	}
}

func Benchmark_extractLogLevelFromLogLine(b *testing.B) {
	// looks scary, but it is some random text of about 1000 chars from charset a-zA-Z0-9
	logLine := "dGzJ6rKk Zj U04SWEqEK4Uwho8 DpNyLz0 Nfs61HJ fz5iKVigg 44 kabOz7ghviGmVONriAdz4lA 7Kis1OTvZGT3 " +
		"ZB6ioK4fgJLbzm AuIcbnDZKx3rZ aeZJQzRb3zhrn vok8Efav6cbyzbRUQ PYsEdQxCpdCDcGNsKG FVwe61 nhF06t9hXSNySEWa " +
		"gBAXP1J8oEL grep1LfeKjA23ntszKA A772vNyxjQF SjWfJypwI7scxk oLlqRzrDl ostO4CCwx01wDB7Utk0 64A7p5eQDITE6zc3 " +
		"rGL DrPnD K2oj Vro2JEvI2YScstnMx SVu H o GUl8fxZJJ1HY0 C  QOA HNJr5XtsCNRrLi 0w C0Pd8XWbVZyQkSlsRm zFw1lW  " +
		"c8j6JFQuQnnB EyL20z0 2Duo0dvynnAGD 45ut2Z Jrz8Nd7Pmg 5oQ09r9vnmy U2 mKHO5uBfndPnbjbr  mzOvQs9bM1 9e " +
		"yvNSfcbPyhuWvB VKJt2kp8IoTVc XCe Uva5mp9NrGh3TEbjQu1 C  Zvdk uPr7St2m kwwMRcS9eC aS6ZuL48eoQUiKo VBPd4m49ymr " +
		"eQZ0fbjWpj6qA A6rYs4E 58dqh9ntu8baziDJ4c 1q6aVEig YrMXTF hahrlt 6hKVHfZLFZ V 9hEVN0WKgcpu6L zLxo6YC57 XQyfAGpFM " +
		"Wm3 S7if5qCXPzvuMZ2 gNHdst Z39s9uNc58QBDeYRW umyIF BDqEdqhE tAs2gidkqee3aux8b NLDb7 ZZLekc0cQZ GUKQuBg2pL2y1S " +
		"RJtBuW ABOqQHLSlNuUw ZlM2nGS2 jwA7cXEOJhY 3oPv4gGAz  Uqdre16MF92C06jOH dayqTCK8XmIilT uvgywFSfNadYvRDQa " +
		"iUbswJNcwqcr6huw LAGrZS8NGlqqzcD2wFU rm Uqcrh3TKLUCkfkwLm  5CIQbxMCUz boBrEHxvCBrUo YJoF2iyif4xq3q yk "
	ld := &FieldDetector{
		validationContext: validationContext{
			discoverLogLevels:       true,
			allowStructuredMetadata: true,
			logLevelFields:          []string{"level", "LEVEL", "Level", "severity", "SEVERITY", "Severity", "lvl", "LVL", "Lvl"},
		},
	}
	for i := 0; i < b.N; i++ {
		level := ld.extractLogLevelFromLogLine(logLine)
		require.Equal(b, constants.LogLevelUnknown, level)
	}
}

func Benchmark_optParseExtractLogLevelFromLogLineJson(b *testing.B) {
	logLine := `{"msg": "something" , "level": "error", "id": "1"}`

	ld := newFieldDetector(
		validationContext{
			discoverLogLevels:       true,
			allowStructuredMetadata: true,
			logLevelFields:          []string{"level", "LEVEL", "Level", "severity", "SEVERITY", "Severity", "lvl", "LVL", "Lvl"},
		})

	for i := 0; i < b.N; i++ {
		level := ld.extractLogLevelFromLogLine(logLine)
		require.Equal(b, constants.LogLevelError, level)
	}
}

func Benchmark_optParseExtractLogLevelFromLogLineLogfmt(b *testing.B) {
	logLine := `FOO=bar MSG="message with keyword error but it should not get picked up" LEVEL=inFO`
	ld := newFieldDetector(
		validationContext{
			discoverLogLevels:       true,
			allowStructuredMetadata: true,
			logLevelFields:          []string{"level", "LEVEL", "Level", "severity", "SEVERITY", "Severity", "lvl", "LVL", "Lvl"},
		})

	for i := 0; i < b.N; i++ {
		level := ld.extractLogLevelFromLogLine(logLine)
		require.Equal(b, constants.LogLevelInfo, level)
	}
}

func Test_DetectGenericFields_Enabled(t *testing.T) {
	t.Run("disabled if map is empty", func(t *testing.T) {
		detector := newFieldDetector(
			validationContext{
				discoverGenericFields:   make(map[string][]string, 0),
				allowStructuredMetadata: true,
			})
		require.False(t, detector.shouldDiscoverGenericFields())
	})
	t.Run("disabled if structured metadata is not allowed", func(t *testing.T) {
		detector := newFieldDetector(
			validationContext{
				discoverGenericFields:   map[string][]string{"trace_id": {"trace_id", "TRACE_ID"}},
				allowStructuredMetadata: false,
			})
		require.False(t, detector.shouldDiscoverGenericFields())
	})
	t.Run("enabled if structured metadata is allowed and map is not empty", func(t *testing.T) {
		detector := newFieldDetector(
			validationContext{
				discoverGenericFields:   map[string][]string{"trace_id": {"trace_id", "TRACE_ID"}},
				allowStructuredMetadata: true,
			})
		require.True(t, detector.shouldDiscoverGenericFields())
	})
}

func Test_DetectGenericFields(t *testing.T) {

	detector := newFieldDetector(
		validationContext{
			discoverGenericFields: map[string][]string{
				"trace_id":   {"trace_id"},
				"org_id":     {"org_id", "user_id", "tenant_id"},
				"product_id": {"product.id"}, // jsonpath
			},
			allowStructuredMetadata: true,
		})

	for _, tc := range []struct {
		name     string
		labels   labels.Labels
		entry    logproto.Entry
		expected push.LabelsAdapter
	}{
		{
			name: "no match",
			labels: labels.Labels{
				{Name: "env", Value: "prod"},
			},
			entry: push.Entry{
				Line:               "log line does not match",
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{},
		},
		{
			name: "stream label matches",
			labels: labels.Labels{
				{Name: "trace_id", Value: "8c5f2ecbade6f01d"},
				{Name: "tenant_id", Value: "fake"},
			},
			entry: push.Entry{
				Line:               "log line does not match",
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{
				{Name: "trace_id", Value: "8c5f2ecbade6f01d"},
				{Name: "org_id", Value: "fake"},
			},
		},
		{
			name: "metadata matches",
			labels: labels.Labels{
				{Name: "env", Value: "prod"},
			},
			entry: push.Entry{
				Line: "log line does not match",
				StructuredMetadata: push.LabelsAdapter{
					{Name: "trace_id", Value: "8c5f2ecbade6f01d"},
					{Name: "user_id", Value: "fake"},
				},
			},
			expected: push.LabelsAdapter{
				{Name: "trace_id", Value: "8c5f2ecbade6f01d"},
				{Name: "org_id", Value: "fake"},
			},
		},
		{
			name: "logline (logfmt) matches",
			labels: labels.Labels{
				{Name: "env", Value: "prod"},
			},
			entry: push.Entry{
				Line:               `msg="this log line matches" trace_id="8c5f2ecbade6f01d" org_id=fake duration=1h`,
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{
				{Name: "trace_id", Value: "8c5f2ecbade6f01d"},
				{Name: "org_id", Value: "fake"},
			},
		},
		{
			name: "logline (logfmt) matches multiple fields",
			labels: labels.Labels{
				{Name: "env", Value: "prod"},
			},
			entry: push.Entry{
				Line:               `msg="this log line matches" tenant_id="fake_a" org_id=fake_b duration=1h`,
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{
				{Name: "org_id", Value: "fake_b"}, // first field from configuration that matches takes precedence
			},
		},
		{
			name: "logline (json) matches",
			labels: labels.Labels{
				{Name: "env", Value: "prod"},
			},
			entry: push.Entry{
				Line:               `{"msg": "this log line matches", "trace_id": "8c5f2ecbade6f01d", "org_id": "fake", "duration": "1s"}`,
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{
				{Name: "trace_id", Value: "8c5f2ecbade6f01d"},
				{Name: "org_id", Value: "fake"},
			},
		},
		{
			name: "logline (json) matches multiple fields",
			labels: labels.Labels{
				{Name: "env", Value: "prod"},
			},
			entry: push.Entry{
				Line:               `{"msg": "this log line matches", "tenant_id": "fake_a", "org_id": "fake_b", "duration": "1s"}`,
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{
				{Name: "org_id", Value: "fake_b"}, // first field from configuration that matches takes precedence
			},
		},
		{
			name: "logline matches jsonpath",
			labels: labels.Labels{
				{Name: "env", Value: "prod"},
			},
			entry: push.Entry{
				Line:               `{"product": {"details": "product details", "id": "P2024/01"}}`,
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{
				{Name: "product_id", Value: "P2024/01"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extracted := push.LabelsAdapter{}
			metadata := logproto.FromLabelAdaptersToLabels(tc.entry.StructuredMetadata)
			for name, hints := range detector.validationContext.discoverGenericFields {
				field, ok := detector.extractGenericField(name, hints, tc.labels, metadata, tc.entry)
				if ok {
					extracted = append(extracted, field)
				}
			}
			require.ElementsMatch(t, tc.expected, extracted)
		})
	}
}
