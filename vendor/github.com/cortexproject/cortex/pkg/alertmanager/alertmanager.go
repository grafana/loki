package alertmanager

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/alertmanager/api"
	"github.com/prometheus/alertmanager/cluster"
	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/dispatch"
	"github.com/prometheus/alertmanager/inhibit"
	"github.com/prometheus/alertmanager/nflog"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/notify/email"
	"github.com/prometheus/alertmanager/notify/opsgenie"
	"github.com/prometheus/alertmanager/notify/pagerduty"
	"github.com/prometheus/alertmanager/notify/pushover"
	"github.com/prometheus/alertmanager/notify/slack"
	"github.com/prometheus/alertmanager/notify/victorops"
	"github.com/prometheus/alertmanager/notify/webhook"
	"github.com/prometheus/alertmanager/notify/wechat"
	"github.com/prometheus/alertmanager/provider/mem"
	"github.com/prometheus/alertmanager/silence"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/alertmanager/ui"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/route"
)

const notificationLogMaintenancePeriod = 15 * time.Minute

// Config configures an Alertmanager.
type Config struct {
	UserID string
	// Used to persist notification logs and silences on disk.
	DataDir     string
	Logger      log.Logger
	Peer        *cluster.Peer
	PeerTimeout time.Duration
	Retention   time.Duration
	ExternalURL *url.URL
}

// An Alertmanager manages the alerts for one user.
type Alertmanager struct {
	cfg             *Config
	api             *api.API
	logger          log.Logger
	nflog           *nflog.Log
	silences        *silence.Silences
	marker          types.Marker
	alerts          *mem.Alerts
	dispatcher      *dispatch.Dispatcher
	inhibitor       *inhibit.Inhibitor
	pipelineBuilder *notify.PipelineBuilder
	stop            chan struct{}
	wg              sync.WaitGroup
	mux             *http.ServeMux
	registry        *prometheus.Registry

	// The Dispatcher is the only component we need to recreate when we call ApplyConfig.
	// Given its metrics don't have any variable labels we need to re-use the same metrics.
	dispatcherMetrics *dispatch.DispatcherMetrics
	// This needs to be set to the hash of the config. All the hashes need to be same
	// for deduping of alerts to work, hence we need this metric. See https://github.com/prometheus/alertmanager/issues/596
	// Further, in upstream AM, this metric is handled using the config coordinator which we don't use
	// hence we need to generate the metric ourselves.
	configHashMetric prometheus.Gauge

	activeMtx sync.Mutex
	active    bool
}

var (
	webReload = make(chan chan error)
)

func init() {
	go func() {
		// Since this is not a "normal" Alertmanager which reads its config
		// from disk, we just accept and ignore web-based reload signals. Config
		// updates are only applied externally via ApplyConfig().
		for range webReload {
		}
	}()
}

// New creates a new Alertmanager.
func New(cfg *Config, reg *prometheus.Registry) (*Alertmanager, error) {
	am := &Alertmanager{
		cfg:       cfg,
		logger:    log.With(cfg.Logger, "user", cfg.UserID),
		stop:      make(chan struct{}),
		active:    false,
		activeMtx: sync.Mutex{},
		configHashMetric: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "alertmanager_config_hash",
			Help: "Hash of the currently loaded alertmanager configuration.",
		}),
	}

	am.registry = reg

	am.wg.Add(1)
	nflogID := fmt.Sprintf("nflog:%s", cfg.UserID)
	var err error
	am.nflog, err = nflog.New(
		nflog.WithRetention(cfg.Retention),
		nflog.WithSnapshot(filepath.Join(cfg.DataDir, nflogID)),
		nflog.WithMaintenance(notificationLogMaintenancePeriod, am.stop, am.wg.Done),
		nflog.WithMetrics(am.registry),
		nflog.WithLogger(log.With(am.logger, "component", "nflog")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create notification log: %v", err)
	}
	if cfg.Peer != nil {
		c := cfg.Peer.AddState("nfl:"+cfg.UserID, am.nflog, am.registry)
		am.nflog.SetBroadcast(c.Broadcast)
	}

	am.marker = types.NewMarker(am.registry)

	silencesID := fmt.Sprintf("silences:%s", cfg.UserID)
	am.silences, err = silence.New(silence.Options{
		SnapshotFile: filepath.Join(cfg.DataDir, silencesID),
		Retention:    cfg.Retention,
		Logger:       log.With(am.logger, "component", "silences"),
		Metrics:      am.registry,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create silences: %v", err)
	}
	if cfg.Peer != nil {
		c := cfg.Peer.AddState("sil:"+cfg.UserID, am.silences, am.registry)
		am.silences.SetBroadcast(c.Broadcast)
	}

	am.pipelineBuilder = notify.NewPipelineBuilder(am.registry)

	am.wg.Add(1)
	go func() {
		am.silences.Maintenance(15*time.Minute, filepath.Join(cfg.DataDir, silencesID), am.stop)
		am.wg.Done()
	}()

	am.alerts, err = mem.NewAlerts(context.Background(), am.marker, 30*time.Minute, am.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create alerts: %v", err)
	}

	am.api, err = api.New(api.Options{
		Alerts:     am.alerts,
		Silences:   am.silences,
		StatusFunc: am.marker.Status,
		Peer:       cfg.Peer,
		Registry:   am.registry,
		Logger:     log.With(am.logger, "component", "api"),
		GroupFunc: func(f1 func(*dispatch.Route) bool, f2 func(*types.Alert, time.Time) bool) (dispatch.AlertGroups, map[model.Fingerprint][]string) {
			return am.dispatcher.Groups(f1, f2)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create api: %v", err)
	}

	router := route.New().WithPrefix(am.cfg.ExternalURL.Path)

	ui.Register(router, webReload, log.With(am.logger, "component", "ui"))
	am.mux = am.api.Register(router, am.cfg.ExternalURL.Path)

	// Override some extra paths registered in the router (eg. /metrics which by default exposes prometheus.DefaultRegisterer).
	// Entire router is registered in Mux to "/" path, so there is no conflict with overwriting specific paths.
	for _, p := range []string{"/metrics", "/-/reload", "/debug/"} {
		a := path.Join(am.cfg.ExternalURL.Path, p)
		// Preserve end slash, as for Mux it means entire subtree.
		if strings.HasSuffix(p, "/") {
			a = a + "/"
		}
		am.mux.Handle(a, http.NotFoundHandler())
	}

	am.dispatcherMetrics = dispatch.NewDispatcherMetrics(am.registry)
	return am, nil
}

// clusterWait returns a function that inspects the current peer state and returns
// a duration of one base timeout for each peer with a higher ID than ourselves.
func clusterWait(p *cluster.Peer, timeout time.Duration) func() time.Duration {
	return func() time.Duration {
		return time.Duration(p.Position()) * timeout
	}
}

// ApplyConfig applies a new configuration to an Alertmanager.
func (am *Alertmanager) ApplyConfig(userID string, conf *config.Config, rawCfg string) error {
	templateFiles := make([]string, len(conf.Templates))
	if len(conf.Templates) > 0 {
		for i, t := range conf.Templates {
			templateFiles[i] = filepath.Join(am.cfg.DataDir, "templates", userID, t)
		}
	}

	tmpl, err := template.FromGlobs(templateFiles...)
	if err != nil {
		return err
	}
	tmpl.ExternalURL = am.cfg.ExternalURL

	am.api.Update(conf, func(_ model.LabelSet) {})

	// Ensure inhibitor is set before being called
	if am.inhibitor != nil {
		am.inhibitor.Stop()
	}

	// Ensure dispatcher is set before being called
	if am.dispatcher != nil {
		am.dispatcher.Stop()
	}

	am.inhibitor = inhibit.NewInhibitor(am.alerts, conf.InhibitRules, am.marker, log.With(am.logger, "component", "inhibitor"))

	waitFunc := clusterWait(am.cfg.Peer, am.cfg.PeerTimeout)
	timeoutFunc := func(d time.Duration) time.Duration {
		if d < notify.MinTimeout {
			d = notify.MinTimeout
		}
		return d + waitFunc()
	}

	integrationsMap, err := buildIntegrationsMap(conf.Receivers, tmpl, am.logger)
	if err != nil {
		return nil
	}

	pipeline := am.pipelineBuilder.New(
		integrationsMap,
		waitFunc,
		am.inhibitor,
		silence.NewSilencer(am.silences, am.marker, am.logger),
		am.nflog,
		am.cfg.Peer,
	)
	am.dispatcher = dispatch.NewDispatcher(
		am.alerts,
		dispatch.NewRoute(conf.Route, nil),
		pipeline,
		am.marker,
		timeoutFunc,
		log.With(am.logger, "component", "dispatcher"),
		am.dispatcherMetrics,
	)

	go am.dispatcher.Run()
	go am.inhibitor.Run()

	// Ensure the alertmanager is set to active
	am.activeMtx.Lock()
	am.active = true
	am.activeMtx.Unlock()

	am.configHashMetric.Set(md5HashAsMetricValue([]byte(rawCfg)))
	return nil
}

// IsActive returns if the alertmanager is currently running
// or is paused
func (am *Alertmanager) IsActive() bool {
	am.activeMtx.Lock()
	defer am.activeMtx.Unlock()
	return am.active
}

// Pause running jobs in the alertmanager that are able to be restarted and sets
// to inactives
func (am *Alertmanager) Pause() {
	// Set to inactive
	am.activeMtx.Lock()
	am.active = false
	am.activeMtx.Unlock()

	// Stop the inhibitor and dispatcher which will be recreated when
	// a new config is applied
	if am.inhibitor != nil {
		am.inhibitor.Stop()
		am.inhibitor = nil
	}
	if am.dispatcher != nil {
		am.dispatcher.Stop()
		am.dispatcher = nil
	}

	// Remove all of the active silences from the alertmanager
	silences, _, err := am.silences.Query()
	if err != nil {
		level.Warn(am.logger).Log("msg", "unable to retrieve silences for removal", "err", err)
	}
	for _, si := range silences {
		err = am.silences.Expire(si.Id)
		if err != nil {
			level.Warn(am.logger).Log("msg", "unable to remove silence", "err", err, "silence", si.Id)
		}
	}
}

// Stop stops the Alertmanager.
func (am *Alertmanager) Stop() {
	if am.inhibitor != nil {
		am.inhibitor.Stop()
	}

	if am.dispatcher != nil {
		am.dispatcher.Stop()
	}

	am.alerts.Close()
	close(am.stop)
	am.wg.Wait()
}

// buildIntegrationsMap builds a map of name to the list of integration notifiers off of a
// list of receiver config.
func buildIntegrationsMap(nc []*config.Receiver, tmpl *template.Template, logger log.Logger) (map[string][]notify.Integration, error) {
	integrationsMap := make(map[string][]notify.Integration, len(nc))
	for _, rcv := range nc {
		integrations, err := buildReceiverIntegrations(rcv, tmpl, logger)
		if err != nil {
			return nil, err
		}
		integrationsMap[rcv.Name] = integrations
	}
	return integrationsMap, nil
}

// buildReceiverIntegrations builds a list of integration notifiers off of a
// receiver config.
// Taken from https://github.com/prometheus/alertmanager/blob/94d875f1227b29abece661db1a68c001122d1da5/cmd/alertmanager/main.go#L112-L159.
func buildReceiverIntegrations(nc *config.Receiver, tmpl *template.Template, logger log.Logger) ([]notify.Integration, error) {
	var (
		errs         types.MultiError
		integrations []notify.Integration
		add          = func(name string, i int, rs notify.ResolvedSender, f func(l log.Logger) (notify.Notifier, error)) {
			n, err := f(log.With(logger, "integration", name))
			if err != nil {
				errs.Add(err)
				return
			}
			integrations = append(integrations, notify.NewIntegration(n, rs, name, i))
		}
	)

	for i, c := range nc.WebhookConfigs {
		add("webhook", i, c, func(l log.Logger) (notify.Notifier, error) { return webhook.New(c, tmpl, l) })
	}
	for i, c := range nc.EmailConfigs {
		add("email", i, c, func(l log.Logger) (notify.Notifier, error) { return email.New(c, tmpl, l), nil })
	}
	for i, c := range nc.PagerdutyConfigs {
		add("pagerduty", i, c, func(l log.Logger) (notify.Notifier, error) { return pagerduty.New(c, tmpl, l) })
	}
	for i, c := range nc.OpsGenieConfigs {
		add("opsgenie", i, c, func(l log.Logger) (notify.Notifier, error) { return opsgenie.New(c, tmpl, l) })
	}
	for i, c := range nc.WechatConfigs {
		add("wechat", i, c, func(l log.Logger) (notify.Notifier, error) { return wechat.New(c, tmpl, l) })
	}
	for i, c := range nc.SlackConfigs {
		add("slack", i, c, func(l log.Logger) (notify.Notifier, error) { return slack.New(c, tmpl, l) })
	}
	for i, c := range nc.VictorOpsConfigs {
		add("victorops", i, c, func(l log.Logger) (notify.Notifier, error) { return victorops.New(c, tmpl, l) })
	}
	for i, c := range nc.PushoverConfigs {
		add("pushover", i, c, func(l log.Logger) (notify.Notifier, error) { return pushover.New(c, tmpl, l) })
	}
	if errs.Len() > 0 {
		return nil, &errs
	}
	return integrations, nil
}

func md5HashAsMetricValue(data []byte) float64 {
	sum := md5.Sum(data)
	// We only want 48 bits as a float64 only has a 53 bit mantissa.
	smallSum := sum[0:6]
	var bytes = make([]byte, 8)
	copy(bytes, smallSum)
	return float64(binary.LittleEndian.Uint64(bytes))
}
