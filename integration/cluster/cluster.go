package cluster

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/grafana/dskit/multierror"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util/cfg"
)

var wrapRegistryOnce sync.Once

func wrapRegistry() {
	wrapRegistryOnce.Do(func() {
		prometheus.DefaultRegisterer = &wrappedRegisterer{Registerer: prometheus.DefaultRegisterer}
	})
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

type Cluster struct {
	sharedPath string
	components []*Component
	waitGroup  sync.WaitGroup
}

func New() *Cluster {
	wrapRegistry()
	sharedPath, err := ioutil.TempDir("", "loki-shared-data")
	if err != nil {
		panic(err.Error())
	}

	return &Cluster{
		sharedPath: sharedPath,
	}
}

func (c *Cluster) Run() error {
	for _, component := range c.components {
		if err := component.run(); err != nil {
			return err
		}
	}
	return nil
}
func (c *Cluster) Cleanup() error {
	var (
		files []string
		dirs  []string
	)
	if c.sharedPath != "" {
		dirs = append(dirs, c.sharedPath)
	}

	// call all components cleanup
	errs := multierror.New()
	for _, component := range c.components {
		f, d := component.cleanup()
		files = append(files, f...)
		dirs = append(dirs, d...)
	}
	if err := errs.Err(); err != nil {
		return err
	}

	// wait for all process to close
	c.waitGroup.Wait()

	// cleanup dirs/files

	for _, d := range dirs {
		errs.Add(os.RemoveAll(d))
	}
	for _, f := range files {
		errs.Add(os.Remove(f))
	}

	return errs.Err()
}

func (c *Cluster) AddComponent(name string, flags ...string) *Component {
	component := &Component{
		name:    name,
		cluster: c,
		flags:   flags,
	}

	var err error
	component.httpPort, err = getFreePort()
	if err != nil {
		panic(fmt.Errorf("error allocating HTTP port: %w", err))
	}

	component.grpcPort, err = getFreePort()
	if err != nil {
		panic(fmt.Errorf("error allocating GRPC port: %w", err))
	}

	c.components = append(c.components, component)
	return component
}

type Component struct {
	loki    *loki.Loki
	name    string
	cluster *Cluster
	flags   []string

	httpPort int
	grpcPort int

	configFile string
	dataPath   string
}

func (c *Component) HTTPURL() *url.URL {
	return &url.URL{
		Host:   fmt.Sprintf("localhost:%d", c.httpPort),
		Scheme: "http",
	}
}

func (c *Component) GRPCURL() *url.URL {
	return &url.URL{
		Host:   fmt.Sprintf("localhost:%d", c.grpcPort),
		Scheme: "grpc",
	}
}

func (c *Component) writeConfig() error {
	var err error

	configFile, err := ioutil.TempFile("", "loki-config")
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}

	c.dataPath, err = ioutil.TempDir("", "loki-data")
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}

	if _, err := configFile.Write([]byte(fmt.Sprintf(`
auth_enabled: true

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

storage_config:
  boltdb_shipper:
    shared_store: filesystem
    active_index_directory: %s/index
    cache_location: %s/boltdb-cache

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

ingester:
  lifecycler:
    min_ready_duration: 0s

frontend_worker:
  scheduler_address: localhost:%d

frontend:
  scheduler_address: localhost:%d

`,
		c.httpPort,
		c.grpcPort,
		c.dataPath,
		c.cluster.sharedPath,
		c.cluster.sharedPath,
		c.dataPath,
		c.dataPath,
		c.dataPath,
		c.grpcPort,
		c.grpcPort,
	))); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}
	if err := configFile.Close(); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}

	c.configFile = configFile.Name()

	return nil
}

func (c *Component) run() error {
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

	go func() {
		for {
			time.Sleep(time.Millisecond * 200)
			if c.loki.Server.HTTP == nil {
				continue
			}

			req := httptest.NewRequest("GET", "http://localhost/ready", nil)
			w := httptest.NewRecorder()
			c.loki.Server.HTTP.ServeHTTP(w, req)

			if w.Code == 200 {
				close(readyCh)
				return
			}
		}
	}()

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

// cleanup calls the stop handler and returns files and directories to be cleaned up
func (c *Component) cleanup() (files []string, dirs []string) {
	if c.loki != nil {
		c.loki.SignalHandler.Stop()
	}
	if c.configFile != "" {
		files = append(files, c.configFile)
	}
	if c.dataPath != "" {
		dirs = append(dirs, c.dataPath)
	}
	return files, dirs
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
