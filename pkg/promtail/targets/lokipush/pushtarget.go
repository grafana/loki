package lokipush

import (
	"flag"
	"net/http"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/imdario/mergo"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
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
	config *scrapeconfig.PushTargetConfig) (*PushTarget, error) {

	pt := &PushTarget{
		logger:        logger,
		handler:       handler,
		relabelConfig: relabel,
		jobName:       jobName,
		config:        config,
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

	err := pt.run()
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

	util.InitLogger(&t.config.Server)

	srv, err := server.New(t.config.Server)
	if err != nil {
		return err
	}

	t.server = srv
	t.server.HTTP.Handle("/loki/api/v1/push", http.HandlerFunc(t.handle))

	go func() {
		err := srv.Run()
		if err != nil {
			level.Error(t.logger).Log("msg", "Loki push server shutdown with error", "err", err)
		}
	}()

	return nil
}

func (t *PushTarget) handle(w http.ResponseWriter, r *http.Request) {
	req, err := distributor.ParseRequest(r)
	if err != nil {
		level.Warn(t.logger).Log("msg", "failed to parse incoming push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var lastErr error
	for _, stream := range req.Streams {
		matchers, err := logql.ParseMatchers(stream.Labels)
		if err != nil {
			lastErr = err
			continue
		}

		lb := labels.NewBuilder(make(labels.Labels, 0, len(matchers)+len(t.config.Labels)))

		// Add stream labels
		for i := range matchers {
			lb.Set(matchers[i].Name, matchers[i].Value)
		}

		// Add configured labels
		for k, v := range t.config.Labels {
			lb.Set(string(k), string(v))
		}

		// Apply relabeling
		processed := relabel.Process(lb.Labels(), t.relabelConfig...)
		if processed == nil || len(processed) == 0 {
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
			var err error
			if t.config.KeepTimestamp {
				err = t.handler.Handle(filtered, entry.Timestamp, entry.Line)
			} else {
				err = t.handler.Handle(filtered, time.Now(), entry.Line)
			}

			if err != nil {
				lastErr = err
				continue
			}
		}
	}

	if lastErr != nil {
		level.Warn(t.logger).Log("msg", "at least one entry in the push request failed to process", "err", lastErr.Error())
		http.Error(w, lastErr.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	return
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
	return nil
}
