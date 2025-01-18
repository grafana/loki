package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

type testEnv struct {
	queue     *JobQueue
	scheduler *BlockScheduler
	transport *types.MemoryTransport
	builder   *Worker
}

type mockOffsetManager struct {
	topic         string
	consumerGroup string
}

func (m *mockOffsetManager) Topic() string         { return m.topic }
func (m *mockOffsetManager) ConsumerGroup() string { return m.consumerGroup }
func (m *mockOffsetManager) GroupLag(_ context.Context, _ int64) (map[int32]partition.Lag, error) {
	return nil, nil
}
func (m *mockOffsetManager) FetchLastCommittedOffset(_ context.Context, _ int32) (int64, error) {
	return 0, nil
}
func (m *mockOffsetManager) FetchPartitionOffset(_ context.Context, _ int32, _ partition.SpecialOffset) (int64, error) {
	return 0, nil
}
func (m *mockOffsetManager) Commit(_ context.Context, _ int32, _ int64) error {
	return nil
}

func newTestEnv(builderID string) (*testEnv, error) {
	mockOffsetMgr := &mockOffsetManager{
		topic:         "test-topic",
		consumerGroup: "test-group",
	}
	scheduler, err := NewScheduler(Config{
		Strategy:       RecordCountStrategy,
		JobQueueConfig: JobQueueConfig{},
	}, mockOffsetMgr, log.NewNopLogger(), prometheus.NewRegistry())
	if err != nil {
		return nil, err
	}

	transport := types.NewMemoryTransport(scheduler)
	builder := NewWorker(builderID, transport)

	return &testEnv{
		queue:     scheduler.queue,
		scheduler: scheduler,
		transport: transport,
		builder:   builder,
	}, nil
}

func TestScheduleAndProcessJob(t *testing.T) {
	env, err := newTestEnv("test-builder-1")
	if err != nil {
		t.Fatalf("failed to create test environment: %v", err)
	}

	ctx := context.Background()

	// Create and enqueue a test job
	job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})
	err = env.scheduler.handlePlannedJob(NewJobWithMetadata(job, 100))
	if err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	// Builder gets job
	receivedJob, ok, err := env.builder.GetJob(ctx)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if !ok {
		t.Fatal("expected to receive job")
	}
	if receivedJob.ID() != job.ID() {
		t.Errorf("got job ID %s, want %s", receivedJob.ID(), job.ID())
	}

	// Builder completes job
	err = env.builder.CompleteJob(ctx, receivedJob, true)
	if err != nil {
		t.Fatalf("failed to complete job: %v", err)
	}

	// Try to get another job (should be none available)
	_, ok, err = env.builder.GetJob(ctx)
	if err != nil {
		t.Fatalf("failed to get second job: %v", err)
	}
	if ok {
		t.Error("got unexpected second job")
	}
}

func TestContextCancellation(t *testing.T) {
	env, err := newTestEnv("test-builder-1")
	if err != nil {
		t.Fatalf("failed to create test environment: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Try to get job after context timeout
	time.Sleep(20 * time.Millisecond)
	_, _, err = env.builder.GetJob(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestMultipleBuilders(t *testing.T) {
	// Create first environment
	env1, err := newTestEnv("test-builder-1")
	if err != nil {
		t.Fatalf("failed to create test environment: %v", err)
	}
	// Create second builder using same scheduler
	builder2 := NewWorker("test-builder-2", env1.transport)

	ctx := context.Background()

	// Create test jobs
	job1 := types.NewJob(1, types.Offsets{Min: 100, Max: 200})
	job2 := types.NewJob(2, types.Offsets{Min: 300, Max: 400})

	// Enqueue jobs
	err = env1.scheduler.handlePlannedJob(NewJobWithMetadata(job1, 100))
	if err != nil {
		t.Fatalf("failed to enqueue job1: %v", err)
	}
	err = env1.scheduler.handlePlannedJob(NewJobWithMetadata(job2, 100))
	if err != nil {
		t.Fatalf("failed to enqueue job2: %v", err)
	}

	// Builders get jobs
	receivedJob1, ok, err := env1.builder.GetJob(ctx)
	if err != nil {
		t.Fatalf("builder1 failed to get job: %v", err)
	}
	if !ok {
		t.Fatal("builder1 expected to receive job")
	}

	receivedJob2, ok, err := builder2.GetJob(ctx)
	if err != nil {
		t.Fatalf("builder2 failed to get job: %v", err)
	}
	if !ok {
		t.Fatal("builder2 expected to receive job")
	}

	// Verify different jobs were assigned
	if receivedJob1.ID() == receivedJob2.ID() {
		t.Error("builders received same job")
	}

	// Complete jobs
	err = env1.builder.CompleteJob(ctx, receivedJob1, true)
	if err != nil {
		t.Fatalf("builder1 failed to complete job: %v", err)
	}

	err = builder2.CompleteJob(ctx, receivedJob2, true)
	if err != nil {
		t.Fatalf("builder2 failed to complete job: %v", err)
	}

	// Try to get more jobs (should be none available)
	_, ok, err = env1.builder.GetJob(ctx)
	if err != nil {
		t.Fatalf("builder1 failed to get second job: %v", err)
	}
	if ok {
		t.Error("builder1 got unexpected second job")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "valid config with record count strategy",
			cfg: Config{
				Interval:          time.Minute,
				LookbackPeriod:    -1,
				Strategy:          RecordCountStrategy,
				TargetRecordCount: 1000,
			},
		},
		{
			name: "zero interval",
			cfg: Config{
				Interval:          0,
				LookbackPeriod:    -1,
				Strategy:          RecordCountStrategy,
				TargetRecordCount: 1000,
			},
			wantErr: "interval must be a non-zero value",
		},
		{
			name: "negative interval",
			cfg: Config{
				Interval:          -time.Minute,
				LookbackPeriod:    -1,
				Strategy:          RecordCountStrategy,
				TargetRecordCount: 1000,
			},
			wantErr: "interval must be a non-zero value",
		},
		{
			name: "invalid lookback period",
			cfg: Config{
				Interval:          time.Minute,
				LookbackPeriod:    -3,
				Strategy:          RecordCountStrategy,
				TargetRecordCount: 1000,
			},
			wantErr: "only -1(latest) and -2(earliest) are valid as negative values for lookback_period",
		},
		{
			name: "invalid strategy",
			cfg: Config{
				Interval:          time.Minute,
				LookbackPeriod:    -1,
				Strategy:          "invalid",
				TargetRecordCount: 1000,
			},
			wantErr: "invalid strategy: invalid",
		},
		{
			name: "zero target record count",
			cfg: Config{
				Interval:          time.Minute,
				LookbackPeriod:    -1,
				Strategy:          RecordCountStrategy,
				TargetRecordCount: 0,
			},
			wantErr: "target record count must be a non-zero value",
		},
		{
			name: "negative target record count",
			cfg: Config{
				Interval:          time.Minute,
				LookbackPeriod:    -1,
				Strategy:          RecordCountStrategy,
				TargetRecordCount: -1000,
			},
			wantErr: "target record count must be a non-zero value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("Validate() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if err.Error() != tt.wantErr {
					t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() error = %v, wantErr nil", err)
			}
		})
	}
}

// Worker handles communication with the scheduler service.
type Worker struct {
	transport types.BuilderTransport
	builderID string
}

// NewWorker creates a new Worker instance.
func NewWorker(builderID string, transport types.BuilderTransport) *Worker {
	return &Worker{
		transport: transport,
		builderID: builderID,
	}
}

// GetJob requests a new job from the scheduler.
func (w *Worker) GetJob(ctx context.Context) (*types.Job, bool, error) {
	resp, err := w.transport.SendGetJobRequest(ctx, &types.GetJobRequest{
		BuilderID: w.builderID,
	})
	if err != nil {
		return nil, false, err
	}
	return resp.Job, resp.OK, nil
}

// CompleteJob marks a job as finished.
func (w *Worker) CompleteJob(ctx context.Context, job *types.Job, success bool) error {
	err := w.transport.SendCompleteJob(ctx, &types.CompleteJobRequest{
		BuilderID: w.builderID,
		Job:       job,
		Success:   success,
	})
	return err
}

// SyncJob informs the scheduler about an in-progress job.
func (w *Worker) SyncJob(ctx context.Context, job *types.Job) error {
	err := w.transport.SendSyncJob(ctx, &types.SyncJobRequest{
		BuilderID: w.builderID,
		Job:       job,
	})
	return err
}
