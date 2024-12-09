package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/blockbuilder/builder"
	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

type testEnv struct {
	queue     *JobQueue
	scheduler *BlockScheduler
	transport *types.MemoryTransport
	builder   *builder.Worker
}

func newTestEnv(builderID string) *testEnv {
	queue := NewJobQueue()
	scheduler := NewScheduler(Config{}, queue, nil, log.NewNopLogger(), prometheus.NewRegistry())
	transport := types.NewMemoryTransport(scheduler)
	builder := builder.NewWorker(builderID, transport)

	return &testEnv{
		queue:     queue,
		scheduler: scheduler,
		transport: transport,
		builder:   builder,
	}
}

func TestScheduleAndProcessJob(t *testing.T) {
	env := newTestEnv("test-builder-1")
	ctx := context.Background()

	// Create and enqueue a test job
	job := types.NewJob(1, types.Offsets{Min: 100, Max: 200})
	err := env.queue.Enqueue(job, 100)
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
	if receivedJob.ID != job.ID {
		t.Errorf("got job ID %s, want %s", receivedJob.ID, job.ID)
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
	env := newTestEnv("test-builder-1")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Try to get job after context timeout
	time.Sleep(20 * time.Millisecond)
	_, _, err := env.builder.GetJob(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestMultipleBuilders(t *testing.T) {
	// Create first environment
	env1 := newTestEnv("test-builder-1")
	// Create second builder using same scheduler
	builder2 := builder.NewWorker("test-builder-2", env1.transport)

	ctx := context.Background()

	// Create test jobs
	job1 := types.NewJob(1, types.Offsets{Min: 100, Max: 200})
	job2 := types.NewJob(2, types.Offsets{Min: 300, Max: 400})

	// Enqueue jobs
	err := env1.queue.Enqueue(job1, 100)
	if err != nil {
		t.Fatalf("failed to enqueue job1: %v", err)
	}
	err = env1.queue.Enqueue(job2, 100)
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
	if receivedJob1.ID == receivedJob2.ID {
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
			// Check that planner is set for valid configs
			if tt.cfg.planner == nil {
				t.Error("Validate() did not set planner for valid config")
			}
		})
	}
}
