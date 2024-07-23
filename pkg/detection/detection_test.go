package detection

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"

	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

func Test_detectLogLevelFromLogEntry(t *testing.T) {
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
			expectedLogLevel: LogLevelDebug,
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
			expectedLogLevel: LogLevelInfo,
		},
		{
			name: "non otlp without any of the log level keywords in log line",
			entry: logproto.Entry{
				Line: "foo",
			},
			expectedLogLevel: LogLevelUnknown,
		},
		{
			name: "non otlp with log level keywords in log line",
			entry: logproto.Entry{
				Line: "this is a warning log",
			},
			expectedLogLevel: LogLevelWarn,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword error but it should not get picked up","level":"critical"}`,
			},
			expectedLogLevel: LogLevelCritical,
		},
		{
			name: "json log line with an error",
			entry: logproto.Entry{
				Line: `{"FOO":"bar","MSG":"message with keyword error but it should not get picked up","LEVEL":"Critical"}`,
			},
			expectedLogLevel: LogLevelCritical,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","level":"warn"}`,
			},
			expectedLogLevel: LogLevelWarn,
		},
		{
			name: "json log line with an warning",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","SEVERITY":"FATAL"}`,
			},
			expectedLogLevel: LogLevelFatal,
		},
		{
			name: "json log line with an error in block case",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword warn but it should not get picked up","level":"ERR"}`,
			},
			expectedLogLevel: LogLevelError,
		},
		{
			name: "json log line with an INFO in block case",
			entry: logproto.Entry{
				Line: `{"foo":"bar","msg":"message with keyword INFO get picked up"}`,
			},
			expectedLogLevel: LogLevelInfo,
		},
		{
			name: "logfmt log line with an INFO and not level returns info log level",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with info and not level should get picked up"`,
			},
			expectedLogLevel: LogLevelInfo,
		},
		{
			name: "logfmt log line with a warn",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=warn`,
			},
			expectedLogLevel: LogLevelWarn,
		},
		{
			name: "logfmt log line with a warn with camel case",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=Warn`,
			},
			expectedLogLevel: LogLevelWarn,
		},
		{
			name: "logfmt log line with a trace",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=Trace`,
			},
			expectedLogLevel: LogLevelTrace,
		},
		{
			name: "logfmt log line with some other level returns unknown log level",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword but it should not get picked up" level=NA`,
			},
			expectedLogLevel: LogLevelUnknown,
		},
		{
			name: "logfmt log line with label Severity is allowed for level detection",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword but it should not get picked up" severity=critical`,
			},
			expectedLogLevel: LogLevelCritical,
		},
		{
			name: "logfmt log line with label Severity with camelcase is allowed for level detection",
			entry: logproto.Entry{
				Line: `Foo=bar MSG="Message with keyword but it should not get picked up" Severity=critical`,
			},
			expectedLogLevel: LogLevelCritical,
		},
		{
			name: "logfmt log line with a info with non standard case",
			entry: logproto.Entry{
				Line: `foo=bar msg="message with keyword error but it should not get picked up" level=inFO`,
			},
			expectedLogLevel: LogLevelInfo,
		},
		{
			name: "logfmt log line with a info with non block case for level",
			entry: logproto.Entry{
				Line: `FOO=bar MSG="message with keyword error but it should not get picked up" LEVEL=inFO`,
			},
			expectedLogLevel: LogLevelInfo,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detectedLogLevel := DetectLogLevelFromLogEntry(tc.entry, logproto.FromLabelAdaptersToLabels(tc.entry.StructuredMetadata))
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

	for i := 0; i < b.N; i++ {
		level := extractLogLevelFromLogLine(logLine)
		require.Equal(b, LogLevelUnknown, level)
	}
}

func Benchmark_optParseExtractLogLevelFromLogLineJson(b *testing.B) {
	logLine := `{"msg": "something" , "level": "error", "id": "1"}`

	for i := 0; i < b.N; i++ {
		level := extractLogLevelFromLogLine(logLine)
		require.Equal(b, LogLevelError, level)
	}
}

func Benchmark_optParseExtractLogLevelFromLogLineLogfmt(b *testing.B) {
	logLine := `FOO=bar MSG="message with keyword error but it should not get picked up" LEVEL=inFO`

	for i := 0; i < b.N; i++ {
		level := extractLogLevelFromLogLine(logLine)
		require.Equal(b, LogLevelInfo, level)
	}
}
