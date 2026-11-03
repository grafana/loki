package output

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

var errWriteFailed = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errWriteFailed
}

func TestNewLogOutput(t *testing.T) {
	options := &LogOutputOptions{Timezone: time.UTC, NoLabels: false, ColoredOutput: false}

	out, err := NewLogOutput(nil, "default", options)
	assert.NoError(t, err)
	assert.IsType(t, &DefaultOutput{nil, options}, out)
	assert.Equal(t, time.RFC3339, out.(*DefaultOutput).options.TimestampFormat)

	out, err = NewLogOutput(nil, "jsonl", options)
	assert.NoError(t, err)
	assert.IsType(t, &JSONLOutput{nil, options}, out)

	out, err = NewLogOutput(nil, "raw", options)
	assert.NoError(t, err)
	assert.IsType(t, &RawOutput{nil, options}, out)

	out, err = NewLogOutput(nil, "unknown", options)
	assert.Error(t, err)
	assert.Nil(t, out)
}

func TestLogOutputsPropagateWriterError(t *testing.T) {
	for _, mode := range []string{"default", "jsonl", "raw"} {
		t.Run(mode, func(t *testing.T) {
			out, err := NewLogOutput(failingWriter{}, mode, &LogOutputOptions{Timezone: time.UTC})
			require.NoError(t, err)

			err = out.FormatAndPrintln(time.Time{}, loghttp.LabelSet{"app": "loki"}, 0, "line")
			require.ErrorIs(t, err, errWriteFailed)
		})
	}
}
