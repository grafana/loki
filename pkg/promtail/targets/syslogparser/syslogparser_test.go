package syslogparser_test

import (
	"io"
	"strings"
	"testing"

	"github.com/grafana/loki/pkg/promtail/targets/syslogparser"
	"github.com/influxdata/go-syslog"
	"github.com/stretchr/testify/require"
)

func TestParseStream_OctetCounting(t *testing.T) {
	r := strings.NewReader("23 <13>1 - - - - - - First24 <13>1 - - - - - - Second")

	resultsC := syslogparser.ParseStream(r)
	results := make([]*syslog.Result, 0)
	for res := range resultsC {
		results = append(results, res)
	}

	require.Equal(t, 2, len(results))
	require.NoError(t, results[0].Error)
	require.Equal(t, "First", *results[0].Message.Message())
	require.NoError(t, results[1].Error)
	require.Equal(t, "Second", *results[1].Message.Message())
}

func TestParseStream_NewlineSeparated(t *testing.T) {
	r := strings.NewReader("<13>1 - - - - - - First\n<13>1 - - - - - - Second")

	resultsC := syslogparser.ParseStream(r)
	results := make([]*syslog.Result, 0)
	for res := range resultsC {
		results = append(results, res)
	}

	require.Equal(t, 2, len(results))
	require.NoError(t, results[0].Error)
	require.Equal(t, "First", *results[0].Message.Message())
	require.NoError(t, results[1].Error)
	require.Equal(t, "Second", *results[1].Message.Message())
}

func TestParseStream_InvalidStream(t *testing.T) {
	r := strings.NewReader("invalid")

	resultsC := syslogparser.ParseStream(r)
	require.EqualError(t, (<-resultsC).Error, "invalid or unsupported framing. first byte: 'i'")

	_, ok := <-resultsC
	require.Equal(t, false, ok, "channel should be closed after unrecoverable error")
}

func TestParseStream_EmptyStream(t *testing.T) {
	r := strings.NewReader("")

	resultsC := syslogparser.ParseStream(r)
	require.Equal(t, (<-resultsC).Error, io.EOF)

	_, ok := <-resultsC
	require.Equal(t, false, ok, "channel should be closed after unrecoverable error")
}
