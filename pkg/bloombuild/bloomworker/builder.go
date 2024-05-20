package bloomworker

import (
	"context"
	"github.com/go-kit/log"

	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

type Worker struct {
	services.Service

	cfg     Config
	metrics *Metrics
	logger  log.Logger
}

func New(
	cfg Config,
	logger log.Logger,
	r prometheus.Registerer,
) (*Worker, error) {
	utillog.WarnExperimentalUse("Bloom Builder", logger)

	w := &Worker{
		cfg:     cfg,
		metrics: NewMetrics(r),
		logger:  logger,
	}

	w.Service = services.NewBasicService(w.starting, w.running, w.stopping)
	return w, nil
}

func (w *Worker) starting(_ context.Context) (err error) {
	w.metrics.running.Set(1)
	return err
}

func (w *Worker) stopping(_ error) error {
	w.metrics.running.Set(0)
	return nil
}

func (w *Worker) running(_ context.Context) error {
	return nil
}
