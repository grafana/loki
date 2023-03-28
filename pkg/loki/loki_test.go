package loki

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/chunkenc"
	ingesterclient "github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	internalserver "github.com/grafana/loki/pkg/server"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
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
		// wait for Loki HTTP server to be ready.
		// retries at most 10 times (1 second in total) to avoid infinite loops when no timeout is set.
		for i := 0; i < 10; i++ {
			// waits until request to /ready doesn't error.
			resp, err := http.DefaultClient.Get(fmt.Sprintf("http://localhost:%d/ready", httpPort))
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

	resp, err := http.DefaultClient.Get(fmt.Sprintf("http://localhost:%d/config", httpPort))
	require.NoError(t, err)

	defer resp.Body.Close()

	bBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, string(bBytes), "abc")
	assert.True(t, customHandlerInvoked)
}

func TestGenerateMyData(t *testing.T) {
	var defaultsConfig Config

	if err := cfg.Unmarshal(&defaultsConfig, cfg.Defaults(flag.CommandLine)); err != nil {
		fmt.Println("Failed parsing defaults config:", err)
		os.Exit(1)
	}

	var lokiCfg ConfigWrapper
	destArgs := []string{"-config.file=../../cmd/loki/loki-local-config.yaml"}
	if err := cfg.DynamicUnmarshal(&lokiCfg, destArgs, flag.NewFlagSet("config-file-loader", flag.ContinueOnError)); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	if err := lokiCfg.Validate(); err != nil {
		fmt.Println("Failed to validate dest store config:", err)
		os.Exit(1)
	}

	limits, err := validation.NewOverrides(lokiCfg.LimitsConfig, nil)
	if err != nil {
		fmt.Println("Failed to create limit overrides:", err)
		os.Exit(1)
	}

	// Create a new registerer to avoid registering duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	clientMetrics := storage.NewClientMetrics()
	store, err := storage.NewStore(lokiCfg.StorageConfig, lokiCfg.ChunkStoreConfig, lokiCfg.SchemaConfig, limits, clientMetrics, prometheus.DefaultRegisterer, util_log.Logger)
	if err != nil {
		fmt.Println("Failed to create store:", err)
		os.Exit(1)
	}

	oneDay := 24 * time.Hour
	oneDayAgo := model.Now().Add(-oneDay)
	thirtyDaysAgo := model.Now().Add(-30 * oneDay)
	halfYearAgo := model.Now().Add(-180 * oneDay)

	c1 := createChunk(t, "org1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}},
		oneDayAgo, oneDayAgo.Add(time.Hour), func() string { return "1 day ago" })

	c2 := createChunk(t, "org1", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "foo"}},
		thirtyDaysAgo, thirtyDaysAgo.Add(time.Hour), func() string { return "30 days ago" })

	c3 := createChunk(t, "org1", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "foo"}},
		halfYearAgo, halfYearAgo.Add(time.Hour), func() string { return "180 days ago" })
	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
		c1, c2, c3,
	}))
}

func createChunk(t testing.TB, userID string, lbs labels.Labels, from model.Time, through model.Time, lineBuilder func() string) chunk.Chunk {
	t.Helper()
	const (
		targetSize = 1500 * 1024
		blockSize  = 256 * 1024
	)
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(labels.MetricName, "logs")
	metric := labelsBuilder.Labels(nil)
	fp := ingesterclient.Fingerprint(lbs)
	chunkEnc := chunkenc.NewMemChunk(chunkenc.EncSnappy, chunkenc.UnorderedHeadBlockFmt, blockSize, targetSize)

	for ts := from; !ts.After(through); ts = ts.Add(1 * time.Minute) {
		lineContent := ts.String()
		if lineBuilder != nil {
			lineContent = lineBuilder()
		}
		require.NoError(t, chunkEnc.Append(&logproto.Entry{
			Timestamp: ts.Time(),
			Line:      lineContent,
		}))
	}

	require.NoError(t, chunkEnc.Close())
	c := chunk.NewChunk(userID, fp, metric, chunkenc.NewFacade(chunkEnc, blockSize, targetSize), from, through)
	require.NoError(t, c.Encode())
	return c
}
