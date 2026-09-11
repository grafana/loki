package runtimeconfig

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
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

// MapLoader loads the configuration from a map.
// See [github.com/grafana/dskit/runtimeconfig/mapstructure.Decode] for a proposed implementation.
type MapLoader func(m map[string]interface{}) (interface{}, error)

// Config holds the config for an Manager instance.
// It holds config related to loading per-tenant config.
type Config struct {
	ReloadPeriod time.Duration `yaml:"period" category:"advanced"`
	// LoadPath contains the path to the runtime config files or HTTP URLs.
	// Requires a non-empty value
	LoadPath     flagext.StringSliceCSV `yaml:"file"`
	Preprocessor Preprocessor           `yaml:"-"`
	Loader       Loader                 `yaml:"-"`
	// MapLoader, if set, is used instead of Loader and receives the merged
	// configuration as a map[string]any directly.
	// See [github.com/grafana/dskit/runtimeconfig/mapstructure.Decode] for a proposed implementation.
	MapLoader MapLoader `yaml:"-"`

	// Configurations related to fetching runtime configurations from HTTP URLs rather than local files.
	HTTPClientTimeout           time.Duration                       `yaml:"http_client_timeout" category:"advanced"`
	HTTPClientClusterValidation clusterutil.ClusterValidationConfig `yaml:"http_client_cluster_validation" category:"advanced"`
	// HTTPClientDisableKeepAlives disables HTTP keep-alives for the runtime config HTTP client.
	HTTPClientDisableKeepAlives bool `yaml:"http_client_disable_keep_alives" category:"advanced"`
}

// RegisterFlagsWithPrefix registers flags under the specified prefix, which could be empty.
// If a non-empty prefix is provided, it's expected to end with a dot.
func (mc *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Var(&mc.LoadPath, prefix+"file", "Comma separated list of yaml files or URLs with the configuration that can be updated at runtime. Runtime config files will be merged from left to right. An entry can end with semicolon-separated parameters that say what happens when it cannot be read: \";optional-on-startup\" lets the process start without it, but a later failure still fails the reload; \";optional-keep-last-value-on-failure\" also lets the process start without it, and a later failure keeps the value the source supplied last. Without a parameter, a source that cannot be read fails the load. Quote the value in a shell, because \";\" starts a new command.")
	f.DurationVar(&mc.ReloadPeriod, prefix+"reload-period", 10*time.Second, "How often to check runtime config files.")
	f.DurationVar(&mc.HTTPClientTimeout, prefix+"http-client-timeout", 30*time.Second, "HTTP client timeout when fetching runtime config from URLs.")
	f.BoolVar(&mc.HTTPClientDisableKeepAlives, prefix+"http-client-disable-keep-alives", true, "Disable HTTP keep-alives for the runtime config HTTP client. When enabled, each reload opens a new connection, which prevents long-lived connections from being pinned to a single backend when the runtime config URL is served by multiple replicas behind a connection-level (L4) load balancer, such as a Kubernetes Service.")
	mc.HTTPClientClusterValidation.RegisterFlagsWithPrefix(prefix+"http-client-cluster-validation.", f)
}

// RegisterFlags registers flags.
func (mc *Config) RegisterFlags(f *flag.FlagSet) {
	mc.RegisterFlagsWithPrefix("runtime-config.", f)
}

// configSource is one LoadPath entry: where to read from, how that source
// behaves, and the state loadConfig keeps for it.
//
// path and parameters are set by parseConfigSource, sourceID by assignSourceIDs, and
// provider in New. loadConfig updates lastValue and lastDigest; they need no
// synchronization because only loadConfig touches them, and only in the Starting and
// Running states.
type configSource struct {
	path       string
	provider   provider
	parameters sourceParameters

	// How this source identifies itself in metrics, as the value of the "source"
	// label and of the histogram's "url" label.
	sourceID string

	// Bytes last applied from this source. Stays nil unless the source keeps its
	// last value on failure, to not waste memory.
	lastValue []byte

	// Digest of the bytes this source contributed to the last applied merge,
	// zero when it contributed nothing. A digest of real bytes is never zero,
	// so the zero value also covers a source that has never contributed.
	lastDigest [sha256.Size]byte
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
	sourceLoadSuccess *prometheus.GaugeVec
	configHash        *prometheus.GaugeVec

	configSources []configSource
}

// New creates an instance of Manager. Manager is a services.Service, and must be explicitly started to perform any work.
func New(cfg Config, configName string, registerer prometheus.Registerer, logger log.Logger) (*Manager, error) {
	if len(cfg.LoadPath) == 0 {
		return nil, errors.New("LoadPath is empty")
	}

	// Parse every entry before registering any metric.
	configSources := make([]configSource, 0, len(cfg.LoadPath))
	for _, entry := range cfg.LoadPath {
		cs, err := parseConfigSource(entry)
		if err != nil {
			return nil, err
		}
		configSources = append(configSources, cs)
	}
	if err := assignSourceIDs(configSources); err != nil {
		return nil, err
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
		// The "source" label comes from assignSourceIDs, which drops the userinfo, query,
		// and fragment of a URL. A secret in the path still reaches /metrics.
		sourceLoadSuccess: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Name: "runtime_config_source_last_reload_successful",
			Help: "Whether the last read of each individual runtime-config source was successful. A source whose failure is tolerated can be 0 while runtime_config_last_reload_successful is 1.",
		}, []string{"source"}),
		configHash: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Name: "runtime_config_hash",
			Help: "Hash of the currently active runtime configuration, merged from all configured files.",
		}, []string{"sha256"}),
		logger: logger,
	}

	var httpClient *http.Client
	var httpDuration *prometheus.HistogramVec
	for i := range configSources {
		cs := &configSources[i]
		if isURL(cs.path) {
			if httpClient == nil {
				timeout := cfg.HTTPClientTimeout
				if timeout == 0 {
					timeout = 30 * time.Second
				}
				httpClient = &http.Client{Timeout: timeout, Transport: httpTransport(cfg, configName, clusterValidationRegisterer, logger)}
				httpDuration = newHTTPRequestDuration(registerer)
			}
			cs.provider = newHTTPProvider(cs.path, cs.sourceID, httpClient, httpDuration)
		} else {
			cs.provider = newFileProvider(cs.path)
		}

		// Create the series up front, at 0: nothing has been read yet.
		mgr.sourceLoadSuccess.WithLabelValues(cs.sourceID).Set(0)
	}
	mgr.configSources = configSources

	mgr.Service = services.NewBasicService(mgr.starting, mgr.loop, mgr.stopping)
	return &mgr, nil
}

func (om *Manager) starting(ctx context.Context) error {
	if len(om.cfg.LoadPath) == 0 {
		return nil
	}

	return errors.Wrap(om.loadConfig(ctx, true), "failed to load runtime config")
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
			err := om.loadConfig(ctx, false)
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
//
// initial must be true for the load performed while the Manager starts: some sources tolerate a
// failure only then.
func (om *Manager) loadConfig(ctx context.Context, initial bool) error {
	// One entry per config source, holding the result of reading it during this
	// load attempt. A source that contributes nothing keeps a zero digest, which is
	// what configSource.lastDigest expects for change detection.
	sourcesToLoad := make([]struct {
		name    string
		rawData []byte
		// readOK is true for sources read fresh this attempt, as opposed to
		// replaying their last value. Only used to decide what to retain.
		readOK bool
		// skipInMerge is true for a source that takes no part in the merge: a
		// tolerated failure with no last value to replay.
		skipInMerge bool
		digest      [sha256.Size]byte
	}, len(om.configSources))

	for i := range om.configSources {
		cs := &om.configSources[i]
		s := &sourcesToLoad[i]
		s.name = cs.provider.Name()

		buf, err := cs.provider.Read(ctx)
		if err == nil && om.cfg.Preprocessor != nil {
			buf, err = om.cfg.Preprocessor(buf)
			if err != nil {
				err = errors.Wrapf(err, "preprocess %q", s.name)
			}
		} else if err != nil {
			err = errors.Wrapf(err, "read %q", s.name)
		}

		if err != nil {
			om.sourceLoadSuccess.WithLabelValues(cs.sourceID).Set(0)

			if !cs.parameters.toleratesFailure(initial) {
				om.configLoadSuccess.Set(0)
				return err
			}

			level.Warn(om.logger).Log("msg", "failed to load runtime config source, continuing anyway", "source", s.name, "parameters", cs.parameters.String(), "err", err)

			if cs.parameters.keepsLastValueOnFailure() && cs.lastValue != nil {
				s.rawData = cs.lastValue
				s.digest = sha256.Sum256(s.rawData)
			} else {
				s.skipInMerge = true
			}
			continue
		}

		om.sourceLoadSuccess.WithLabelValues(cs.sourceID).Set(1)
		s.readOK = true
		s.rawData = buf
		s.digest = sha256.Sum256(buf)
	}

	// Skip the rebuild when nothing changed, but never on the initial load:
	// lastDigest is zero for every source then, which would look equal to a
	// load where nothing contributes, and GetConfig must be the Loader's
	// result rather than nil.
	if !initial {
		unchanged := true
		for i, cs := range om.configSources {
			if cs.lastDigest != sourcesToLoad[i].digest {
				unchanged = false
				break
			}
		}
		if unchanged {
			om.configLoadSuccess.Set(1)
			return nil
		}
	}

	mergedConfig := map[string]interface{}{}
	for i := range om.configSources {
		cs := &om.configSources[i]
		s := sourcesToLoad[i]
		if s.skipInMerge {
			continue
		}

		yamlFile, err := om.unmarshalMaybeGzipped(s.name, s.rawData)
		if err != nil {
			om.sourceLoadSuccess.WithLabelValues(cs.sourceID).Set(0)
			om.configLoadSuccess.Set(0)
			return errors.Wrapf(err, "unmarshal %q", s.name)
		}
		mergedConfig, err = mergeConfigMaps(mergedConfig, yamlFile, "")
		if err != nil {
			om.sourceLoadSuccess.WithLabelValues(cs.sourceID).Set(0)
			om.configLoadSuccess.Set(0)
			return errors.Wrapf(err, "can't merge %q on top of the previous providers", s.name)
		}
	}

	var (
		cfg  interface{}
		hash [sha256.Size]byte
		err  error
	)
	if om.cfg.MapLoader != nil {
		// There are no merged bytes to hash, so hash the name and digest of every
		// source that contributed. The name length goes in first, otherwise a
		// different split of the same bytes across sources hashes the same.
		h := sha256.New()
		var nameLength [8]byte
		for _, s := range sourcesToLoad {
			if s.skipInMerge {
				continue
			}
			binary.BigEndian.PutUint64(nameLength[:], uint64(len(s.name)))
			_, _ = h.Write(nameLength[:])
			_, _ = io.WriteString(h, s.name)
			_, _ = h.Write(s.digest[:])
		}
		copy(hash[:], h.Sum(nil))

		cfg, err = om.cfg.MapLoader(mergedConfig)
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrap(err, "load file")
		}
	} else {
		buf, err := yaml.Marshal(mergedConfig)
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrap(err, "marshal file")
		}

		hash = sha256.Sum256(buf)
		cfg, err = om.cfg.Loader(bytes.NewReader(buf))
		if err != nil {
			om.configLoadSuccess.Set(0)
			return errors.Wrap(err, "load file")
		}
	}
	om.configLoadSuccess.Set(1)

	om.setConfig(cfg)
	om.callListeners(cfg)

	// expose hash of runtime config
	om.configHash.Reset()
	om.configHash.WithLabelValues(fmt.Sprintf("%x", hash)).Set(1)

	// Preserve per-source merge state for the next loop. Only sources that can
	// replay their last value keep bytes, and only bytes this load applied, so a
	// body that failed to unmarshal cannot poison the replay.
	for i := range om.configSources {
		s := sourcesToLoad[i]
		om.configSources[i].lastDigest = s.digest
		if s.readOK && om.configSources[i].parameters.keepsLastValueOnFailure() {
			om.configSources[i].lastValue = bytes.Clone(s.rawData)
		}
	}
	return nil
}

func (om *Manager) unmarshalMaybeGzipped(filename string, data []byte) (map[string]any, error) {
	if strings.HasSuffix(filename, ".gz") {
		yamlFile := map[string]any{}
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, errors.Wrap(err, "read gzipped file")
		}
		defer r.Close()
		err = yaml.NewDecoder(r).Decode(&yamlFile)
		return yamlFile, errors.Wrap(err, "uncompress/unmarshal gzipped file")
	}

	m, err := unmarshalJSONOrYAML(data)
	if err != nil {
		// Give a hint if we think that file is gzipped.
		if isGzip(data) {
			return nil, errors.Wrap(err, "file looks gzipped but doesn't have a .gz extension")
		}
		return nil, err
	}
	return m, nil
}

// unmarshalJSONOrYAML decodes data into a map. If the data appears to be a JSON
// object (its first non-whitespace byte is '{'), it is decoded with
// encoding/json, which is faster. YAML is used as a fallback
func unmarshalJSONOrYAML(data []byte) (map[string]any, error) {
	if looksLikeJSONObject(data) {
		m := map[string]any{}
		if err := json.Unmarshal(data, &m); err == nil {
			return m, nil
		}
	}

	m := map[string]any{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func looksLikeJSONObject(data []byte) bool {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = cfg.HTTPClientDisableKeepAlives

	var rt http.RoundTripper = transport
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
		rt = middleware.ClusterValidationRoundTripper(cfg.HTTPClientClusterValidation.Label, reporter, transport)
	}
	return rt
}
