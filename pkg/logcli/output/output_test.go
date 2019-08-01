package output

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewLogOutput(t *testing.T) {
	options := &LogOutputOptions{time.UTC, false}

	out, err := NewLogOutput("default", options)
	assert.NoError(t, err)
	assert.IsType(t, &DefaultOutput{options}, out)

	out, err = NewLogOutput("jsonl", options)
	assert.NoError(t, err)
	assert.IsType(t, &JSONLOutput{options}, out)

	out, err = NewLogOutput("raw", options)
	assert.NoError(t, err)
	assert.IsType(t, &RawOutput{options}, out)

	out, err = NewLogOutput("unknown", options)
	assert.Error(t, err)
	assert.Nil(t, out)
}
