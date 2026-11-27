package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var errFormatWrite = errors.New("write failed")

type failingFormatWriter struct{}

func (failingFormatWriter) Write([]byte) (int, error) {
	return 0, errFormatWrite
}

func TestFormatLogQLWriteError(t *testing.T) {
	query := strings.NewReader(`{app="test"}`)

	err := formatLogQL(query, failingFormatWriter{})

	require.ErrorIs(t, err, errFormatWrite)
}
