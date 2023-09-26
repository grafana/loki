package writer

import (
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type StreamWriter struct {
	w      io.Writer
	logger log.Logger
}

func NewStreamWriter(w io.Writer, logger log.Logger) *StreamWriter {
	return &StreamWriter{
		w:      w,
		logger: logger,
	}
}

func (s *StreamWriter) WriteEntry(ts time.Time, entry string) {
	_, err := fmt.Fprint(s.w, entry)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to write log entry", "entry", ts, "error", err)
	}
}

func (s *StreamWriter) Stop() {}
