package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

type Builder struct {
	services.Service

	ID string

	cfg     Config
	metrics *Metrics
	logger  log.Logger

	client protos.PlannerForBuilderClient
}

func New(
	cfg Config,
	logger log.Logger,
	r prometheus.Registerer,
) (*Builder, error) {
	utillog.WarnExperimentalUse("Bloom Builder", logger)

	w := &Builder{
		ID:      uuid.NewString(),
		cfg:     cfg,
		metrics: NewMetrics(r),
		logger:  logger,
	}

	w.Service = services.NewBasicService(w.starting, w.running, w.stopping)
	return w, nil
}

func (w *Builder) starting(_ context.Context) error {
	w.metrics.running.Set(1)
	return nil
}

func (w *Builder) stopping(_ error) error {
	if w.client != nil {
		req := &protos.NotifyBuilderShutdownRequest{
			BuilderID: w.ID,
		}
		if _, err := w.client.NotifyBuilderShutdown(context.Background(), req); err != nil {
			level.Error(w.logger).Log("msg", "failed to notify planner about builder shutdown", "err", err)
		}
	}

	w.metrics.running.Set(0)
	return nil
}

func (w *Builder) running(ctx context.Context) error {
	opts, err := w.cfg.GrpcConfig.DialOption(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create grpc dial options: %w", err)
	}

	// TODO: Wrap hereafter in retry logic
	conn, err := grpc.DialContext(ctx, w.cfg.PlannerAddress, opts...)
	if err != nil {
		return fmt.Errorf("failed to dial bloom planner: %w", err)
	}

	w.client = protos.NewPlannerForBuilderClient(conn)

	c, err := w.client.BuilderLoop(ctx)
	if err != nil {
		return fmt.Errorf("failed to start builder loop: %w", err)
	}

	// Start processing tasks from planner
	if err := w.builderLoop(c); err != nil {
		return fmt.Errorf("builder loop failed: %w", err)
	}

	return nil
}

func (w *Builder) builderLoop(c protos.PlannerForBuilder_BuilderLoopClient) error {
	// Send ready message to planner
	if err := c.Send(&protos.BuilderToPlanner{BuilderID: w.ID}); err != nil {
		return fmt.Errorf("failed to send ready message to planner: %w", err)
	}

	for w.State() == services.Running {
		// When the planner connection closes or the builder stops, the context
		// will be canceled and the loop will exit.
		protoTask, err := c.Recv()
		if err != nil {
			if errors.Is(c.Context().Err(), context.Canceled) {
				level.Debug(w.logger).Log("msg", "builder loop context canceled")
				return nil
			}

			return fmt.Errorf("failed to receive task from planner: %w", err)
		}

		w.metrics.taskStarted.Inc()
		start := time.Now()
		status := statusSuccess

		err = w.processTask(c.Context(), protoTask.Task)
		if err != nil {
			status = statusFailure
			level.Error(w.logger).Log("msg", "failed to process task", "err", err)
		}

		w.metrics.taskCompleted.WithLabelValues(status).Inc()
		w.metrics.taskTime.WithLabelValues(status).Observe(time.Since(start).Seconds())

		// Acknowledge task completion to planner
		if err = w.notifyTaskCompletedToPlanner(c, err); err != nil {
			return fmt.Errorf("failed to notify task completion to planner: %w", err)
		}
	}

	level.Debug(w.logger).Log("msg", "builder loop stopped")
	return nil
}

func (w *Builder) notifyTaskCompletedToPlanner(c protos.PlannerForBuilder_BuilderLoopClient, err error) error {
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}

	// TODO: Implement retry
	if err := c.Send(&protos.BuilderToPlanner{
		BuilderID: w.ID,
		Error:     errMsg,
	}); err != nil {
		return fmt.Errorf("failed to acknowledge task completion to planner: %w", err)
	}
	return nil
}

func (w *Builder) processTask(_ context.Context, protoTask *protos.ProtoTask) error {
	task, err := protos.FromProtoTask(protoTask)
	if err != nil {
		return fmt.Errorf("failed to convert proto task to task: %w", err)
	}

	level.Debug(w.logger).Log("msg", "received task", "task", task.ID)

	// TODO: Implement task processing

	return nil
}
