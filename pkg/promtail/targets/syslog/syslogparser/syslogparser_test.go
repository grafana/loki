package syslogparser_test

import (
	"io"
	"strings"
	"testing"

	"github.com/influxdata/go-syslog/v3"
	"github.com/influxdata/go-syslog/v3/rfc5424"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/promtail/targets/syslog/syslogparser"
)

func TestParseStream_OctetCounting(t *testing.T) {
	r := strings.NewReader("23 <13>1 - - - - - - First24 <13>1 - - - - - - Second")

	results := make([]*syslog.Result, 0)
	cb := func(res *syslog.Result) {
		results = append(results, res)
	}

	err := syslogparser.ParseStream(r, cb)
	require.NoError(t, err)

	require.Equal(t, 2, len(results))
	require.NoError(t, results[0].Error)
	require.Equal(t, "First", *results[0].Message.(*rfc5424.SyslogMessage).Message)
	require.NoError(t, results[1].Error)
	require.Equal(t, "Second", *results[1].Message.(*rfc5424.SyslogMessage).Message)
}

func TestParseStream_NewlineSeparated(t *testing.T) {
	r := strings.NewReader("<13>1 - - - - - - First\n<13>1 - - - - - - Second\n")

	results := make([]*syslog.Result, 0)
	cb := func(res *syslog.Result) {
		results = append(results, res)
	}

	err := syslogparser.ParseStream(r, cb)
	require.NoError(t, err)

	require.Equal(t, 2, len(results))
	require.NoError(t, results[0].Error)
	require.Equal(t, "First", *results[0].Message.(*rfc5424.SyslogMessage).Message)
	require.NoError(t, results[1].Error)
	require.Equal(t, "Second", *results[1].Message.(*rfc5424.SyslogMessage).Message)
}

func TestParseStream_InvalidStream(t *testing.T) {
	r := strings.NewReader("invalid")

	err := syslogparser.ParseStream(r, func(res *syslog.Result) {})
	require.EqualError(t, err, "invalid or unsupported framing. first byte: 'i'")
}

func TestParseStream_EmptyStream(t *testing.T) {
	r := strings.NewReader("")

	err := syslogparser.ParseStream(r, func(res *syslog.Result) {})
	require.Equal(t, err, io.EOF)
}
