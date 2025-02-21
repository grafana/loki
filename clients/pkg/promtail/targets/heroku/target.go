package heroku

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
	herokuEncoding "github.com/heroku/x/logplex/encoding"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	lokiClient "github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/serverutils"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/logproto"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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

	mergedServerConfigs, err := serverutils.MergeWithDefaults(config.Server)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configs and override defaults when configuring heroku drain target: %w", err)
	}
	// Set the config to the new combined config.
	config.Server = mergedServerConfigs

	err = ht.run()
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
	h.config.Server.Log = log.With(util_log.Logger, "component", "heroku_drain")

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

		// Create __heroku_drain_param_<name> labels from query parameters
		params := r.URL.Query()
		for k, v := range params {
			lb.Set(fmt.Sprintf("__heroku_drain_param_%s", k), strings.Join(v, ","))
		}

		tenantIDHeaderValue := r.Header.Get("X-Scope-OrgID")
		if tenantIDHeaderValue != "" {
			// If present, first inject the tenant ID in, so it can be relabeled if necessary
			lb.Set(lokiClient.ReservedLabelTenantID, tenantIDHeaderValue)
		}

		processed, _ := relabel.Process(lb.Labels(), h.relabelConfigs...)

		// Start with the set of labels fixed in the configuration
		filtered := h.Labels().Clone()
		for _, lbl := range processed {
			if strings.HasPrefix(lbl.Name, "__") {
				continue
			}
			filtered[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
		}

		// Then, inject it as the reserved label, so it's used by the remote write client
		if tenantIDHeaderValue != "" {
			filtered[lokiClient.ReservedLabelTenantID] = model.LabelValue(tenantIDHeaderValue)
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
