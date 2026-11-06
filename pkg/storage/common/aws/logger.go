package aws

import (
	"fmt"

	"github.com/aws/smithy-go/logging"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// sdkLogger routes aws-sdk-go-v2 log output through a go-kit logger so Loki's log level applies to it.
type sdkLogger struct {
	logger log.Logger
}

// NewSDKLogger returns a smithy logging.Logger that writes to logger at the matching go-kit level.
func NewSDKLogger(logger log.Logger) logging.Logger {
	return sdkLogger{logger: log.With(logger, "component", "aws-sdk")}
}

func (s sdkLogger) Logf(classification logging.Classification, format string, v ...interface{}) {
	var l log.Logger
	switch classification {
	case logging.Warn:
		l = level.Warn(s.logger)
	case logging.Debug:
		l = level.Debug(s.logger)
	default:
		l = level.Info(s.logger)
	}
	_ = l.Log("msg", fmt.Sprintf(format, v...))
}
