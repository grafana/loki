package ckit

import (
	"bytes"
	"io"
	golog "log"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

var (
	errPrefix        = []byte("[ERR]")
	warnPrefix       = []byte("[WARN]")
	infoPrefix       = []byte("[INFO]")
	debugPrefix      = []byte("[DEBUG]")
	memberListPrefix = []byte("memberlist: ")
)

// memberListOutputLogger will do best-effort classification of the logging level that memberlist uses in its log lines
// and use the corresponding level when logging with logger. It will drop redundant `[<level>] memberlist:` parts.
// This helps us surface only desired log messages from memberlist. If classification fails, info level is used as
// a fallback. See tests for detailed behaviour.
type memberListOutputLogger struct {
	logger log.Logger
}

var _ io.Writer = (*memberListOutputLogger)(nil)

func newMemberListLogger(logger log.Logger) *golog.Logger {
	return golog.New(&memberListOutputLogger{logger: logger}, "", 0)
}

func (m *memberListOutputLogger) Write(p []byte) (int, error) {
	var err error

	sanitizeFn := func(dropPrefix []byte, msg []byte) string {
		noLevel := bytes.TrimSpace(bytes.TrimPrefix(msg, dropPrefix))
		return string(bytes.TrimPrefix(noLevel, memberListPrefix))
	}

	switch {
	case bytes.HasPrefix(p, errPrefix):
		err = level.Error(m.logger).Log("msg", sanitizeFn(errPrefix, p))
	case bytes.HasPrefix(p, warnPrefix):
		err = level.Warn(m.logger).Log("msg", sanitizeFn(warnPrefix, p))
	case bytes.HasPrefix(p, infoPrefix):
		err = level.Info(m.logger).Log("msg", sanitizeFn(infoPrefix, p))
	case bytes.HasPrefix(p, debugPrefix):
		err = level.Debug(m.logger).Log("msg", sanitizeFn(debugPrefix, p))
	default:
		err = level.Info(m.logger).Log("msg", sanitizeFn(nil, p))
	}

	if err != nil {
		return 0, err
	}
	return len(p), nil
}
