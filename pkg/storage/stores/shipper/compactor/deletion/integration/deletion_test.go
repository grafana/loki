package integration

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util/cfg"
)

func init() {
	// hack in a duplication ingoring registry
	prometheus.DefaultRegisterer = &wrappedRegisterer{Registerer: prometheus.DefaultRegisterer}
}

type wrappedRegisterer struct {
	prometheus.Registerer
}

func (w *wrappedRegisterer) Register(collector prometheus.Collector) error {
	if err := w.Registerer.Register(collector); err != nil {
		var aErr prometheus.AlreadyRegisteredError
		if errors.As(err, &aErr) {
			return nil
		}
		return err
	}
	return nil
}

func (w *wrappedRegisterer) MustRegister(collectors ...prometheus.Collector) {
	for _, c := range collectors {
		if err := w.Register(c); err != nil {
			panic(err.Error())
		}
	}
}

type testCluster struct {
	sharedPath string
	components []*testComponent
	waitGroup  sync.WaitGroup
}

func newTestCluster() *testCluster {
	sharedPath, err := ioutil.TempDir("", "loki-sharded-data")
	if err != nil {
		panic(err.Error())
	}

	return &testCluster{
		sharedPath: sharedPath,
	}
}

func (c *testCluster) run() error {
	for _, component := range c.components {
		if err := component.run(); err != nil {
			return err
		}
	}
	return nil
}
func (c *testCluster) cleanup() error {
	errs := multierror.New()
	for _, component := range c.components {
		errs.Add(component.cleanup())
	}
	if c.sharedPath != "" {
		errs.Add(os.RemoveAll(c.sharedPath))
	}
	if err := errs.Err(); err != nil {
		return err
	}
	c.waitGroup.Wait()

	return nil
}

func (c *testCluster) addComponent(name string, flags ...string) *testComponent {
	component := &testComponent{
		cluster: c,
		flags:   flags,
	}
	c.components = append(c.components, component)
	return component
}

type testComponent struct {
	loki    *loki.Loki
	cluster *testCluster
	flags   []string

	httpPort int
	grpcPort int

	configFile string
	dataPath   string
}

func (c *testComponent) writeConfig() error {
	var err error
	c.httpPort, err = getFreePort()
	if err != nil {
		return fmt.Errorf("error allocating HTTP port: %w", err)
	}

	c.grpcPort, err = getFreePort()
	if err != nil {
		return fmt.Errorf("error allocating GRPC port: %w", err)
	}

	configFile, err := ioutil.TempFile("", "loki-config")
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}

	c.dataPath, err = ioutil.TempDir("", "loki-data")
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}

	if _, err := configFile.Write([]byte(fmt.Sprintf(`
auth_enabled: false

server:
  http_listen_port: %d
  grpc_listen_port: %d

common:
  path_prefix: %s
  storage:
    filesystem:
      chunks_directory: %s/chunks
      rules_directory: %s/rules
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

compactor:
  working_directory: %s/retention
  shared_store: filesystem
  retention_enabled: true

analytics:
  reporting_enabled: false

ruler:
  alertmanager_url: http://localhost:9093
`,
		c.httpPort,
		c.grpcPort,
		c.dataPath,
		c.cluster.sharedPath,
		c.cluster.sharedPath,
		c.dataPath,
	))); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}
	if err := configFile.Close(); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}

	c.configFile = configFile.Name()

	return nil
}

func (c *testComponent) run() error {
	if err := c.writeConfig(); err != nil {
		return err
	}

	var config loki.ConfigWrapper

	var flagset = flag.NewFlagSet("test-flags", flag.ExitOnError)

	if err := cfg.DynamicUnmarshal(&config, append(
		c.flags,
		"-config.file",
		c.configFile,
	), flagset); err != nil {
		return err
	}

	if err := config.Validate(); err != nil {
		return err
	}

	var err error
	c.loki, err = loki.New(config.Config)
	if err != nil {
		return err
	}

	var (
		readyCh = make(chan struct{})
		errCh   = make(chan error, 1)
	)

	c.loki.ModuleManager.RegisterModule("test-ready", func() (services.Service, error) {
		close(readyCh)
		return nil, nil
	})
	c.loki.ModuleManager.AddDependency("test-ready", loki.Server)
	c.loki.ModuleManager.AddDependency(loki.All, "test-ready")
	c.loki.ModuleManager.AddDependency(loki.Compactor, "test-ready")

	c.cluster.waitGroup.Add(1)
	go func() {
		defer c.cluster.waitGroup.Done()
		err := c.loki.Run(loki.RunOpts{})
		if err != nil {
			errCh <- err
		}
	}()

	select {
	case <-readyCh:
		break
	case err := <-errCh:
		return err
	}

	return nil
}

func (c *testComponent) cleanup() error {
	errs := multierror.New()
	if c.loki != nil {
		c.loki.SignalHandler.Stop()
	}
	if c.dataPath != "" {
		errs.Add(os.RemoveAll(c.dataPath))
	}
	if c.configFile != "" {
		errs.Add(os.Remove(c.configFile))
	}
	return errs.Err()
}

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

func TestFilterOnlyMonolithCompactor(t *testing.T) {
	cluster := newTestCluster()
	defer cluster.cleanup()

	flags := []string{
		"-boltdb.shipper.compactor.deletion-mode=filter-only",
	}

	var (
		tAll       = cluster.addComponent("all", append(flags, "-target=all")...)
		tCompactor = cluster.addComponent("compactor", append(flags, "-target=compactor")...)
	)

	require.NoError(t, cluster.run())

	now := time.Now().Add(-5 * time.Minute)

	// TODO: do not sleep
	time.Sleep(time.Second)

	t.Run("ingest-logs", func(t *testing.T) {
		// ingest some log lines
		var jsonData = []byte(fmt.Sprintf(`{
  "streams": [
    {
      "stream": {
        "job": "fake"
      },
      "values": [
          [ "%d", "lineA" ],
          [ "%d", "lineB" ]
      ]
    }
  ]
}`, now.UnixNano(), now.UnixNano()))
		req, err := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/loki/api/v1/push", tAll.httpPort), bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		assert.Equal(t, 204, resp.StatusCode)
	})

	// request a deletion
	t.Run("request-deletion", func(t *testing.T) {
		reqU, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d/loki/api/v1/delete", tCompactor.httpPort))
		reqQ := reqU.Query()
		reqQ.Set("query", `{job="fake"}`) // TODO support a real filter with  |= lineA`)
		// TODO: Investigate why this is not nano seconds as for querying
		reqQ.Set("start", strconv.FormatInt(now.Add(-time.Hour).Unix(), 10))
		reqQ.Set("end", strconv.FormatInt(now.Add(time.Millisecond).Unix(), 10))
		reqU.RawQuery = reqQ.Encode()

		resp, err := http.Post(reqU.String(), "", nil)
		require.NoError(t, err)
		assert.Equal(t, 204, resp.StatusCode)
	})

	// TODO: do not sleep
	time.Sleep(time.Second)

	t.Run("query", func(t *testing.T) {
		reqU, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d/loki/api/v1/query_range", tAll.httpPort))
		reqQ := reqU.Query()
		reqQ.Set("query", `{job="fake"}`)
		reqQ.Set("start", strconv.FormatInt(now.Add(-time.Hour).UnixNano(), 10))
		reqQ.Set("end", strconv.FormatInt(now.Add(time.Millisecond).UnixNano(), 10))
		reqU.RawQuery = reqQ.Encode()

		resp, err := http.Post(reqU.String(), "", nil)
		require.NoError(t, err)
		assert.Equal(t, 200, resp.StatusCode)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, "", string(body))
	})
}
