package gcplog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/serverutils"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type pushTarget struct {
	config         *scrapeconfig.GcplogTargetConfig
	entries        chan<- api.Entry
	handler        api.EntryHandler
	jobName        string
	logger         log.Logger
	metrics        *Metrics
	relabelConfigs []*relabel.Config
	server         *server.Server
}

// newPushTarget creates a brand new GCP Push target, capable of receiving message from a GCP PubSub push subscription.
func newPushTarget(metrics *Metrics, logger log.Logger, handler api.EntryHandler, jobName string, config *scrapeconfig.GcplogTargetConfig, relabel []*relabel.Config) (*pushTarget, error) {
	wrappedLogger := log.With(logger, "component", "gcp_push")

	ht := &pushTarget{
		config:         config,
		entries:        handler.Chan(),
		handler:        handler,
		jobName:        jobName,
		logger:         wrappedLogger,
		metrics:        metrics,
		relabelConfigs: relabel,
	}

	mergedServerConfigs, err := serverutils.MergeWithDefaults(config.Server)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configs and override defaults when configuring gcp push target: %w", err)
	}
	config.Server = mergedServerConfigs
	// Avoid logging entire received request on failures
	config.Server.ExcludeRequestInLog = true

	err = ht.run()
	if err != nil {
		return nil, err
	}

	return ht, nil
}

func (h *pushTarget) run() error {
	level.Info(h.logger).Log("msg", "starting gcp push target", "job", h.jobName)

	// To prevent metric collisions because all metrics are going to be registered in the global Prometheus registry.

	tentativeServerMetricNamespace := "promtail_gcp_push_target_" + h.jobName
	if !model.IsValidMetricName(model.LabelValue(tentativeServerMetricNamespace)) {
		return fmt.Errorf("invalid prometheus-compatible job name: %s", h.jobName)
	}
	h.config.Server.MetricsNamespace = tentativeServerMetricNamespace

	// We don't want the /debug and /metrics endpoints running, since this is not the main promtail HTTP server.
	// We want this target to expose the least surface area possible, hence disabling WeaveWorks HTTP server metrics
	// and debugging functionality.
	h.config.Server.RegisterInstrumentation = false

	// Wrapping util logger with component-specific key vals, and the expected GoKit logging interface
	h.config.Server.Log = log.With(util_log.Logger, "component", "gcp_push")

	srv, err := server.New(h.config.Server)
	if err != nil {
		return err
	}
	h.server = srv

	h.server.HTTP.Path("/gcp/api/v1/push").Methods("POST").Handler(http.HandlerFunc(h.push))

	go func() {
		err := srv.Run()
		if err != nil {
			level.Error(h.logger).Log("msg", "gcp push target shutdown with error", "err", err)
		}
	}()

	return nil
}

func (h *pushTarget) push(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	// Create no-op context.WithTimeout returns to simplify logic
	ctx := r.Context()
	cancel := context.CancelFunc(func() {})
	if h.config.PushTimeout != 0 {
		ctx, cancel = context.WithTimeout(r.Context(), h.config.PushTimeout)
	}
	defer cancel()

	pushMessage := PushMessage{}
	bs, err := io.ReadAll(r.Body)
	if err != nil {
		h.metrics.gcpPushErrors.WithLabelValues("read_error").Inc()
		level.Warn(h.logger).Log("msg", "failed to read incoming gcp push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = json.Unmarshal(bs, &pushMessage)
	if err != nil {
		h.metrics.gcpPushErrors.WithLabelValues("format").Inc()
		level.Warn(h.logger).Log("msg", "failed to unmarshall gcp push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err = pushMessage.Validate(); err != nil {
		h.metrics.gcpPushErrors.WithLabelValues("invalid_message").Inc()
		level.Warn(h.logger).Log("msg", "invalid gcp push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	entry, err := translate(pushMessage, h.config.Labels, h.config.UseIncomingTimestamp, h.config.UseFullLine, h.relabelConfigs, r.Header.Get("X-Scope-OrgID"))
	if err != nil {
		h.metrics.gcpPushErrors.WithLabelValues("translation").Inc()
		level.Warn(h.logger).Log("msg", "failed to translate gcp push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	level.Debug(h.logger).Log("msg", fmt.Sprintf("Received line: %s", entry.Line))

	if err := h.doSendEntry(ctx, entry); err != nil {
		// NOTE: timeout errors can be tracked with a metrics exporter from the spun weave-works server, and the 503 status code
		// promtail_gcp_push_target_{job name}_request_duration_seconds_count{status_code="503"}
		level.Warn(h.logger).Log("msg", "error sending log entry", "err", err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	h.metrics.gcpPushEntries.WithLabelValues().Inc()
	w.WriteHeader(http.StatusNoContent)
}

func (h *pushTarget) doSendEntry(ctx context.Context, entry api.Entry) error {
	select {
	// Timeout the api.Entry channel send operation, which is the only blocking operation in the handler
	case <-ctx.Done():
		return fmt.Errorf("timeout exceeded: %w", ctx.Err())
	case h.entries <- entry:
		return nil
	}
}

func (h *pushTarget) Type() target.TargetType {
	return target.GcplogTargetType
}

func (h *pushTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

func (h *pushTarget) Labels() model.LabelSet {
	return h.config.Labels
}

func (h *pushTarget) Ready() bool {
	return true
}

func (h *pushTarget) Details() interface{} {
	return map[string]string{}
}

func (h *pushTarget) Stop() error {
	level.Info(h.logger).Log("msg", "stopping gcp push target", "job", h.jobName)
	h.server.Stop()
	h.server.Shutdown()
	h.handler.Stop()
	return nil
}
