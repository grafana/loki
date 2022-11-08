package cluster

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/grafana/dskit/multierror"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util/cfg"
)

var (
	wrapRegistryOnce sync.Once

	configTemplate = template.Must(template.New("").Parse(`
auth_enabled: true

server:
  http_listen_port: 0
  grpc_listen_port: 0

common:
  path_prefix: {{.dataPath}}
  storage:
    filesystem:
      chunks_directory: {{.sharedDataPath}}/chunks
      rules_directory: {{.sharedDataPath}}/rules
  replication_factor: 1
  ring:
    instance_addr: 127.0.0.1
    kvstore:
      store: inmemory

storage_config:
  boltdb_shipper:
    shared_store: filesystem
    active_index_directory: {{.dataPath}}/index
    cache_location: {{.dataPath}}/boltdb-cache

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
  working_directory: {{.dataPath}}/retention
  shared_store: filesystem
  retention_enabled: true

analytics:
  reporting_enabled: false

ingester:
  lifecycler:
    min_ready_duration: 0s

{{if .remoteWriteUrls}}
ruler:
  wal:
    dir: {{.rulerWALPath}}
  storage:
    type: local
    local:
      directory: {{.rulesPath}}
  rule_path: {{.sharedDataPath}}/rule
  enable_api: true
  ring:
    kvstore:
      store: inmemory
  remote_write:
    enabled: true
    clients:
      remote_client1:
        url: {{index .remoteWriteUrls 0}}/api/v1/write
      remote_client2:
        url: {{index .remoteWriteUrls 1}}/api/v1/write
{{end}}
`))

	rulesConfig = `
groups:
- name: always-firing
  interval: 1s
  rules:
  - alert: fire
    expr: |
      1 > 0
    for: 0m
    labels:
      severity: warning
    annotations:
      summary: test
`
)

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
	sharedPath, err := os.MkdirTemp("", "loki-shared-data")
	if err != nil {
		panic(err.Error())
	}

	return &Cluster{
		sharedPath: sharedPath,
	}
}

func (c *Cluster) Run() error {
	for _, component := range c.components {
		if component.running {
			continue
		}

		if err := component.run(); err != nil {
			return err
		}
	}
	return nil
}
func (c *Cluster) Cleanup() error {
	_, cancelFunc := context.WithTimeout(context.Background(), time.Second*3)
	defer cancelFunc()

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
		running: false,
	}

	c.components = append(c.components, component)
	return component
}

type Component struct {
	loki    *loki.Loki
	name    string
	cluster *Cluster
	flags   []string

	configFile   string
	dataPath     string
	rulerWALPath string
	rulesPath    string
	RulesTenant  string

	running bool

	RemoteWriteUrls []string
}

func (c *Component) HTTPURL() string {
	return fmt.Sprintf("http://localhost:%s", port(c.loki.Server.HTTPListenAddr().String()))
}

func (c *Component) GRPCURL() string {
	return fmt.Sprintf("localhost:%s", port(c.loki.Server.GRPCListenAddr().String()))
}

func port(addr string) string {
	parts := strings.Split(addr, ":")
	return parts[len(parts)-1]
}

func (c *Component) writeConfig() error {
	var err error

	configFile, err := os.CreateTemp("", "loki-config")
	if err != nil {
		return fmt.Errorf("error creating config file: %w", err)
	}

	c.dataPath, err = os.MkdirTemp("", "loki-data")
	if err != nil {
		return fmt.Errorf("error creating data path: %w", err)
	}

	if len(c.RemoteWriteUrls) > 0 {
		c.rulesPath, err = os.MkdirTemp(c.cluster.sharedPath, "rules")
		if err != nil {
			return fmt.Errorf("error creating rules path: %w", err)
		}

		fakeDir, err := os.MkdirTemp(c.rulesPath, "fake")
		if err != nil {
			return fmt.Errorf("error creating rules/fake path: %w", err)
		}

		s := strings.Split(fakeDir, "/")
		c.RulesTenant = s[len(s)-1]

		c.rulerWALPath, err = os.MkdirTemp(c.cluster.sharedPath, "ruler-wal")
		if err != nil {
			return fmt.Errorf("error creating ruler-wal path: %w", err)
		}

		rulesConfigFile, err := os.CreateTemp(fakeDir, "rules*.yaml")
		if err != nil {
			return fmt.Errorf("error creating rules config file: %w", err)
		}

		if _, err = rulesConfigFile.Write([]byte(rulesConfig)); err != nil {
			return fmt.Errorf("error writing to rules config file: %w", err)
		}

		rulesConfigFile.Close()
	}

	if err := configTemplate.Execute(configFile, map[string]interface{}{
		"dataPath":        c.dataPath,
		"sharedDataPath":  c.cluster.sharedPath,
		"remoteWriteUrls": c.RemoteWriteUrls,
		"rulesPath":       c.rulesPath,
		"rulerWALPath":    c.rulerWALPath,
	}); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}

	if err := configFile.Close(); err != nil {
		return fmt.Errorf("error closing config file: %w", err)
	}
	c.configFile = configFile.Name()
	return nil
}

func (c *Component) run() error {
	c.running = true

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
			if c.loki == nil || c.loki.Server == nil || c.loki.Server.HTTP == nil {
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
			newErr := fmt.Errorf("error starting component %v: %w", c.name, err)
			errCh <- newErr
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
	if c.loki != nil && c.loki.SignalHandler != nil {
		c.loki.SignalHandler.Stop()
	}
	if c.configFile != "" {
		files = append(files, c.configFile)
	}
	if c.dataPath != "" {
		dirs = append(dirs, c.dataPath)
	}
	if c.rulerWALPath != "" {
		dirs = append(dirs, c.rulerWALPath)
	}
	if c.rulesPath != "" {
		dirs = append(dirs, c.rulesPath)
	}

	return files, dirs
}

func NewRemoteWriteServer(handler *http.HandlerFunc) *httptest.Server {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Errorf("failed to listen on: %v", err))
	}

	server := httptest.NewUnstartedServer(*handler)
	server.Listener = l
	server.Start()

	return server
}
