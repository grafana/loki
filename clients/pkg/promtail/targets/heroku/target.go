package heroku

import (
	"flag"
	"fmt"
	lokiClient "github.com/grafana/loki/clients/pkg/promtail/client"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	herokuEncoding "github.com/heroku/x/logplex/encoding"
	"github.com/imdario/mergo"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/pkg/logproto"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type Target struct {
	logger         log.Logger
	handler        api.EntryHandler
	config         *scrapeconfig.HerokuDrainTargetConfig
	jobName        string
	server         *server.Server
	metrics        *Metrics
	relabelConfigs []*relabel.Config
}

// NewTarget creates a brand new Heroku Drain target, capable of receiving logs from a Heroku application through an HTTP drain.
func NewTarget(metrics *Metrics, logger log.Logger, handler api.EntryHandler, jobName string, config *scrapeconfig.HerokuDrainTargetConfig, relabel []*relabel.Config) (*Target, error) {
	wrappedLogger := log.With(logger, "component", "heroku_drain")

	ht := &Target{
		metrics:        metrics,
		logger:         wrappedLogger,
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
		return nil, errors.Wrap(err, "failed to parse configs and override defaults when configuring heroku drain target")
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
	level.Info(h.logger).Log("msg", "starting heroku drain target", "job", h.jobName)

	// To prevent metric collisions because all metrics are going to be registered in the global Prometheus registry.

	tentativeServerMetricNamespace := "promtail_heroku_drain_target_" + h.jobName
	if !model.IsValidMetricName(model.LabelValue(tentativeServerMetricNamespace)) {
		return fmt.Errorf("invalid prometheus-compatible job name: %s", h.jobName)
	}
	h.config.Server.MetricsNamespace = tentativeServerMetricNamespace

	// We don't want the /debug and /metrics endpoints running, since this is not the main promtail HTTP server.
	// We want this target to expose the least surface area possible, hence disabling WeaveWorks HTTP server metrics
	// and debugging functionality.
	h.config.Server.RegisterInstrumentation = false

	// Wrapping util logger with component-specific key vals, and the expected GoKit logging interface
	h.config.Server.Log = logging.GoKit(log.With(util_log.Logger, "component", "heroku_drain"))

	srv, err := server.New(h.config.Server)
	if err != nil {
		return err
	}

	h.server = srv
	h.server.HTTP.Path("/heroku/api/v1/drain").Methods("POST").Handler(http.HandlerFunc(h.drain))

	go func() {
		err := srv.Run()
		if err != nil {
			level.Error(h.logger).Log("msg", "heroku drain target shutdown with error", "err", err)
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
		lb.Set("__heroku_drain_host", message.Hostname)
		lb.Set("__heroku_drain_app", message.Application)
		lb.Set("__heroku_drain_proc", message.Process)
		lb.Set("__heroku_drain_log_id", message.ID)

		if h.config.UseIncomingTimestamp {
			ts = message.Timestamp
		}

		// If the incoming request carries the tenant id, inject it as the reserved label so it's used by the
		// remote write client.
		tenantIDHeaderValue := r.Header.Get("X-Scope-OrgID")
		if tenantIDHeaderValue != "" {
			lb.Set(lokiClient.ReservedLabelTenantID, tenantIDHeaderValue)
		}

		processed := relabel.Process(lb.Labels(), h.relabelConfigs...)

		// Start with the set of labels fixed in the configuration
		filtered := h.Labels().Clone()
		for _, lbl := range processed {
			if strings.HasPrefix(lbl.Name, "__") && lbl.Name != lokiClient.ReservedLabelTenantID {
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
		level.Warn(h.logger).Log("msg", "failed to read incoming heroku request", "err", err.Error())
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
	level.Info(h.logger).Log("msg", "stopping heroku drain target", "job", h.jobName)
	h.server.Shutdown()
	h.handler.Stop()
	return nil
}
