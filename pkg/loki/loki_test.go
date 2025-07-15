package loki

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/collectors/version"

	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalserver "github.com/grafana/loki/v3/pkg/server"
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

	flagToCheck = "-server.http-listen-port"
	require.Contains(t, gotFlags, flagToCheck)
	require.Equal(t, c.Server.HTTPListenPort, 3100)
	require.Contains(t, gotFlags[flagToCheck], "(default 3100)")
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

func TestLoki_AppendOptionalInternalServer(t *testing.T) {
	fake := &Loki{
		Cfg: Config{
			Target: flagext.StringSliceCSV{All},
			Server: server.Config{
				HTTPListenAddress: "3100",
			},
		},
	}
	err := fake.setupModuleManager()
	assert.NoError(t, err)

	var tests []string
	for target, deps := range fake.deps {
		for _, dep := range deps {
			if dep == Server {
				tests = append(tests, target)
				break
			}
		}
	}

	assert.NotEmpty(t, tests, tests)

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			l := &Loki{
				Cfg: Config{
					Target: flagext.StringSliceCSV{tt},
					Server: server.Config{
						HTTPListenAddress: "3100",
					},
					InternalServer: internalserver.Config{
						Config: server.Config{
							HTTPListenAddress: "3101",
						},
						Enable: true,
					},
				},
			}
			err := l.setupModuleManager()
			assert.NoError(t, err)
			assert.Contains(t, l.deps[tt], InternalServer)
			assert.Contains(t, l.deps[tt], Server)
		})
	}
}

func getRandomPorts(n int) []int {
	portListeners := []net.Listener{}
	for i := 0; i < n; i++ {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			panic(err)
		}

		portListeners = append(portListeners, listener)
	}

	portNumbers := []int{}
	for i := 0; i < n; i++ {
		port := portListeners[i].Addr().(*net.TCPAddr).Port
		portNumbers = append(portNumbers, port)
		if err := portListeners[i].Close(); err != nil {
			panic(err)
		}

	}

	return portNumbers
}

func TestLoki_CustomRunOptsBehavior(t *testing.T) {
	t.Parallel()
	// Set an overall test timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a channel to signal test completion
	done := make(chan struct{})
	ports := getRandomPorts(2)
	httpPort := ports[0]
	grpcPort := ports[1]

	yamlConfig := fmt.Sprintf(`target: querier
server:
  http_listen_port: %d
  grpc_listen_port: %d
common:
  compactor_address: http://localhost:%d
  path_prefix: /tmp/loki
  instance_addr: localhost
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
        period: 24h`, httpPort, grpcPort, httpPort)

	cfgWrapper, _, err := configWrapperFromYAML(t, yamlConfig, nil)
	require.NoError(t, err)

	loki, err := New(cfgWrapper.Config)
	require.NoError(t, err)

	lokiHealthCheck := func() error {
		// Set an overall timeout for health check attempts
		timeout := time.After(5 * time.Second)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		// Loop until timeout or success
		for {
			select {
			case <-timeout:
				return fmt.Errorf("timeout waiting for loki HTTP to become healthy")
			case <-ticker.C:
				// Try to connect to the /ready endpoint
				resp, err := http.DefaultClient.Get(fmt.Sprintf("http://0.0.0.0:%d/ready", httpPort))
				if err != nil {
					continue
				}

				// Check if status is OK
				if resp.StatusCode != http.StatusOK {
					resp.Body.Close()
					continue
				}

				resp.Body.Close()
				return nil // Loki is healthy
			}
		}
	}

	customHandlerInvoked := false
	customHandler := func(w http.ResponseWriter, _ *http.Request) {
		customHandlerInvoked = true
		_, err := w.Write([]byte("abc"))
		require.NoError(t, err)
	}

	// Create a context for server shutdown
	srvCtx, srvCancel := context.WithCancel(context.Background())

	// Run Loki querier in a different go routine and with custom /config handler.
	go func() {
		defer close(done)
		runOpts := RunOpts{
			CustomConfigEndpointHandlerFn: customHandler,
		}

		// Create a separate goroutine to stop Loki when test is done or times out
		go func() {
			select {
			case <-ctx.Done():
				t.Logf("Test timeout reached, stopping Loki server")
				loki.SignalHandler.Stop()
			case <-srvCtx.Done():
				t.Logf("Test completed, stopping Loki server")
				loki.SignalHandler.Stop()
			}
		}()

		err := loki.Run(runOpts)
		require.NoError(t, err)
	}()

	// Run the test in a goroutine to handle timeout properly
	go func() {
		// Using a separate function to handle any panics in the test goroutine
		defer srvCancel() // Always cancel the context when this function exits

		// Wait for Loki to be healthy
		err = lokiHealthCheck()
		if err != nil {
			t.Errorf("Health check failed: %v", err)
			return
		}

		// Make request to custom handler
		resp, err := http.DefaultClient.Get(fmt.Sprintf("http://0.0.0.0:%d/config", httpPort))
		if err != nil {
			t.Errorf("Failed to get config: %v", err)
			return
		}
		defer resp.Body.Close()

		bBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Errorf("Failed to read response: %v", err)
			return
		}

		// Verify results
		if string(bBytes) != "abc" {
			t.Errorf("Expected response 'abc', got '%s'", string(bBytes))
		}
		if !customHandlerInvoked {
			t.Errorf("Custom handler was not invoked")
		}
	}()

	// Wait for either test completion or timeout
	select {
	case <-ctx.Done():
		t.Fatalf("Test timed out after 30 seconds")
	case <-done:
		// Test completed successfully
	}

	// Clean up metrics
	unregisterLokiMetrics(loki)
}

func unregisterLokiMetrics(loki *Loki) {
	loki.ClientMetrics.Unregister()
	loki.deleteClientMetrics.Unregister()
	prometheus.Unregister(version.NewCollector(constants.Loki))
	prometheus.Unregister(collectors.NewGoCollector(
		collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll),
	))
	//TODO Update DSKit to have a method to unregister these metrics
	prometheus.Unregister(loki.Metrics.TCPConnections)
	prometheus.Unregister(loki.Metrics.TCPConnectionsLimit)
	prometheus.Unregister(loki.Metrics.RequestDuration)
	prometheus.Unregister(loki.Metrics.ReceivedMessageSize)
	prometheus.Unregister(loki.Metrics.SentMessageSize)
	prometheus.Unregister(loki.Metrics.InflightRequests)
}
