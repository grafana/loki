package loki

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

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

	// Populate map with parsed default flags.
	// Key is the flag and value is the default text.
	gotFlags := make(map[string]string)
	for {
		line, err := buf.ReadString(delim)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		nextLine, err := buf.ReadString(delim)
		require.NoError(t, err)

		trimmedLine := strings.Trim(line, " \n")
		splittedLine := strings.Split(trimmedLine, " ")[0]
		gotFlags[splittedLine] = nextLine
	}

	flagToCheck := "-distributor.ring.store"
	require.Contains(t, gotFlags, flagToCheck)
	require.Equal(t, c.Distributor.DistributorRing.KVStore.Store, "inmemory")
	require.Contains(t, gotFlags[flagToCheck], "(default \"inmemory\")")

	flagToCheck = "-server.grpc.keepalive.min-time-between-pings"
	require.Contains(t, gotFlags, flagToCheck)
	require.Equal(t, c.Server.GRPCServerMinTimeBetweenPings, 10*time.Second)
	require.Contains(t, gotFlags[flagToCheck], "(default 10s)")

	flagToCheck = "-server.grpc.keepalive.ping-without-stream-allowed"
	require.Contains(t, gotFlags, flagToCheck)
	require.Equal(t, c.Server.GRPCServerPingWithoutStreamAllowed, true)
	require.Contains(t, gotFlags[flagToCheck], "(default true)")
}
