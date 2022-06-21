package heroku

import (
	"flag"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/grafana/loki/pkg/logproto"
	util_log "github.com/grafana/loki/pkg/util/log"
	herokuEncoding "github.com/heroku/x/logplex/encoding"
	"github.com/imdario/mergo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/weaveworks/common/server"
	"net/http"
	"strings"
	"time"
)

type Target struct {
	logger         log.Logger
	handler        api.EntryHandler
	config         *scrapeconfig.HerokuTargetConfig
	jobName        string
	server         *server.Server
	metrics        *Metrics
	relabelConfigs []*relabel.Config
}

func NewTarget(metrics *Metrics, logger log.Logger, handler api.EntryHandler, jobName string, config *scrapeconfig.HerokuTargetConfig, relabel []*relabel.Config) (*Target, error) {

	ht := &Target{
		metrics:        metrics,
		logger:         logger,
		handler:        handler,
		jobName:        jobName,
		config:         config,
		relabelConfigs: relabel,
	}

	// Bit of a chicken and egg problem trying to register the defaults and apply overrides from the loaded config.
	// First create an empty config and set defaults.
	defaults := server.Config{}
	defaults.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	// Then apply any config values loaded as overrides to the defaults.
	if err := mergo.Merge(&defaults, config.Server, mergo.WithOverride); err != nil {
		level.Error(logger).Log("msg", "failed to parse configs and override defaults when configuring push server", "err", err)
	}
	// The merge won't overwrite with a zero value but in the case of ports 0 value
	// indicates the desire for a random port so reset these to zero if the incoming config val is 0
	if config.Server.HTTPListenPort == 0 {
		defaults.HTTPListenPort = 0
	}
	if config.Server.GRPCListenPort == 0 {
		defaults.GRPCListenPort = 0
	}
	// Set the config to the new combined config.
	config.Server = defaults

	err := ht.run()
	if err != nil {
		return nil, err
	}

	return ht, nil
}

func (h *Target) run() error {
	level.Info(h.logger).Log("msg", "starting heroku target", "job", h.jobName)
	// To prevent metric collisions because all metrics are going to be registered in the global Prometheus registry.
	h.config.Server.MetricsNamespace = "promtail_heroku_target_" + h.jobName

	// We don't want the /debug and /metrics endpoints running
	h.config.Server.RegisterInstrumentation = false

	// The logger registers a metric which will cause a duplicate registry panic unless we provide an empty registry
	// The metric created is for counting log lines and isn't likely to be missed.
	util_log.InitLogger(&h.config.Server, prometheus.NewRegistry())

	srv, err := server.New(h.config.Server)
	if err != nil {
		return err
	}

	h.server = srv
	h.server.HTTP.Path("/heroku/api/v1/drain").Methods("POST").Handler(http.HandlerFunc(h.drain))

	go func() {
		err := srv.Run()
		if err != nil {
			level.Error(h.logger).Log("msg", "Loki push server shutdown with error", "err", err)
		}
	}()

	return nil
}

func (h *Target) drain(w http.ResponseWriter, r *http.Request) {
	entries := h.handler.Chan()
	defer r.Body.Close()
	herokuScanner := herokuEncoding.NewDrainScanner(r.Body)
	for herokuScanner.Scan() {
		ts := time.Now()
		message := herokuScanner.Message()
		lb := labels.NewBuilder(nil)
		lb.Set("__logplex_host", message.Hostname)
		lb.Set("__logplex_app", message.Application)
		lb.Set("__logplex_proc", message.Process)
		lb.Set("__logplex_log_id", message.ID)

		if h.config.UseIncomingTimestamp {
			ts = message.Timestamp
		}

		processed := relabel.Process(lb.Labels(), h.relabelConfigs...)

		// Start with the set of labels fixed in the configuration
		filtered := h.Labels().Clone()
		for _, lbl := range processed {
			if strings.HasPrefix(lbl.Name, "__") {
				continue
			}
			filtered[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
		}

		entries <- api.Entry{
			Labels: filtered,
			Entry: logproto.Entry{
				Timestamp: ts,
				Line:      message.Message,
			},
		}
		h.metrics.herokuEntries.WithLabelValues().Inc()
	}
	err := herokuScanner.Err()
	if err != nil {
		h.metrics.herokuErrors.WithLabelValues().Inc()
		level.Warn(h.logger).Log("msg", "failed to read incoming push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Target) Type() target.TargetType {
	return target.HerokuDrainTargetType
}

func (h *Target) DiscoveredLabels() model.LabelSet {
	return nil
}

func (h *Target) Labels() model.LabelSet {
	return h.config.Labels
}

func (h *Target) Ready() bool {
	return true
}

func (h *Target) Details() interface{} {
	return map[string]string{}
}

func (h *Target) Stop() error {
	level.Info(h.logger).Log("msg", "stopping heroku target", "job", h.jobName)
	h.server.Shutdown()
	h.handler.Stop()
	return nil
}
