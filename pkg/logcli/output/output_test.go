package output

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLogOutput(t *testing.T) {
	options := &LogOutputOptions{time.UTC, false, false}

	out, err := NewLogOutput(nil,"default", options)
	assert.NoError(t, err)
	assert.IsType(t, &DefaultOutput{nil,options}, out)

	out, err = NewLogOutput(nil,"jsonl", options)
	assert.NoError(t, err)
	assert.IsType(t, &JSONLOutput{nil,options}, out)

	out, err = NewLogOutput(nil,"raw", options)
	assert.NoError(t, err)
	assert.IsType(t, &RawOutput{nil,options}, out)

	out, err = NewLogOutput(nil,"unknown", options)
	assert.Error(t, err)
	assert.Nil(t, out)
}
