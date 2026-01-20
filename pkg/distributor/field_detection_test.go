package distributor

import (
	"fmt"
	"strings"
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

	t.Run("detected_level structured metadata takes precedence over other level detection methods", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		// Create a write request with multiple potential level sources:
		// 1. A JSON log line with level=error
		// 2. A log level in stream labels (level=debug)
		// 3. OTLP severity number in structured metadata
		// 4. A severity field in structured metadata
		// 5. The detected_level field in structured metadata (should take precedence)
		writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar", level="debug"}`}, false, false, false)
		writeReq.Streams[0].Entries[0].Line = `{"msg":"this is a test message", "level":"error"}`
		writeReq.Streams[0].Entries[0].StructuredMetadata = push.LabelsAdapter{
			{
				Name:  loghttp_push.OTLPSeverityNumber,
				Value: fmt.Sprintf("%d", plog.SeverityNumberWarn),
			},
			{
				Name:  "severity",
				Value: constants.LogLevelCritical,
			},
			{
				Name:  constants.LevelLabel, // detected_level
				Value: constants.LogLevelTrace,
			},
		}

		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{foo="bar", level="debug"}`, topVal.Streams[0].Labels)

		// Verify that detected_level from structured metadata is preserved and used
		sm := topVal.Streams[0].Entries[0].StructuredMetadata

		detectedLevelLbls := make([]logproto.LabelAdapter, 0, len(sm))
		for _, sm := range sm {
			if sm.Name == constants.LevelLabel {
				detectedLevelLbls = append(detectedLevelLbls, sm)
			}
		}

		require.Len(t, detectedLevelLbls, 1)
		require.Contains(t, detectedLevelLbls, logproto.LabelAdapter{
			Name:  constants.LevelLabel,
			Value: constants.LogLevelTrace,
		})
	})

	t.Run("detected_level with mixed case value gets normalized to lowercase", func(t *testing.T) {
		// Test various mixed case values
		testCases := []struct {
			input    string
			expected string
		}{
			{"WaRn", constants.LogLevelWarn},
			{"InFo", constants.LogLevelInfo},
			{"CRITICAL", constants.LogLevelCritical},
			{"Debug", constants.LogLevelDebug},
			{"FaTaL", constants.LogLevelFatal},
			{"tRaCe", constants.LogLevelTrace},
			{"ERROR", constants.LogLevelError},
		}

		for _, tc := range testCases {
			// Create a fresh setup for each test case
			limits, ingester := setup(true)
			distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

			writeReq := makeWriteRequestWithLabels(1, 10, []string{`{foo="bar"}`}, false, false, false)
			writeReq.Streams[0].Entries[0].Line = `test log`
			writeReq.Streams[0].Entries[0].StructuredMetadata = push.LabelsAdapter{
				{
					Name:  constants.LevelLabel,
					Value: tc.input,
				},
			}

			_, err := distributors[0].Push(ctx, writeReq)
			require.NoError(t, err)
			topVal := ingester.Peek()

			sm := topVal.Streams[0].Entries[0].StructuredMetadata
			require.Len(t, sm, 1)
			require.Equal(t, constants.LevelLabel, sm[0].Name)
			require.Equal(t, tc.expected, sm[0].Value, "Input %q should normalize to %q", tc.input, tc.expected)
		}
	})

	t.Run("level from stream labels gets normalized to lowercase in detected_level", func(t *testing.T) {
		// Test various mixed case values in stream labels
		testCases := []struct {
			streamLabel string
			expected    string
		}{
			{`{foo="bar", level="ERROR"}`, constants.LogLevelError},
			{`{foo="bar", level="WaRn"}`, constants.LogLevelWarn},
			{`{foo="bar", level="InFo"}`, constants.LogLevelInfo},
			{`{foo="bar", level="CRITICAL"}`, constants.LogLevelCritical},
			{`{foo="bar", level="Debug"}`, constants.LogLevelDebug},
			{`{foo="bar", level="FaTaL"}`, constants.LogLevelFatal},
			{`{foo="bar", level="tRaCe"}`, constants.LogLevelTrace},
		}

		for _, tc := range testCases {
			// Create a fresh setup for each test case
			limits, ingester := setup(true)
			distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

			writeReq := makeWriteRequestWithLabels(1, 10, []string{tc.streamLabel}, false, false, false)
			writeReq.Streams[0].Entries[0].Line = `log message without level`

			_, err := distributors[0].Push(ctx, writeReq)
			require.NoError(t, err)
			topVal := ingester.Peek()

			// Verify that detected_level is normalized to lowercase
			sm := topVal.Streams[0].Entries[0].StructuredMetadata
			require.Len(t, sm, 1, "Expected detected_level in structured metadata for stream label %s", tc.streamLabel)
			require.Equal(t, constants.LevelLabel, sm[0].Name)
			require.Equal(t, tc.expected, sm[0].Value, "Stream label %q should normalize to %q in detected_level", tc.streamLabel, tc.expected)
		}
	})

	t.Run("indexed OTEL severity takes precedence over structured metadata or log line", func(t *testing.T) {
		limits, ingester := setup(true)
		distributors, _ := prepare(t, 1, 5, limits, func(_ string) (ring_client.PoolClient, error) { return ingester, nil })

		writeReq := makeWriteRequestWithLabels(2, 10, []string{`{foo="bar", SeverityText="debug"}`}, false, false, false)
		writeReq.Streams[0].Entries[0].Line = `{"msg":"this is a test message", "level":"error"}`
		writeReq.Streams[0].Entries[0].StructuredMetadata = push.LabelsAdapter{
			{
				Name:  loghttp_push.OTLPSeverityNumber,
				Value: fmt.Sprintf("%d", plog.SeverityNumberWarn),
			},
		}
		writeReq.Streams[0].Entries[1].Line = `{"msg":"this is another message", "level":"trace"}`
		writeReq.Streams[0].Entries[1].StructuredMetadata = push.LabelsAdapter{
			{
				Name:  loghttp_push.OTLPSeverityText,
				Value: constants.LogLevelInfo,
			},
		}

		_, err := distributors[0].Push(ctx, writeReq)
		require.NoError(t, err)
		topVal := ingester.Peek()
		require.Equal(t, `{SeverityText="debug", foo="bar"}`, topVal.Streams[0].Labels)

		// Verify that detected_level from structured metadata is preserved and used
		sm := topVal.Streams[0].Entries[0].StructuredMetadata

		detectedLevelLbls := make([]logproto.LabelAdapter, 0, len(sm))
		for _, sm := range sm {
			if sm.Name == constants.LevelLabel {
				detectedLevelLbls = append(detectedLevelLbls, sm)
			}
		}

		require.Len(t, detectedLevelLbls, 1)
		require.Contains(t, detectedLevelLbls, logproto.LabelAdapter{
			Name:  constants.LevelLabel,
			Value: constants.LogLevelDebug,
		})

		// Verify that detected_level from structured metadata is preserved and used
		sm2 := topVal.Streams[0].Entries[1].StructuredMetadata

		detectedLevelLbls2 := make([]logproto.LabelAdapter, 0, len(sm))
		for _, sm := range sm2 {
			if sm.Name == constants.LevelLabel {
				detectedLevelLbls2 = append(detectedLevelLbls2, sm)
			}
		}

		require.Len(t, detectedLevelLbls2, 1)
		require.Contains(t, detectedLevelLbls2, logproto.LabelAdapter{
			Name:  constants.LevelLabel,
			Value: constants.LogLevelDebug,
		})
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
			name: "non otlp with debug keyword in log line",
			entry: logproto.Entry{
				Line: "this is a debug message",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with DEBUG keyword in log line",
			entry: logproto.Entry{
				Line: "this is a DEBUG message",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with debug: prefix in log line",
			entry: logproto.Entry{
				Line: "debug: something happened",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with DEBUG: prefix in log line",
			entry: logproto.Entry{
				Line: "DEBUG: something happened",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with critical keyword in log line",
			entry: logproto.Entry{
				Line: "this is a critical message",
			},
			expectedLogLevel: constants.LogLevelCritical, // Changed from Unknown to Critical
		},
		{
			name: "non otlp with CRITICAL keyword in log line",
			entry: logproto.Entry{
				Line: "this is a CRITICAL message",
			},
			expectedLogLevel: constants.LogLevelCritical, // Changed from Unknown to Critical
		},
		{
			name: "non otlp with critical: prefix in log line",
			entry: logproto.Entry{
				Line: "critical: something happened",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with CRITICAL: prefix in log line",
			entry: logproto.Entry{
				Line: "CRITICAL: something happened",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with [debug] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[debug] this is a debug message",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with [DEBUG] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[DEBUG] this is a debug message",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with [critical] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[critical] this is a critical message",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with [CRITICAL] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[CRITICAL] this is a critical message",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with [info] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[info] this is an info message",
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "non otlp with [INFO] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[INFO] this is an info message",
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "non otlp with [warn] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[warn] this is a warning message",
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "non otlp with [WARNING] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[WARNING] this is a warning message",
			},
			expectedLogLevel: constants.LogLevelWarn,
		},
		{
			name: "non otlp with [error] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[error] this is an error message",
			},
			expectedLogLevel: constants.LogLevelError,
		},
		{
			name: "non otlp with [ERROR] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[ERROR] this is an error message",
			},
			expectedLogLevel: constants.LogLevelError,
		},
		{
			name: "non otlp with [err] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[err] this is an error message",
			},
			expectedLogLevel: constants.LogLevelError,
		},
		{
			name: "non otlp with [ERR] bracket pattern in log line",
			entry: logproto.Entry{
				Line: "[ERR] this is an error message",
			},
			expectedLogLevel: constants.LogLevelError,
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
		{
			name: "logfmt log line with a info with short level",
			entry: logproto.Entry{
				Line: `FOO=bar MSG="message that should qualify to unknown when there is no level defined" LEVEL=Inf`,
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
		{
			name: "logfmt log line with a info with full level",
			entry: logproto.Entry{
				Line: `FOO=bar MSG="message that should qualify to unknown when there is no level defined" LEVEL=Information`,
			},
			expectedLogLevel: constants.LogLevelInfo,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detectedLogLevel := ld.detectLogLevelFromLogEntry(tc.entry, logproto.FromLabelAdaptersToLabels(tc.entry.StructuredMetadata))
			require.Equal(t, tc.expectedLogLevel, detectedLogLevel, "log line: %s", tc.entry.Line)
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
			name: "non otlp with debug keyword in log line",
			entry: logproto.Entry{
				Line: "this is a debug message",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with DEBUG keyword in log line",
			entry: logproto.Entry{
				Line: "this is a DEBUG message",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with debug: prefix in log line",
			entry: logproto.Entry{
				Line: "debug: something happened",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with DEBUG: prefix in log line",
			entry: logproto.Entry{
				Line: "DEBUG: something happened",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with critical keyword in log line",
			entry: logproto.Entry{
				Line: "this is a critical message",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with CRITICAL keyword in log line",
			entry: logproto.Entry{
				Line: "this is a CRITICAL message",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with critical: prefix in log line",
			entry: logproto.Entry{
				Line: "critical: something happened",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with CRITICAL: prefix in log line",
			entry: logproto.Entry{
				Line: "CRITICAL: something happened",
			},
			expectedLogLevel: constants.LogLevelCritical,
		},
		{
			name: "non otlp with [debug] bracket pattern in custom test",
			entry: logproto.Entry{
				Line: "[debug] this is a debug message with custom fields",
			},
			expectedLogLevel: constants.LogLevelDebug,
		},
		{
			name: "non otlp with [CRITICAL] bracket pattern in custom test",
			entry: logproto.Entry{
				Line: "[CRITICAL] this is a critical message with custom fields",
			},
			expectedLogLevel: constants.LogLevelCritical,
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
	tests := map[string]string{
		"level field at start":      `{"level": "error", "field1": "value1", "field2": "value2", "field3": "value3", "field4": "value4", "field5": "value5", "field6": "value6", "field7": "value7", "field8": "value8", "field9": "value9"}`,
		"level field in middle":     `{"field1": "value1", "field2": "value2", "field3": "value3", "field4": "value4", "level": "error", "field5": "value5", "field6": "value6", "field7": "value7", "field8": "value8", "field9": "value9"}`,
		"level field at end":        `{"field1": "value1", "field2": "value2", "field3": "value3", "field4": "value4", "field5": "value5", "field6": "value6", "field7": "value7", "field8": "value8", "field9": "value9", "level": "error"}`,
		"no level field":            `{"field1": "value1", "field2": "value2", "field3": "value3", "field4": "value4", "field5": "value5", "field6": "value6", "field7": "value7", "field8": "value8", "field9": "value9"}`,
		"nested level field":        `{"metadata": {"level": "error"}, "field1": "value1", "field2": "value2", "field3": "value3", "field4": "value4", "field5": "value5", "field6": "value6", "field7": "value7", "field8": "value8", "field9": "value9"}`,
		"deeply nested level field": `{"a": {"b": {"c": {"level": "error"}}}, "field1": "value1", "field2": "value2", "field3": "value3", "field4": "value4", "field5": "value5", "field6": "value6", "field7": "value7", "field8": "value8", "field9": "value9"}`,
	}
	ld := newFieldDetector(
		validationContext{
			discoverLogLevels:       true,
			allowStructuredMetadata: true,
			logLevelFields:          []string{"level", "LEVEL", "Level", "severity", "SEVERITY", "Severity", "lvl", "LVL", "Lvl"},
		})

	for name, logLine := range tests {
		b.Run(name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = ld.extractLogLevelFromLogLine(logLine)
			}
		})
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
			labels: labels.FromStrings(
				"env", "prod",
			),
			entry: push.Entry{
				Line:               "log line does not match",
				StructuredMetadata: push.LabelsAdapter{},
			},
			expected: push.LabelsAdapter{},
		},
		{
			name: "stream label matches",
			labels: labels.FromStrings(
				"trace_id", "8c5f2ecbade6f01d",
				"tenant_id", "fake",
			),
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
			labels: labels.FromStrings(
				"env", "prod",
			),
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
			labels: labels.FromStrings(
				"env", "prod",
			),
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
			labels: labels.FromStrings(
				"env", "prod",
			),
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
			labels: labels.FromStrings(
				"env", "prod",
			),
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
			labels: labels.FromStrings(
				"env", "prod",
			),
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
			labels: labels.FromStrings(
				"env", "prod",
			),
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

func TestGetLevelUsingJsonParser(t *testing.T) {
	tests := []struct {
		name               string
		json               string
		allowedLevelFields map[string]struct{}
		maxDepth           int
		want               string
	}{
		{
			name:               "simple top level field",
			json:               `{"level": "error"}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			want:               "error",
		},
		{
			name:               "nested field one level deep",
			json:               `{"a": {"level": "info"}}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			want:               "info",
		},
		{
			name:               "deeply nested field",
			json:               `{"a": {"b": {"c": {"level": "warn"}}}}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			want:               "warn",
		},
		{
			name:               "multiple allowed fields picks first",
			json:               `{"severity": "error", "level": "info"}`,
			allowedLevelFields: map[string]struct{}{"level": {}, "severity": {}},
			want:               "error",
		},
		{
			name:               "multiple nested fields picks first",
			json:               `{"a": {"level": "error"}, "b": {"level": "info"}}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			want:               "error",
		},
		{
			name:               "array values are ignored",
			json:               `{"arr": [{"level": "debug"}], "level": "info"}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			want:               "info",
		},
		{
			name:               "non-string values are ignored",
			json:               `{"level": 123, "severity": "warn"}`,
			allowedLevelFields: map[string]struct{}{"level": {}, "severity": {}},
			want:               "warn",
		},
		{
			name:               "empty when no match",
			json:               `{"foo": "bar"}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			want:               "",
		},
		{
			name:               "empty for invalid json",
			json:               `{"foo": "bar"`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			want:               "",
		},
		{
			name:               "custom field names",
			json:               `{"custom_level": "error", "log_severity": "warn"}`,
			allowedLevelFields: map[string]struct{}{"custom_level": {}, "log_severity": {}},
			want:               "error",
		},
		// Adding depth-specific test cases
		{
			name:               "depth limited - only top level",
			json:               `{"a": {"level": "debug"}, "level": "info"}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			maxDepth:           1,
			want:               "info",
		},
		{
			name:               "depth limited - no match",
			json:               `{"a": {"level": "debug"}}`,
			allowedLevelFields: map[string]struct{}{"level": {}},
			maxDepth:           1,
			want:               "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getLevelUsingJSONParser([]byte(tt.json), tt.allowedLevelFields, tt.maxDepth)
			if string(got) != tt.want {
				t.Errorf("getLevelUsingJsonParser() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func Test_detectLevelFromLogLine(t *testing.T) {
	for _, level := range []struct {
		word  string
		level string
	}{
		{word: "trace", level: constants.LogLevelTrace},
		{word: "debug", level: constants.LogLevelDebug},
		{word: "info", level: constants.LogLevelInfo},
		{word: "warn", level: constants.LogLevelWarn},
		{word: "warning", level: constants.LogLevelWarn},
		{word: "error", level: constants.LogLevelError},
		{word: "err", level: constants.LogLevelError},
		{word: "fatal", level: constants.LogLevelFatal},
		{word: "critical", level: constants.LogLevelCritical},
		{word: "unknown", level: constants.LogLevelUnknown},
	} {
		tests := []struct {
			name     string
			log      string
			expected string
		}{
			{
				name:     fmt.Sprintf("detect %s level", level.word),
				log:      fmt.Sprintf("this is a %s message", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level uppercase", strings.ToUpper(level.word)),
				log:      fmt.Sprintf("this is a %s message", strings.ToUpper(level.word)),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level at start of string", level.word),
				log:      fmt.Sprintf("%s occurred", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level at end of string", level.word),
				log:      fmt.Sprintf("an %s", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level with space before", level.word),
				log:      fmt.Sprintf("this is an %s message", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level with colon", level.word),
				log:      fmt.Sprintf("%s: something happened", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level with punctuation after", level.word),
				log:      fmt.Sprintf("%s, something happened", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level with special characters", level.word),
				log:      fmt.Sprintf("%s! something happened", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level with parentheses", level.word),
				log:      fmt.Sprintf("(%s) something happened", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("detect %s level with brackets", level.word),
				log:      fmt.Sprintf("[%s] something happened", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("nginx like log with %s", level.word),
				log:      fmt.Sprintf("2024-01-01T10:00:00Z %s: connection failed", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("bracket pattern [%s]", level.word),
				log:      fmt.Sprintf("[%s] connection failed", level.word),
				expected: level.level,
			},
			{
				name:     fmt.Sprintf("bracket pattern [%s]", strings.ToUpper(level.word)),
				log:      fmt.Sprintf("[%s] connection failed", strings.ToUpper(level.word)),
				expected: level.level,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := detectLevelFromLogLine(tt.log)
				require.Equal(t, tt.expected, result, "log: %q", tt.log)
			})
		}
	}

	additionalTestCases := []struct {
		name     string
		log      string
		expected string
	}{
		{
			name:     "terror should not match error",
			log:      "this is a terror attack",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "errors should not match error",
			log:      "there were multiple errors",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "error123 should not match error",
			log:      "error123 occurred",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "123error should not match error",
			log:      "123error occurred",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "errorCode should not match error",
			log:      "errorCode is invalid",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "myError should not match error",
			log:      "myError occurred",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "debugging should not match debug",
			log:      "debugging the issue",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "debugger should not match debug",
			log:      "debugger attached",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "information should not match info",
			log:      "information is available",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "informative should not match info",
			log:      "informative message",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "warnings should not match warn",
			log:      "warnings issued",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "criticality should not match critical",
			log:      "criticality assessment",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "fatalism should not match fatal",
			log:      "fatalism philosophy",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "tracer should not match trace",
			log:      "tracer bullet",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "traced should not match trace",
			log:      "traced the issue",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "empty string",
			log:      "",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "no level word",
			log:      "this is a regular log message",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "JSON string with error",
			log:      `{"message": "error occurred"}`,
			expected: constants.LogLevelError,
		},
		{
			name:     "JSON string with error in key",
			log:      `{"error": "something"}`,
			expected: constants.LogLevelError,
		},
		{
			name:     "JSON string with errorCode should not match",
			log:      `{"errorCode": 123}`,
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "logfmt with error",
			log:      `msg="error occurred"`,
			expected: constants.LogLevelError,
		},
		{
			name:     "logfmt with errorCode should not match",
			log:      `errorCode=123`,
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "error with equals sign",
			log:      "error=something",
			expected: constants.LogLevelError,
		},
		{
			name:     "logfmt level",
			log:      "level=error",
			expected: constants.LogLevelError,
		},
		{
			name:     "bracket pattern [terror] should not match error",
			log:      "[terror] attack occurred",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "bracket pattern [errors] should not match error",
			log:      "[errors] occurred",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "bracket pattern [errorCode] should not match error",
			log:      "[errorCode] is invalid",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "bracket pattern [debugging] should not match debug",
			log:      "[debugging] the issue",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "bracket pattern [information] should not match info",
			log:      "[information] is available",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "bracket pattern in JSON string",
			log:      `{"message": "[error] occurred"}`,
			expected: constants.LogLevelError,
		},
		{
			name:     "bracket pattern in logfmt string",
			log:      `msg="[error] occurred"`,
			expected: constants.LogLevelError,
		},
		{
			name:     "error_code should not match error",
			log:      "error_code is 500",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "log_error should not match error",
			log:      "log_error function called",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "trace_id should not match trace",
			log:      "trace_id is abc123",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "stack_trace should not match trace",
			log:      "stack_trace printed",
			expected: constants.LogLevelUnknown,
		},
		{
			name:     "error surrounded by underscores should not match",
			log:      "some_error_code is invalid",
			expected: constants.LogLevelUnknown,
		},
	}
	for _, tt := range additionalTestCases {
		t.Run(tt.name, func(t *testing.T) {
			result := detectLevelFromLogLine(tt.log)
			require.Equal(t, tt.expected, result, "log: %q", tt.log)
		})
	}
}
