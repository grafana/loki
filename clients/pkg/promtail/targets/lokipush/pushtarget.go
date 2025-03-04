package lokipush

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/serverutils"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type PushTarget struct {
	logger        log.Logger
	handler       api.EntryHandler
	config        *scrapeconfig.PushTargetConfig
	relabelConfig []*relabel.Config
	jobName       string
	server        *server.Server
}

func NewPushTarget(logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	jobName string,
	config *scrapeconfig.PushTargetConfig,
) (*PushTarget, error) {

	pt := &PushTarget{
		logger:        logger,
		handler:       handler,
		relabelConfig: relabel,
		jobName:       jobName,
		config:        config,
	}

	mergedServerConfigs, err := serverutils.MergeWithDefaults(config.Server)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configs and override defaults when configuring loki push target: %w", err)
	}
	// Set the config to the new combined config.
	config.Server = mergedServerConfigs

	err = pt.run()
	if err != nil {
		return nil, err
	}

	return pt, nil
}

func (t *PushTarget) run() error {
	level.Info(t.logger).Log("msg", "starting push server", "job", t.jobName)
	// To prevent metric collisions because all metrics are going to be registered in the global Prometheus registry.
	t.config.Server.MetricsNamespace = "promtail_" + t.jobName

	// We don't want the /debug and /metrics endpoints running
	t.config.Server.RegisterInstrumentation = false

	// The logger registers a metric which will cause a duplicate registry panic unless we provide an empty registry
	// The metric created is for counting log lines and isn't likely to be missed.
	serverCfg := &t.config.Server
	serverCfg.Log = util_log.InitLogger(serverCfg, prometheus.NewRegistry(), false)

	// Set new registry for upcoming metric server
	// If not, it'll likely panic when the tool gets reloaded.
	if t.config.Server.Registerer == nil {
		t.config.Server.Registerer = prometheus.NewRegistry()
	}

	srv, err := server.New(t.config.Server)
	if err != nil {
		return err
	}

	t.server = srv
	t.server.HTTP.Path("/loki/api/v1/push").Methods("POST").Handler(http.HandlerFunc(t.handleLoki))
	t.server.HTTP.Path("/promtail/api/v1/raw").Methods("POST").Handler(http.HandlerFunc(t.handlePlaintext))
	t.server.HTTP.Path("/ready").Methods("GET").Handler(http.HandlerFunc(t.ready))

	go func() {
		err := srv.Run()
		if err != nil {
			level.Error(t.logger).Log("msg", "Loki push server shutdown with error", "err", err)
		}
	}()

	return nil
}

func (t *PushTarget) handleLoki(w http.ResponseWriter, r *http.Request) {
	logger := util_log.WithContext(r.Context(), util_log.Logger)
	userID, _ := tenant.TenantID(r.Context())
	req, err := push.ParseRequest(logger, userID, r, push.EmptyLimits{}, push.ParseLokiRequest, nil, nil, false)
	if err != nil {
		level.Warn(t.logger).Log("msg", "failed to parse incoming push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var lastErr error
	for _, stream := range req.Streams {
		ls, err := promql_parser.ParseMetric(stream.Labels)
		if err != nil {
			lastErr = err
			continue
		}
		sort.Sort(ls)

		lb := labels.NewBuilder(ls)

		// Add configured labels
		for k, v := range t.config.Labels {
			lb.Set(string(k), string(v))
		}

		// Apply relabeling
		processed, keep := relabel.Process(lb.Labels(), t.relabelConfig...)
		if !keep || len(processed) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Convert to model.LabelSet
		filtered := model.LabelSet{}
		for i := range processed {
			if strings.HasPrefix(processed[i].Name, "__") {
				continue
			}
			filtered[model.LabelName(processed[i].Name)] = model.LabelValue(processed[i].Value)
		}

		for _, entry := range stream.Entries {
			e := api.Entry{
				Labels: filtered.Clone(),
				Entry: logproto.Entry{
					Line:               entry.Line,
					StructuredMetadata: entry.StructuredMetadata,
				},
			}
			if t.config.KeepTimestamp {
				e.Timestamp = entry.Timestamp
			} else {
				e.Timestamp = time.Now()
			}
			t.handler.Chan() <- e
		}
	}

	if lastErr != nil {
		level.Warn(t.logger).Log("msg", "at least one entry in the push request failed to process", "err", lastErr.Error())
		http.Error(w, lastErr.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handlePlaintext handles newline delimited input such as plaintext or NDJSON.
func (t *PushTarget) handlePlaintext(w http.ResponseWriter, r *http.Request) {
	entries := t.handler.Chan()
	defer r.Body.Close()
	body := bufio.NewReader(r.Body)
	for {
		line, err := body.ReadString('\n')
		if err != nil && err != io.EOF {
			level.Warn(t.logger).Log("msg", "failed to read incoming push request", "err", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if err == io.EOF {
				break
			}
			continue
		}
		entries <- api.Entry{
			Labels: t.Labels().Clone(),
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}
		if err == io.EOF {
			break
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// Type returns PushTargetType.
func (t *PushTarget) Type() target.TargetType {
	return target.PushTargetType
}

// Ready indicates whether or not the PushTarget target is ready to be read from.
func (t *PushTarget) Ready() bool {
	return true
}

// DiscoveredLabels returns the set of labels discovered by the PushTarget, which
// is always nil. Implements Target.
func (t *PushTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the PushTarget.
func (t *PushTarget) Labels() model.LabelSet {
	return t.config.Labels
}

// Details returns target-specific details.
func (t *PushTarget) Details() interface{} {
	return map[string]string{}
}

// Stop shuts down the PushTarget.
func (t *PushTarget) Stop() error {
	level.Info(t.logger).Log("msg", "stopping push server", "job", t.jobName)
	t.server.Shutdown()
	t.handler.Stop()
	return nil
}

// ready function serves the ready endpoint
func (t *PushTarget) ready(w http.ResponseWriter, _ *http.Request) {
	resp := "ready"
	if _, err := w.Write([]byte(resp)); err != nil {
		level.Error(t.logger).Log("msg", "failed to respond to ready endoint", "err", err)
	}
}
