package http

import (
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/targets/serverutils"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/server"
)

type TargetServer struct {
	logger         log.Logger
	handler        api.EntryHandler
	config         *Config
	componentName  string
	jobName        string
	server         *server.Server
	relabelConfigs []*relabel.Config
}

func NewTargetServer(
	mountRoute func(router *mux.Router),
	logger log.Logger,
	handler api.EntryHandler,
	jobName, componentName string,
	config *Config,
	relabel []*relabel.Config,
) (*TargetServer, error) {
	wrappedLogger := log.With(logger, "component", componentName)

	t := &TargetServer{
		logger:         wrappedLogger,
		handler:        handler,
		jobName:        jobName,
		config:         config,
		relabelConfigs: relabel,
	}

	mergedServerConfigs, err := serverutils.MergeWithDefaults(config.Server)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configs and override defaults when configuring target: %w", err)
	}
	// Set the config to the new combined config.
	config.Server = mergedServerConfigs

	return t, nil
}

func (ts *TargetServer) MountAndRun(mountRoute func(router *mux.Router)) error {
	level.Info(ts.logger).Log("msg", fmt.Sprintf("starting %s target", ts.componentName), "job", ts.jobName)

	// To prevent metric collisions because all metrics are going to be registered in the global Prometheus registry.

	tentativeServerMetricNamespace := fmt.Sprintf("promtail_%s_target_", ts.componentName) + ts.jobName
	if !model.IsValidMetricName(model.LabelValue(tentativeServerMetricNamespace)) {
		return fmt.Errorf("invalid prometheus-compatible job name: %s", ts.jobName)
	}
	ts.config.Server.MetricsNamespace = tentativeServerMetricNamespace

	// We don't want the /debug and /metrics endpoints running, since this is not the main promtail HTTP server.
	// We want this target to expose the least surface area possible, hence disabling WeaveWorks HTTP server metrics
	// and debugging functionality.
	ts.config.Server.RegisterInstrumentation = false

	// Wrapping util logger with component-specific key vals, and the expected GoKit logging interface
	ts.config.Server.Log = logging.GoKit(log.With(util_log.Logger, "component", ts.componentName))

	srv, err := server.New(ts.config.Server)
	if err != nil {
		return err
	}

	ts.server = srv
	mountRoute(ts.server.HTTP)

	go func() {
		err := srv.Run()
		if err != nil {
			level.Error(ts.logger).Log("msg", fmt.Sprintf("%s target shutdown with error", ts.componentName), "err", err)
		}
	}()

	return nil
}

func (ts *TargetServer) Stop() {
	ts.server.Stop()
	ts.server.Shutdown()
}
