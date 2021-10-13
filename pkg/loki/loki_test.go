package loki

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlagDefaults(t *testing.T) {
	c := Config{}

	f := flag.NewFlagSet("test", flag.PanicOnError)
	c.RegisterFlags(f)

	buf := bytes.Buffer{}

	f.SetOutput(&buf)
	f.PrintDefaults()

	const delim = '\n'

	minTimeChecked := false
	pingWithoutStreamChecked := false
	for {
		line, err := buf.ReadString(delim)
		if err == io.EOF {
			break
		}

		require.NoError(t, err)

		if strings.Contains(line, "-server.grpc.keepalive.min-time-between-pings") {
			nextLine, err := buf.ReadString(delim)
			require.NoError(t, err)
			assert.Contains(t, nextLine, "(default 10s)")
			minTimeChecked = true
		}

		if strings.Contains(line, "-server.grpc.keepalive.ping-without-stream-allowed") {
			nextLine, err := buf.ReadString(delim)
			require.NoError(t, err)
			assert.Contains(t, nextLine, "(default true)")
			pingWithoutStreamChecked = true
		}
	}

	require.True(t, minTimeChecked)
	require.True(t, pingWithoutStreamChecked)

	require.Equal(t, true, c.Server.GRPCServerPingWithoutStreamAllowed)
	require.Equal(t, 10*time.Second, c.Server.GRPCServerMinTimeBetweenPings)
}
