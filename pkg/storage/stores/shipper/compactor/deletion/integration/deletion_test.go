package integration

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util/cfg"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func getFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

func newCfg(t *testing.T) string {
	httpPort, err := getFreePort()
	require.NoError(t, err)

	grpcPort, err := getFreePort()
	require.NoError(t, err)

	file, err := ioutil.TempFile("", "loki-config")
	require.NoError(t, err)

	_, err = file.Write([]byte(fmt.Sprintf(`
auth_enabled: false

server:
  http_listen_port: %d
  grpc_listen_port: %d

common:
  path_prefix: /tmp/loki
  storage:
    filesystem:
      chunks_directory: /tmp/loki/chunks
      rules_directory: /tmp/loki/rules
  replication_factor: 1
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h

analytics:
  reporting_enabled: false

ruler:
  alertmanager_url: http://localhost:9093
`, httpPort, grpcPort)))
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Logf("remove config %s", file.Name())
		os.Remove(file.Name())
	})

	return file.Name()
}

func TestFilterOnlyMonolithCompactor(t *testing.T) {

	for _, n := range []string{"a", "b"} {
		t.Run(n, func(t *testing.T) {
			var config loki.ConfigWrapper

			var flagset = flag.NewFlagSet("test-flags", flag.ExitOnError)
			myCfg := newCfg(t)

			require.NoError(t, cfg.DynamicUnmarshal(&config, []string{
				"-config.file", myCfg,
			}, flagset))

			require.NoError(t, config.Validate())

			// hack in a fresh registry
			reg := prometheus.NewRegistry()
			prometheus.DefaultGatherer = reg
			prometheus.DefaultRegisterer = reg

			l, err := loki.New(config.Config)
			require.NoError(t, err)

			go func() {
				require.NoError(t, l.Run(loki.RunOpts{}))
			}()
		})
	}

	time.Sleep(30 * time.Second)

}
