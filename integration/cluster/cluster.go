package cluster

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/multierror"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/integration/util"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

var (
	wrapRegistryOnce sync.Once

	configTemplate = template.Must(template.New("").Parse(`
auth_enabled: true

server:
  http_listen_port: 0
  grpc_listen_port: 0
  grpc_server_max_recv_msg_size: 110485813
  grpc_server_max_send_msg_size: 110485813


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

limits_config:
  per_stream_rate_limit: 50MB
  per_stream_rate_limit_burst: 50MB
  ingestion_rate_mb: 50
  ingestion_burst_size_mb: 50

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

querier:
  multi_tenant_queries_enabled: true

ruler:
  enable_api: true
  ring:
    kvstore:
      store: inmemory
  wal:
    dir: {{.sharedDataPath}}/ruler-wal
  storage:
    type: local
    local:
      directory: {{.sharedDataPath}}/rules
  rule_path: {{.sharedDataPath}}/prom-rule

`))
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
	sharedPath    string
	overridesFile string
	components    []*Component
	waitGroup     sync.WaitGroup
}

func New(logLevel level.Value) *Cluster {
	if logLevel != nil {
		util_log.Logger = level.NewFilter(log.NewLogfmtLogger(os.Stderr), level.Allow(logLevel))
	}

	wrapRegistry()
	sharedPath, err := os.MkdirTemp("", "loki-shared-data")
	if err != nil {
		panic(err.Error())
	}

	overridesFile := filepath.Join(sharedPath, "loki-overrides.yaml")

	err = os.WriteFile(filepath.Join(sharedPath, "loki-overrides.yaml"), []byte(`overrides:`), 0777)
	if err != nil {
		panic(fmt.Errorf("error creating overrides file: %w", err))
	}

	return &Cluster{
		sharedPath:    sharedPath,
		overridesFile: overridesFile,
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

func (c *Cluster) Restart() error {
	if err := c.stop(false); err != nil {
		return err
	}

	return c.Run()
}

func (c *Cluster) Cleanup() error {
	return c.stop(true)
}

func (c *Cluster) stop(cleanupFiles bool) error {
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

	if cleanupFiles {
		// cleanup dirs/files
		for _, d := range dirs {
			errs.Add(os.RemoveAll(d))
		}
		for _, f := range files {
			errs.Add(os.Remove(f))
		}
	}

	return errs.Err()
}

func (c *Cluster) AddComponent(name string, flags ...string) *Component {
	component := &Component{
		name:          name,
		cluster:       c,
		flags:         flags,
		running:       false,
		overridesFile: c.overridesFile,
	}

	c.components = append(c.components, component)
	return component
}

type Component struct {
	loki    *loki.Loki
	name    string
	cluster *Cluster
	flags   []string

	configFile    string
	extraConfigs  []string
	overridesFile string
	dataPath      string

	running bool
	wg      sync.WaitGroup
}

// ClusterSharedPath returns the path to the shared directory between all components in the cluster.
// This path will be removed once the cluster is stopped.
func (c *Component) ClusterSharedPath() string {
	return c.cluster.sharedPath
}

func (c *Component) HTTPURL() string {
	return fmt.Sprintf("http://localhost:%s", port(c.loki.Server.HTTPListenAddr().String()))
}

func (c *Component) GRPCURL() string {
	return fmt.Sprintf("localhost:%s", port(c.loki.Server.GRPCListenAddr().String()))
}

func (c *Component) WithExtraConfig(cfg string) {
	if c.running {
		panic("cannot set extra config after component is running")
	}

	c.extraConfigs = append(c.extraConfigs, cfg)
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

	mergedConfig, err := c.MergedConfig()
	if err != nil {
		return fmt.Errorf("error getting merged config: %w", err)
	}

	if err := os.WriteFile(configFile.Name(), mergedConfig, 0644); err != nil {
		return fmt.Errorf("error writing config file: %w", err)
	}

	if err := configFile.Close(); err != nil {
		return fmt.Errorf("error closing config file: %w", err)
	}
	c.configFile = configFile.Name()
	return nil
}

// MergedConfig merges the base config template with any additional config that has been provided
func (c *Component) MergedConfig() ([]byte, error) {
	var sb bytes.Buffer

	if err := configTemplate.Execute(&sb, map[string]interface{}{
		"dataPath":       c.dataPath,
		"sharedDataPath": c.cluster.sharedPath,
	}); err != nil {
		return nil, fmt.Errorf("error writing config file: %w", err)
	}

	merger := util.NewYAMLMerger()
	merger.AddFragment(sb.Bytes())

	for _, extra := range c.extraConfigs {
		merger.AddFragment([]byte(extra))
	}

	merged, err := merger.Merge()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged config to YAML: %w", err)
	}

	return merged, nil
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
		"-limits.per-user-override-config",
		c.overridesFile,
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
	c.wg.Add(1)

	go func() {
		defer c.cluster.waitGroup.Done()
		defer c.wg.Done()
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
		c.running = false
	}
	if c.configFile != "" {
		files = append(files, c.configFile)
	}
	if c.dataPath != "" {
		dirs = append(dirs, c.dataPath)
	}

	return files, dirs
}

func (c *Component) Restart() error {
	c.cleanup()
	c.wg.Wait()
	return c.run()
}

type runtimeConfigValues struct {
	TenantLimits map[string]*validation.Limits `yaml:"overrides"`
}

func (c *Component) SetTenantLimits(tenant string, limits validation.Limits) error {
	rcv := runtimeConfigValues{}
	rcv.TenantLimits = c.loki.TenantLimits.AllByUserID()
	if rcv.TenantLimits == nil {
		rcv.TenantLimits = map[string]*validation.Limits{}
	}
	rcv.TenantLimits[tenant] = &limits

	config, err := yaml.Marshal(rcv)
	if err != nil {
		return err
	}

	return os.WriteFile(c.overridesFile, config, 0777)
}

func (c *Component) GetTenantLimits(tenant string) validation.Limits {
	limits := c.loki.TenantLimits.TenantLimits(tenant)
	if limits == nil {
		return c.loki.Cfg.LimitsConfig
	}

	return *limits
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
