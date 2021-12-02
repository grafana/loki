package loki

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
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

	flagToCheck := "-server.grpc.keepalive.min-time-between-pings"
	require.Contains(t, gotFlags, flagToCheck)
	require.Equal(t, c.Server.GRPCServerMinTimeBetweenPings, 10*time.Second)
	require.Contains(t, gotFlags[flagToCheck], "(default 10s)")

	flagToCheck = "-server.grpc.keepalive.ping-without-stream-allowed"
	require.Contains(t, gotFlags, flagToCheck)
	require.Equal(t, c.Server.GRPCServerPingWithoutStreamAllowed, true)
	require.Contains(t, gotFlags[flagToCheck], "(default true)")
}

func TestLoki_isModuleEnabled(t1 *testing.T) {
	tests := []struct {
		name   string
		target flagext.StringSliceCSV
		module string
		want   bool
	}{
		{name: "Target All includes Querier", target: flagext.StringSliceCSV{"all"}, module: Querier, want: true},
		{name: "Target Querier does not include Distributor", target: flagext.StringSliceCSV{"querier"}, module: Distributor, want: false},
		{name: "Target Read includes Query Frontend", target: flagext.StringSliceCSV{"read"}, module: QueryFrontend, want: true},
		{name: "Target Querier does not include Query Frontend", target: flagext.StringSliceCSV{"querier"}, module: QueryFrontend, want: false},
		{name: "Target Query Frontend does not include Querier", target: flagext.StringSliceCSV{"query-frontend"}, module: Querier, want: false},
		{name: "Multi target includes querier", target: flagext.StringSliceCSV{"query-frontend", "query-scheduler", "querier"}, module: Querier, want: true},
		{name: "Multi target does not include distributor", target: flagext.StringSliceCSV{"query-frontend", "query-scheduler", "querier"}, module: Distributor, want: false},
		{name: "Test recursive dep, Ingester -> TenantConfigs -> RuntimeConfig", target: flagext.StringSliceCSV{"ingester"}, module: RuntimeConfig, want: true},
	}
	for _, tt := range tests {
		t1.Run(tt.name, func(t1 *testing.T) {
			t := &Loki{
				Cfg: Config{
					Target: tt.target,
				},
			}
			err := t.setupModuleManager()
			assert.NoError(t1, err)
			if got := t.isModuleActive(tt.module); got != tt.want {
				t1.Errorf("isModuleActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoki_CustomRunOptsBehavior(t *testing.T) {
	yamlConfig := `target: querier
server:
  http_listen_port: 3100
common:
  path_prefix: /tmp/loki
  ring:
    kvstore:
      store: inmemory
schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      row_shards: 10
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h`

	cfgWrapper, _, err := configWrapperFromYAML(t, yamlConfig, nil)
	require.NoError(t, err)

	loki, err := New(cfgWrapper.Config)
	require.NoError(t, err)

	lokiHealthCheck := func() error {
		// wait for Loki HTTP server to be ready.
		// retries at most 10 times (1 second in total) to avoid infinite loops when no timeout is set.
		for i := 0; i < 10; i++ {
			// waits until request to /ready doesn't error.
			resp, err := http.DefaultClient.Get("http://localhost:3100/ready")
			if err != nil {
				time.Sleep(time.Millisecond * 200)
				continue
			}

			// waits until /ready returns OK.
			if resp.StatusCode != http.StatusOK {
				time.Sleep(time.Millisecond * 200)
				continue
			}

			// Loki is healthy.
			return nil
		}

		return fmt.Errorf("loki HTTP not healthy")
	}

	customHandlerInvoked := false
	customHandler := func(w http.ResponseWriter, _ *http.Request) {
		customHandlerInvoked = true
		_, err := w.Write([]byte("abc"))
		require.NoError(t, err)
	}

	// Run Loki querier in a different go routine and with custom /config handler.
	go func() {
		err := loki.Run(RunOpts{CustomConfigEndpointHandlerFn: customHandler})
		require.NoError(t, err)
	}()

	err = lokiHealthCheck()
	require.NoError(t, err)

	resp, err := http.DefaultClient.Get("http://localhost:3100/config")
	require.NoError(t, err)

	defer resp.Body.Close()

	bBytes, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, string(bBytes), "abc")
	assert.True(t, customHandlerInvoked)
}
