package aws

import (
	"bytes"
	"testing"

	"github.com/aws/smithy-go/logging"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/stretchr/testify/require"
)

func TestSDKLoggerLevelMapping(t *testing.T) {
	tests := map[string]struct {
		classification logging.Classification
		expectedLevel  string
	}{
		"warn":    {classification: logging.Warn, expectedLevel: "level=warn"},
		"debug":   {classification: logging.Debug, expectedLevel: "level=debug"},
		"unknown": {classification: "TRACE", expectedLevel: "level=info"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			NewSDKLogger(log.NewLogfmtLogger(&buf)).Logf(tc.classification, "checksum %s", "skipped")

			out := buf.String()
			require.Contains(t, out, tc.expectedLevel)
			require.Contains(t, out, `msg="checksum skipped"`)
			require.Contains(t, out, "component=aws-sdk")
		})
	}
}

func TestSDKLoggerRespectsLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	logger := NewSDKLogger(level.NewFilter(log.NewLogfmtLogger(&buf), level.AllowError()))

	logger.Logf(logging.Warn, "Response has no supported checksum. Not validating response payload.")
	logger.Logf(logging.Debug, "request sent")
	require.Empty(t, buf.String())
}
