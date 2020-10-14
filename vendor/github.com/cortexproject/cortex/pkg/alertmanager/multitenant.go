package alertmanager

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/cluster"
	amconfig "github.com/prometheus/alertmanager/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/alertmanager/alerts"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/services"
)

var backoffConfig = util.BackoffConfig{
	// Backoff for loading initial configuration set.
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: 2 * time.Second,
}

const (
	// If a config sets the webhook URL to this, it will be rewritten to
	// a URL derived from Config.AutoWebhookRoot
	autoWebhookURL = "http://internal.monitor"

	statusPage = `
<!doctype html>
<html>
	<head><title>Cortex Alertmanager Status</title></head>
	<body>
		<h1>Cortex Alertmanager Status</h1>
		<h2>Node</h2>
		<dl>
			<dt>Name</dt><dd>{{.self.Name}}</dd>
			<dt>Addr</dt><dd>{{.self.Addr}}</dd>
			<dt>Port</dt><dd>{{.self.Port}}</dd>
		</dl>
		<h3>Members</h3>
		{{ with .members }}
		<table>
		<tr><th>Name</th><th>Addr</th></tr>
		{{ range . }}
		<tr><td>{{ .Name }}</td><td>{{ .Addr }}</td></tr>
		{{ end }}
		</table>
		{{ else }}
		<p>No peers</p>
		{{ end }}
	</body>
</html>
`
)

var (
	statusTemplate *template.Template
)

func init() {
	statusTemplate = template.Must(template.New("statusPage").Funcs(map[string]interface{}{
		"state": func(enabled bool) string {
			if enabled {
				return "enabled"
			}
			return "disabled"
		},
	}).Parse(statusPage))
}

// MultitenantAlertmanagerConfig is the configuration for a multitenant Alertmanager.
type MultitenantAlertmanagerConfig struct {
	DataDir      string           `yaml:"data_dir"`
	Retention    time.Duration    `yaml:"retention"`
	ExternalURL  flagext.URLValue `yaml:"external_url"`
	PollInterval time.Duration    `yaml:"poll_interval"`

	ClusterBindAddr      string              `yaml:"cluster_bind_address"`
	ClusterAdvertiseAddr string              `yaml:"cluster_advertise_address"`
	Peers                flagext.StringSlice `yaml:"peers"`
	PeerTimeout          time.Duration       `yaml:"peer_timeout"`

	FallbackConfigFile string `yaml:"fallback_config_file"`
	AutoWebhookRoot    string `yaml:"auto_webhook_root"`

	Store AlertStoreConfig `yaml:"storage"`

	EnableAPI bool `yaml:"enable_api"`
}

const defaultClusterAddr = "0.0.0.0:9094"

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *MultitenantAlertmanagerConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.DataDir, "alertmanager.storage.path", "data/", "Base path for data storage.")
	f.DurationVar(&cfg.Retention, "alertmanager.storage.retention", 5*24*time.Hour, "How long to keep data for.")

	f.Var(&cfg.ExternalURL, "alertmanager.web.external-url", "The URL under which Alertmanager is externally reachable (for example, if Alertmanager is served via a reverse proxy). Used for generating relative and absolute links back to Alertmanager itself. If the URL has a path portion, it will be used to prefix all HTTP endpoints served by Alertmanager. If omitted, relevant URL components will be derived automatically.")

	f.StringVar(&cfg.FallbackConfigFile, "alertmanager.configs.fallback", "", "Filename of fallback config to use if none specified for instance.")
	f.StringVar(&cfg.AutoWebhookRoot, "alertmanager.configs.auto-webhook-root", "", "Root of URL to generate if config is "+autoWebhookURL)
	f.DurationVar(&cfg.PollInterval, "alertmanager.configs.poll-interval", 15*time.Second, "How frequently to poll Cortex configs")

	f.StringVar(&cfg.ClusterBindAddr, "cluster.listen-address", defaultClusterAddr, "Listen address for cluster.")
	f.StringVar(&cfg.ClusterAdvertiseAddr, "cluster.advertise-address", "", "Explicit address to advertise in cluster.")
	f.Var(&cfg.Peers, "cluster.peer", "Initial peers (may be repeated).")
	f.DurationVar(&cfg.PeerTimeout, "cluster.peer-timeout", time.Second*15, "Time to wait between peers to send notifications.")

	f.BoolVar(&cfg.EnableAPI, "experimental.alertmanager.enable-api", false, "Enable the experimental alertmanager config api.")

	cfg.Store.RegisterFlags(f)
}

type multitenantAlertmanagerMetrics struct {
	invalidConfig *prometheus.GaugeVec
}

func newMultitenantAlertmanagerMetrics(reg prometheus.Registerer) *multitenantAlertmanagerMetrics {
	m := &multitenantAlertmanagerMetrics{}

	m.invalidConfig = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "alertmanager_config_invalid",
		Help:      "Boolean set to 1 whenever the Alertmanager config is invalid for a user.",
	}, []string{"user"})

	return m
}

// A MultitenantAlertmanager manages Alertmanager instances for multiple
// organizations.
type MultitenantAlertmanager struct {
	services.Service

	cfg *MultitenantAlertmanagerConfig

	store AlertStore

	// The fallback config is stored as a string and parsed every time it's needed
	// because we mutate the parsed results and don't want those changes to take
	// effect here.
	fallbackConfig string

	// All the organization configurations that we have. Only used for instrumentation.
	cfgs map[string]alerts.AlertConfigDesc

	alertmanagersMtx sync.Mutex
	alertmanagers    map[string]*Alertmanager

	logger              log.Logger
	alertmanagerMetrics *alertmanagerMetrics
	multitenantMetrics  *multitenantAlertmanagerMetrics

	peer *cluster.Peer
}

// NewMultitenantAlertmanager creates a new MultitenantAlertmanager.
func NewMultitenantAlertmanager(cfg *MultitenantAlertmanagerConfig, logger log.Logger, registerer prometheus.Registerer) (*MultitenantAlertmanager, error) {
	err := os.MkdirAll(cfg.DataDir, 0777)
	if err != nil {
		return nil, fmt.Errorf("unable to create Alertmanager data directory %q: %s", cfg.DataDir, err)
	}

	if cfg.ExternalURL.URL == nil {
		return nil, fmt.Errorf("unable to create Alertmanager because the external URL has not been configured")
	}

	var fallbackConfig []byte
	if cfg.FallbackConfigFile != "" {
		fallbackConfig, err = ioutil.ReadFile(cfg.FallbackConfigFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read fallback config %q: %s", cfg.FallbackConfigFile, err)
		}
		_, err = amconfig.LoadFile(cfg.FallbackConfigFile)
		if err != nil {
			return nil, fmt.Errorf("unable to load fallback config %q: %s", cfg.FallbackConfigFile, err)
		}
	}

	var peer *cluster.Peer
	if cfg.ClusterBindAddr != "" {
		peer, err = cluster.Create(
			log.With(logger, "component", "cluster"),
			registerer,
			cfg.ClusterBindAddr,
			cfg.ClusterAdvertiseAddr,
			cfg.Peers,
			true,
			cluster.DefaultPushPullInterval,
			cluster.DefaultGossipInterval,
			cluster.DefaultTcpTimeout,
			cluster.DefaultProbeTimeout,
			cluster.DefaultProbeInterval,
		)
		if err != nil {
			return nil, errors.Wrap(err, "unable to initialize gossip mesh")
		}
		err = peer.Join(cluster.DefaultReconnectInterval, cluster.DefaultReconnectTimeout)
		if err != nil {
			level.Warn(logger).Log("msg", "unable to join gossip mesh", "err", err)
		}
		go peer.Settle(context.Background(), cluster.DefaultGossipInterval)
	}

	store, err := NewAlertStore(cfg.Store)
	if err != nil {
		return nil, err
	}

	return createMultitenantAlertmanager(cfg, fallbackConfig, peer, store, logger, registerer), nil
}

func createMultitenantAlertmanager(cfg *MultitenantAlertmanagerConfig, fallbackConfig []byte, peer *cluster.Peer, store AlertStore, logger log.Logger, registerer prometheus.Registerer) *MultitenantAlertmanager {
	am := &MultitenantAlertmanager{
		cfg:                 cfg,
		fallbackConfig:      string(fallbackConfig),
		cfgs:                map[string]alerts.AlertConfigDesc{},
		alertmanagers:       map[string]*Alertmanager{},
		alertmanagerMetrics: newAlertmanagerMetrics(),
		multitenantMetrics:  newMultitenantAlertmanagerMetrics(registerer),
		peer:                peer,
		store:               store,
		logger:              log.With(logger, "component", "MultiTenantAlertmanager"),
	}

	if registerer != nil {
		registerer.MustRegister(am.alertmanagerMetrics)
	}

	am.Service = services.NewTimerService(am.cfg.PollInterval, am.starting, am.iteration, am.stopping)
	return am
}

func (am *MultitenantAlertmanager) starting(ctx context.Context) error {
	// Load initial set of all configurations before polling for new ones.
	am.syncConfigs(am.loadAllConfigs())
	return nil
}

func (am *MultitenantAlertmanager) iteration(ctx context.Context) error {
	err := am.updateConfigs()
	if err != nil {
		level.Warn(am.logger).Log("msg", "error updating configs", "err", err)
	}
	// Returning error here would stop "MultitenantAlertmanager" service completely,
	// so we return nil to keep service running.
	return nil
}

// stopping runs when MultitenantAlertmanager transitions to Stopping state.
func (am *MultitenantAlertmanager) stopping(_ error) error {
	am.alertmanagersMtx.Lock()
	for _, am := range am.alertmanagers {
		am.Stop()
	}
	am.alertmanagersMtx.Unlock()
	err := am.peer.Leave(am.cfg.PeerTimeout)
	if err != nil {
		level.Warn(am.logger).Log("msg", "failed to leave the cluster", "err", err)
	}
	level.Debug(am.logger).Log("msg", "stopping")
	return nil
}

// Load the full set of configurations from the alert store, retrying with backoff
// until we can get them.
func (am *MultitenantAlertmanager) loadAllConfigs() map[string]alerts.AlertConfigDesc {
	backoff := util.NewBackoff(context.Background(), backoffConfig)
	for {
		cfgs, err := am.poll()
		if err == nil {
			level.Debug(am.logger).Log("msg", "initial configuration load", "num_configs", len(cfgs))
			return cfgs
		}
		level.Warn(am.logger).Log("msg", "error fetching all configurations, backing off", "err", err)
		backoff.Wait()
	}
}

func (am *MultitenantAlertmanager) updateConfigs() error {
	cfgs, err := am.poll()
	if err != nil {
		return err
	}
	am.syncConfigs(cfgs)
	return nil
}

// poll the alert store. Not re-entrant.
func (am *MultitenantAlertmanager) poll() (map[string]alerts.AlertConfigDesc, error) {
	cfgs, err := am.store.ListAlertConfigs(context.Background())
	if err != nil {
		return nil, err
	}
	return cfgs, nil
}

func (am *MultitenantAlertmanager) syncConfigs(cfgs map[string]alerts.AlertConfigDesc) {
	level.Debug(am.logger).Log("msg", "adding configurations", "num_configs", len(cfgs))
	for user, cfg := range cfgs {
		err := am.setConfig(cfg)
		if err != nil {
			am.multitenantMetrics.invalidConfig.WithLabelValues(user).Set(float64(1))
			level.Warn(am.logger).Log("msg", "error applying config", "err", err)
			continue
		}

		am.multitenantMetrics.invalidConfig.WithLabelValues(user).Set(float64(0))
	}

	am.alertmanagersMtx.Lock()
	defer am.alertmanagersMtx.Unlock()
	for user, userAM := range am.alertmanagers {
		if _, exists := cfgs[user]; !exists {
			// The user alertmanager is only paused in order to retain the prometheus metrics
			// it has reported to its registry. If a new config for this user appears, this structure
			// will be reused.
			level.Info(am.logger).Log("msg", "deactivating per-tenant alertmanager", "user", user)
			userAM.Pause()
			delete(am.cfgs, user)
			am.multitenantMetrics.invalidConfig.DeleteLabelValues(user)
			level.Info(am.logger).Log("msg", "deactivated per-tenant alertmanager", "user", user)
		}
	}
}

func (am *MultitenantAlertmanager) transformConfig(userID string, amConfig *amconfig.Config) (*amconfig.Config, error) {
	if amConfig == nil { // shouldn't happen, but check just in case
		return nil, fmt.Errorf("no usable Cortex configuration for %v", userID)
	}
	if am.cfg.AutoWebhookRoot != "" {
		for _, r := range amConfig.Receivers {
			for _, w := range r.WebhookConfigs {
				if w.URL.String() == autoWebhookURL {
					u, err := url.Parse(am.cfg.AutoWebhookRoot + "/" + userID + "/monitor")
					if err != nil {
						return nil, err
					}
					w.URL = &amconfig.URL{URL: u}
				}
			}
		}
	}

	return amConfig, nil
}

// setConfig applies the given configuration to the alertmanager for `userID`,
// creating an alertmanager if it doesn't already exist.
func (am *MultitenantAlertmanager) setConfig(cfg alerts.AlertConfigDesc) error {
	am.alertmanagersMtx.Lock()
	existing, hasExisting := am.alertmanagers[cfg.User]
	am.alertmanagersMtx.Unlock()
	var userAmConfig *amconfig.Config
	var err error
	var hasTemplateChanges bool

	for _, tmpl := range cfg.Templates {
		hasChanged, err := createTemplateFile(am.cfg.DataDir, cfg.User, tmpl.Filename, tmpl.Body)
		if err != nil {
			return err
		}

		if hasChanged {
			hasTemplateChanges = true
		}
	}

	level.Debug(am.logger).Log("msg", "setting config", "user", cfg.User)

	if cfg.RawConfig == "" {
		if am.fallbackConfig == "" {
			return fmt.Errorf("blank Alertmanager configuration for %v", cfg.User)
		}
		level.Info(am.logger).Log("msg", "blank Alertmanager configuration; using fallback", "user", cfg.User)
		userAmConfig, err = amconfig.Load(am.fallbackConfig)
		if err != nil {
			return fmt.Errorf("unable to load fallback configuration for %v: %v", cfg.User, err)
		}
	} else {
		userAmConfig, err = amconfig.Load(cfg.RawConfig)
		if err != nil && hasExisting {
			// XXX: This means that if a user has a working configuration and
			// they submit a broken one, we'll keep processing the last known
			// working configuration, and they'll never know.
			// TODO: Provide a way of communicating this to the user and for removing
			// Alertmanager instances.
			return fmt.Errorf("invalid Cortex configuration for %v: %v", cfg.User, err)
		}
	}

	if userAmConfig, err = am.transformConfig(cfg.User, userAmConfig); err != nil {
		return err
	}

	// If no Alertmanager instance exists for this user yet, start one.
	if !hasExisting {
		level.Debug(am.logger).Log("msg", "initializing new per-tenant alertmanager", "user", cfg.User)
		newAM, err := am.newAlertmanager(cfg.User, userAmConfig)
		if err != nil {
			return err
		}
		am.alertmanagersMtx.Lock()
		am.alertmanagers[cfg.User] = newAM
		am.alertmanagersMtx.Unlock()
	} else if am.cfgs[cfg.User].RawConfig != cfg.RawConfig || hasTemplateChanges {
		level.Info(am.logger).Log("msg", "updating new per-tenant alertmanager", "user", cfg.User)
		// If the config changed, apply the new one.
		err := existing.ApplyConfig(cfg.User, userAmConfig)
		if err != nil {
			return fmt.Errorf("unable to apply Alertmanager config for user %v: %v", cfg.User, err)
		}
	}
	am.cfgs[cfg.User] = cfg
	return nil
}

func (am *MultitenantAlertmanager) newAlertmanager(userID string, amConfig *amconfig.Config) (*Alertmanager, error) {
	reg := prometheus.NewRegistry()
	newAM, err := New(&Config{
		UserID:      userID,
		DataDir:     am.cfg.DataDir,
		Logger:      util.Logger,
		Peer:        am.peer,
		PeerTimeout: am.cfg.PeerTimeout,
		Retention:   am.cfg.Retention,
		ExternalURL: am.cfg.ExternalURL.URL,
	}, reg)
	if err != nil {
		return nil, fmt.Errorf("unable to start Alertmanager for user %v: %v", userID, err)
	}

	if err := newAM.ApplyConfig(userID, amConfig); err != nil {
		return nil, fmt.Errorf("unable to apply initial config for user %v: %v", userID, err)
	}

	am.alertmanagerMetrics.addUserRegistry(userID, reg)
	return newAM, nil
}

// ServeHTTP serves the Alertmanager's web UI and API.
func (am *MultitenantAlertmanager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	userID, _, err := user.ExtractOrgIDFromHTTPRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	am.alertmanagersMtx.Lock()
	userAM, ok := am.alertmanagers[userID]
	am.alertmanagersMtx.Unlock()

	if ok {
		if !userAM.IsActive() {
			http.Error(w, "the Alertmanager is not configured", http.StatusNotFound)
			return
		}

		userAM.mux.ServeHTTP(w, req)
		return
	}

	if am.fallbackConfig != "" {
		userAM, err = am.alertmanagerFromFallbackConfig(userID)
		if err != nil {
			http.Error(w, "Failed to initialize the Alertmanager", http.StatusInternalServerError)
			return
		}

		userAM.mux.ServeHTTP(w, req)
		return
	}

	http.Error(w, "the Alertmanager is not configured", http.StatusNotFound)
}

func (am *MultitenantAlertmanager) alertmanagerFromFallbackConfig(userID string) (*Alertmanager, error) {
	// Upload an empty config so that the Alertmanager is no de-activated in the next poll
	cfgDesc := alerts.ToProto("", nil, userID)
	err := am.store.SetAlertConfig(context.Background(), cfgDesc)
	if err != nil {
		return nil, err
	}

	// Calling setConfig with an empty configuration will use the fallback config.
	err = am.setConfig(cfgDesc)
	if err != nil {
		return nil, err
	}

	am.alertmanagersMtx.Lock()
	defer am.alertmanagersMtx.Unlock()
	return am.alertmanagers[userID], nil
}

// GetStatusHandler returns the status handler for this multi-tenant
// alertmanager.
func (am *MultitenantAlertmanager) GetStatusHandler() StatusHandler {
	return StatusHandler{
		am: am,
	}
}

// StatusHandler shows the status of the alertmanager.
type StatusHandler struct {
	am *MultitenantAlertmanager
}

// ServeHTTP serves the status of the alertmanager.
func (s StatusHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	err := statusTemplate.Execute(w, s.am.peer.Info())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func createTemplateFile(dataDir, userID, fn, content string) (bool, error) {
	dir := filepath.Join(dataDir, "templates", userID, filepath.Dir(fn))
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return false, fmt.Errorf("unable to create Alertmanager templates directory %q: %s", dir, err)
	}

	file := filepath.Join(dir, fn)
	// Check if the template file already exists and if it has changed
	if tmpl, err := ioutil.ReadFile(file); err == nil && string(tmpl) == content {
		return false, nil
	}

	if err := ioutil.WriteFile(file, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("unable to create Alertmanager template file %q: %s", file, err)
	}

	return true, nil
}
