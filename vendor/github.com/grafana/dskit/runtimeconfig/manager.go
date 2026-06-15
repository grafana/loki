package runtimeconfig

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"
	"go.yaml.in/yaml/v3"

	"github.com/grafana/dskit/clusterutil"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/services"
)

// Preprocessor optionally processes and changes config prior to parsing.
type Preprocessor func(b []byte) ([]byte, error)

// Loader loads the configuration from files.
type Loader func(r io.Reader) (interface{}, error)

// Config holds the config for an Manager instance.
// It holds config related to loading per-tenant config.
type Config struct {
	ReloadPeriod time.Duration `yaml:"period" category:"advanced"`
	// LoadPath contains the path to the runtime config files or HTTP URLs.
	// Requires a non-empty value
	LoadPath     flagext.StringSliceCSV `yaml:"file"`
	Preprocessor Preprocessor           `yaml:"-"`
	Loader       Loader                 `yaml:"-"`

	// Configurations related to fetching runtime configurations from HTTP URLs rather than local files.
	HTTPClientTimeout           time.Duration                       `yaml:"http_client_timeout" category:"advanced"`
	HTTPClientClusterValidation clusterutil.ClusterValidationConfig `yaml:"http_client_cluster_validation" category:"advanced"`
}

// RegisterFlagsWithPrefix registers flags under the specified prefix, which could be empty.
// If a non-empty prefix is provided, it's expected to end with a dot.
func (mc *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Var(&mc.LoadPath, prefix+"file", "Comma separated list of yaml files or URLs with the configuration that can be updated at runtime. Runtime config files will be merged from left to right.")
	f.DurationVar(&mc.ReloadPeriod, prefix+"reload-period", 10*time.Second, "How often to check runtime config files.")
	f.DurationVar(&mc.HTTPClientTimeout, prefix+"http-client-timeout", 30*time.Second, "HTTP client timeout when fetching runtime config from URLs.")
	mc.HTTPClientClusterValidation.RegisterFlagsWithPrefix(prefix+"http-client-cluster-validation.", f)
}

// RegisterFlags registers flags.
func (mc *Config) RegisterFlags(f *flag.FlagSet) {
	mc.RegisterFlagsWithPrefix("runtime-config.", f)
}

// Manager periodically reloads the configuration from specified files, and keeps this
// configuration available for clients.
type Manager struct {
	services.Service

	cfg    Config
	logger log.Logger

	listenersMtx sync.Mutex
	listeners    []chan interface{}

	configPtr atomic.Pointer[interface{}]

	configLoadSuccess prometheus.Gauge
	configHash        *prometheus.GaugeVec

	// Maps provider name to hash. Only used by loadConfig in Starting and Running states, so it doesn't need synchronization.
	fileHashes map[string]string

	providers []provider
}

// New creates an instance of Manager. Manager is a services.Service, and must be explicitly started to perform any work.
func New(cfg Config, configName string, registerer prometheus.Registerer, logger log.Logger) (*Manager, error) {
	if len(cfg.LoadPath) == 0 {
		return nil, errors.New("LoadPath is empty")
	}

	// The cluster-validation counter shares its name with similarly-named counters from other
	// client-side cluster-validation reporters in the calling application (e.g. gRPC clients), so
	// its label set must match theirs: {client, protocol, method}, with no per-manager "config"
	// label. We therefore pass an un-wrapped registerer to httpTransport and disambiguate per
	// manager via the "client" label value (set to "runtime-config/<configName>").
	clusterValidationRegisterer := registerer
	registerer = prometheus.WrapRegistererWith(prometheus.Labels{"config": configName}, registerer)

	mgr := Manager{
		cfg: cfg,
		configLoadSuccess: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "runtime_config_last_reload_successful",
			Help: "Whether the last runtime-config reload attempt was successful.",
		}),
		configHash: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Name: "runtime_config_hash",
			Help: "Hash of the currently active runtime configuration, merged from all configured files.",
		}, []string{"sha256"}),
		logger: logger,
	}

	var httpClient *http.Client
	var httpDuration *prometheus.HistogramVec
	for _, p := range cfg.LoadPath {
		if isURL(p) {
			if httpClient == nil {
				timeout := cfg.HTTPClientTimeout
				if timeout == 0 {
					timeout = 30 * time.Second
				}
				httpClient = &http.Client{Timeout: timeout, Transport: httpTransport(cfg, configName, clusterValidationRegisterer, logger)}
				httpDuration = newHTTPRequestDuration(registerer)
			}
			mgr.providers = append(mgr.providers, newHTTPProvider(p, httpClient, httpDuration))
		} else {
			mgr.providers = append(mgr.providers, newFileProvider(p))
		}
	}

	mgr.Service = services.NewBasicService(mgr.starting, mgr.loop, mgr.stopping)
	return &mgr, nil
}

func (om *Manager) starting(ctx context.Context) error {
	if len(om.cfg.LoadPath) == 0 {
		return nil
	}

	return errors.Wrap(om.loadConfig(ctx), "failed to load runtime config")
}

// CreateListenerChannel creates new channel that can be used to receive new config values.
// If there is no receiver waiting for value when config manager tries to send the update,
// or channel buffer is full, update is discarded.
//
// When config manager is stopped, it closes all channels to notify receivers that they will
// not receive any more updates.
func (om *Manager) CreateListenerChannel(buffer int) <-chan interface{} {
	ch := make(chan interface{}, buffer)

	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	om.listeners = append(om.listeners, ch)
	return ch
}

// CloseListenerChannel removes given channel from list of channels to send notifications to and closes channel.
func (om *Manager) CloseListenerChannel(listener <-chan interface{}) {
	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	for ix, ch := range om.listeners {
		if ch == listener {
			om.listeners = append(om.listeners[:ix], om.listeners[ix+1:]...)
			close(ch)
			break
		}
	}
}

func (om *Manager) loop(ctx context.Context) error {
	if len(om.cfg.LoadPath) == 0 {
		level.Info(om.logger).Log("msg", "runtime config disabled: file not specified")
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(om.cfg.ReloadPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := om.loadConfig(ctx)
			if err != nil {
				// Log but don't stop on error - we don't want to halt all ingesters because of a typo
				level.Error(om.logger).Log("msg", "failed to load config", "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// loadConfig loads all configuration files using the loader function then merges the yaml configuration files into one yaml document.
// and notifies listeners if successful.
func (om *Manager) loadConfig(ctx context.Context) error {
	rawData := make([][]byte, len(om.providers))
	hashes := map[string]string{}

	for i, p := range om.providers {
		buf, err := p.Read(ctx)
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrapf(err, "read %q", p.Name())
		}

		if om.cfg.Preprocessor != nil {
			buf, err = om.cfg.Preprocessor(buf)
			if err != nil {
				om.configLoadSuccess.Set(0)
				return errors.Wrapf(err, "preprocess %q", p.Name())
			}
		}

		rawData[i] = buf
		hashes[p.Name()] = fmt.Sprintf("%x", sha256.Sum256(buf))
	}

	// check if new hashes are the same as before
	sameHashes := true
	for name, h := range hashes {
		if om.fileHashes[name] != h {
			sameHashes = false
			break
		}
	}

	if sameHashes {
		// No need to rebuild runtime config.
		om.configLoadSuccess.Set(1)
		return nil
	}

	mergedConfig := map[string]interface{}{}
	for i, p := range om.providers {
		data := rawData[i]
		yamlFile, err := om.unmarshalMaybeGzipped(p.Name(), data)
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrapf(err, "unmarshal %q", p.Name())
		}
		mergedConfig, err = mergeConfigMaps(mergedConfig, yamlFile, "")
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrapf(err, "can't merge %q on top of the previous providers", p.Name())
		}
	}

	buf, err := yaml.Marshal(mergedConfig)
	if err != nil {
		om.configLoadSuccess.Set(0)
		return errors.Wrap(err, "marshal file")
	}

	hash := sha256.Sum256(buf)
	cfg, err := om.cfg.Loader(bytes.NewReader(buf))
	if err != nil {
		om.configLoadSuccess.Set(0)
		return errors.Wrap(err, "load file")
	}
	om.configLoadSuccess.Set(1)

	om.setConfig(cfg)
	om.callListeners(cfg)

	// expose hash of runtime config
	om.configHash.Reset()
	om.configHash.WithLabelValues(fmt.Sprintf("%x", hash)).Set(1)

	// preserve hashes for next loop
	om.fileHashes = hashes
	return nil
}

func (om *Manager) unmarshalMaybeGzipped(filename string, data []byte) (map[string]any, error) {
	yamlFile := map[string]any{}
	if strings.HasSuffix(filename, ".gz") {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, errors.Wrap(err, "read gzipped file")
		}
		defer r.Close()
		err = yaml.NewDecoder(r).Decode(&yamlFile)
		return yamlFile, errors.Wrap(err, "uncompress/unmarshal gzipped file")
	}

	if err := yaml.Unmarshal(data, &yamlFile); err != nil {
		// Give a hint if we think that file is gzipped.
		if isGzip(data) {
			return nil, errors.Wrap(err, "file looks gzipped but doesn't have a .gz extension")
		}
		return nil, err
	}
	return yamlFile, nil
}

func isGzip(data []byte) bool {
	return len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b
}

func mergeConfigMaps(a, b map[string]interface{}, path string) (_ map[string]interface{}, err error) {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		aVal, aHasKey := a[k]
		bVal, bHasKey := b[k]

		_, aIsMap := a[k].(map[string]interface{})
		_, bIsMap := b[k].(map[string]interface{})

		if aHasKey && aVal == nil && bIsMap {
			aIsMap = true
			out[k] = make(map[string]interface{})
		}

		if bHasKey && bVal == nil && aIsMap {
			bIsMap = true
			v = make(map[string]interface{})
		}

		if aHasKey && aIsMap != bIsMap {
			return nil, errors.Errorf("conflicting types for %q: %T != %T", path+"."+k, a[k], b[k])
		}

		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k], err = mergeConfigMaps(bv, v, path+"."+k)
					if err != nil {
						return nil, err
					}
					continue
				}
			}
		}
		out[k] = v
	}
	return out, nil
}

func (om *Manager) setConfig(config interface{}) {
	om.configPtr.Store(&config)
}

func (om *Manager) callListeners(newValue interface{}) {
	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	for _, ch := range om.listeners {
		select {
		case ch <- newValue:
			// ok
		default:
			// nobody is listening or buffer full.
		}
	}
}

// Stop stops the Manager
func (om *Manager) stopping(_ error) error {
	om.listenersMtx.Lock()
	defer om.listenersMtx.Unlock()

	for _, ch := range om.listeners {
		close(ch)
	}
	om.listeners = nil
	return nil
}

// GetConfig returns last loaded config value, possibly nil.
func (om *Manager) GetConfig() interface{} {
	if p := om.configPtr.Load(); p != nil {
		return *p
	}
	return nil
}

func httpTransport(cfg Config, configName string, registerer prometheus.Registerer, logger log.Logger) http.RoundTripper {
	rt := http.DefaultTransport
	if cfg.HTTPClientClusterValidation.Label != "" {
		invalidClusterValidations := promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "client_invalid_cluster_validation_label_requests_total",
			Help: "Number of requests with invalid cluster validation label.",
			ConstLabels: map[string]string{
				"client":   "runtime-config/" + configName,
				"protocol": "http",
			},
		}, []string{"method"})
		reporter := func(msg string, method string) {
			level.Warn(logger).Log("msg", msg, "method", method, "cluster_validation_label", cfg.HTTPClientClusterValidation.Label, "component", "runtimeconfig", "load_path", cfg.LoadPath)
			invalidClusterValidations.WithLabelValues(method).Inc()
		}
		rt = middleware.ClusterValidationRoundTripper(cfg.HTTPClientClusterValidation.Label, reporter, http.DefaultTransport)
	}
	return rt
}
