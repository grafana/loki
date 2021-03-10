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
	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/api"
	"github.com/prometheus/alertmanager/cluster"
	"github.com/prometheus/alertmanager/cluster/clusterpb"
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
	"github.com/prometheus/alertmanager/timeinterval"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/alertmanager/ui"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/route"

	"github.com/cortexproject/cortex/pkg/util/services"
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

	ShardingEnabled    bool
	ReplicationFactor  int
	ReplicateStateFunc func(context.Context, string, *clusterpb.Part) error
	// The alertmanager replication protocol relies on a position related to other replicas.
	// This position is then used to identify who should notify about the alert first.
	GetPositionFunc func(userID string) int
}

// An Alertmanager manages the alerts for one user.
type Alertmanager struct {
	cfg             *Config
	api             *api.API
	logger          log.Logger
	state           State
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

// State helps with replication and synchronization of notifications and silences across several alertmanager replicas.
type State interface {
	AddState(string, cluster.State, prometheus.Registerer) cluster.ClusterChannel
	Position() int
	WaitReady()
}

// New creates a new Alertmanager.
func New(cfg *Config, reg *prometheus.Registry) (*Alertmanager, error) {
	am := &Alertmanager{
		cfg:    cfg,
		logger: log.With(cfg.Logger, "user", cfg.UserID),
		stop:   make(chan struct{}),
		configHashMetric: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "alertmanager_config_hash",
			Help: "Hash of the currently loaded alertmanager configuration.",
		}),
	}

	am.registry = reg

	// We currently have 3 operational modes:
	// 1) Alertmanager clustering with upstream Gossip
	// 2) Alertmanager sharding and ring-based replication
	// 3) Alertmanager no replication
	// These are covered in order.
	if cfg.Peer != nil {
		level.Debug(am.logger).Log("msg", "starting tenant alertmanager with gossip-based replication")
		am.state = cfg.Peer
	} else if cfg.ShardingEnabled {
		level.Debug(am.logger).Log("msg", "starting tenant alertmanager with ring-based replication")
		state := newReplicatedStates(cfg.UserID, cfg.ReplicationFactor, cfg.ReplicateStateFunc, cfg.GetPositionFunc, am.logger, am.registry)

		if err := state.Service.StartAsync(context.Background()); err != nil {
			return nil, errors.Wrap(err, "failed to start ring-based replication service")
		}

		am.state = state
	} else {
		level.Debug(am.logger).Log("msg", "starting tenant alertmanager without replication")
		am.state = &NilPeer{}
	}

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

	c := am.state.AddState("nfl:"+cfg.UserID, am.nflog, am.registry)
	am.nflog.SetBroadcast(c.Broadcast)

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

	c = am.state.AddState("sil:"+cfg.UserID, am.silences, am.registry)
	am.silences.SetBroadcast(c.Broadcast)

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
		// Cortex should not expose cluster information back to its tenants.
		Peer:     &NilPeer{},
		Registry: am.registry,
		Logger:   log.With(am.logger, "component", "api"),
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

	//TODO: From this point onward, the alertmanager _might_ receive requests - we need to make sure we've settled and are ready.
	return am, nil
}

// clusterWait returns a function that inspects the current peer state and returns
// a duration of one base timeout for each peer with a higher ID than ourselves.
func clusterWait(position func() int, timeout time.Duration) func() time.Duration {
	return func() time.Duration {
		return time.Duration(position()) * timeout
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

	waitFunc := clusterWait(am.state.Position, am.cfg.PeerTimeout)

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

	muteTimes := make(map[string][]timeinterval.TimeInterval, len(conf.MuteTimeIntervals))
	for _, ti := range conf.MuteTimeIntervals {
		muteTimes[ti.Name] = ti.TimeIntervals
	}

	pipeline := am.pipelineBuilder.New(
		integrationsMap,
		waitFunc,
		am.inhibitor,
		silence.NewSilencer(am.silences, am.marker, am.logger),
		muteTimes,
		am.nflog,
		am.state,
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

	am.configHashMetric.Set(md5HashAsMetricValue([]byte(rawCfg)))
	return nil
}

// Stop stops the Alertmanager.
func (am *Alertmanager) Stop() {
	if am.inhibitor != nil {
		am.inhibitor.Stop()
	}

	if am.dispatcher != nil {
		am.dispatcher.Stop()
	}

	if service, ok := am.state.(services.Service); ok {
		service.StopAsync()
	}

	am.alerts.Close()
	close(am.stop)
}

func (am *Alertmanager) StopAndWait() {
	am.Stop()

	if service, ok := am.state.(services.Service); ok {
		if err := service.AwaitTerminated(context.Background()); err != nil {
			level.Warn(am.logger).Log("msg", "error while stopping ring-based replication service", "err", err)
		}
	}

	am.wg.Wait()
}

func (am *Alertmanager) mergePartialExternalState(part *clusterpb.Part) error {
	return am.state.(*state).MergePartialState(part)
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

// NilPeer and NilChannel implements the Alertmanager clustering interface used by the API to expose cluster information.
// In a multi-tenant environment, we choose not to expose these to tenants and thus are not implemented.
type NilPeer struct{}

func (p *NilPeer) Name() string                   { return "" }
func (p *NilPeer) Status() string                 { return "ready" }
func (p *NilPeer) Peers() []cluster.ClusterMember { return nil }
func (p *NilPeer) Position() int                  { return 0 }
func (p *NilPeer) WaitReady()                     {}
func (p *NilPeer) AddState(string, cluster.State, prometheus.Registerer) cluster.ClusterChannel {
	return &NilChannel{}
}

type NilChannel struct{}

func (c *NilChannel) Broadcast([]byte) {}
