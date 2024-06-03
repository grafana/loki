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

	b := &Builder{
		ID:      uuid.NewString(),
		cfg:     cfg,
		metrics: NewMetrics(r),
		logger:  logger,
	}

	b.Service = services.NewBasicService(b.starting, b.running, b.stopping)
	return b, nil
}

func (b *Builder) starting(_ context.Context) error {
	b.metrics.running.Set(1)
	return nil
}

func (b *Builder) stopping(_ error) error {
	if b.client != nil {
		req := &protos.NotifyBuilderShutdownRequest{
			BuilderID: b.ID,
		}
		if _, err := b.client.NotifyBuilderShutdown(context.Background(), req); err != nil {
			level.Error(b.logger).Log("msg", "failed to notify planner about builder shutdown", "err", err)
		}
	}

	b.metrics.running.Set(0)
	return nil
}

func (b *Builder) running(ctx context.Context) error {
	opts, err := b.cfg.GrpcConfig.DialOption(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create grpc dial options: %w", err)
	}

	// TODO: Wrap hereafter in retry logic
	conn, err := grpc.DialContext(ctx, b.cfg.PlannerAddress, opts...)
	if err != nil {
		return fmt.Errorf("failed to dial bloom planner: %w", err)
	}

	b.client = protos.NewPlannerForBuilderClient(conn)

	c, err := b.client.BuilderLoop(ctx)
	if err != nil {
		return fmt.Errorf("failed to start builder loop: %w", err)
	}

	// Start processing tasks from planner
	if err := b.builderLoop(c); err != nil {
		return fmt.Errorf("builder loop failed: %w", err)
	}

	return nil
}

func (b *Builder) builderLoop(c protos.PlannerForBuilder_BuilderLoopClient) error {
	// Send ready message to planner
	if err := c.Send(&protos.BuilderToPlanner{BuilderID: b.ID}); err != nil {
		return fmt.Errorf("failed to send ready message to planner: %w", err)
	}

	for b.State() == services.Running {
		// When the planner connection closes or the builder stops, the context
		// will be canceled and the loop will exit.
		protoTask, err := c.Recv()
		if err != nil {
			if errors.Is(c.Context().Err(), context.Canceled) {
				level.Debug(b.logger).Log("msg", "builder loop context canceled")
				return nil
			}

			return fmt.Errorf("failed to receive task from planner: %w", err)
		}

		b.metrics.taskStarted.Inc()
		start := time.Now()
		status := statusSuccess

		err = b.processTask(c.Context(), protoTask.Task)
		if err != nil {
			status = statusFailure
			level.Error(b.logger).Log("msg", "failed to process task", "err", err)
		}

		b.metrics.taskCompleted.WithLabelValues(status).Inc()
		b.metrics.taskDuration.WithLabelValues(status).Observe(time.Since(start).Seconds())

		// Acknowledge task completion to planner
		if err = b.notifyTaskCompletedToPlanner(c, err); err != nil {
			return fmt.Errorf("failed to notify task completion to planner: %w", err)
		}
	}

	level.Debug(b.logger).Log("msg", "builder loop stopped")
	return nil
}

func (b *Builder) notifyTaskCompletedToPlanner(c protos.PlannerForBuilder_BuilderLoopClient, err error) error {
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}

	// TODO: Implement retry
	if err := c.Send(&protos.BuilderToPlanner{
		BuilderID: b.ID,
		Error:     errMsg,
	}); err != nil {
		return fmt.Errorf("failed to acknowledge task completion to planner: %w", err)
	}
	return nil
}

func (b *Builder) processTask(_ context.Context, protoTask *protos.ProtoTask) error {
	task, err := protos.FromProtoTask(protoTask)
	if err != nil {
		return fmt.Errorf("failed to convert proto task to task: %w", err)
	}

	level.Debug(b.logger).Log("msg", "received task", "task", task.ID)

	// TODO: Implement task processing

	return nil
}
