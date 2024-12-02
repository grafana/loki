package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/blockbuilder/builder"
	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

type testEnv struct {
	queue     *JobQueue
	scheduler *SchedulerImpl
	transport *builder.MemoryTransport
	builder   *builder.WorkerImpl
}

func newTestEnv(builderID string) *testEnv {
	queue := NewJobQueue()
	scheduler := NewScheduler(queue)
	transport := builder.NewMemoryTransport(scheduler)
	builder := builder.NewWorker(builderID, builder.NewMemoryTransport(scheduler))

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
	err := env.queue.Enqueue(job)
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
	err = env.builder.CompleteJob(ctx, receivedJob)
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
	builder2 := builder.NewWorker("test-builder-2", builder.NewMemoryTransport(env1.scheduler))

	ctx := context.Background()

	// Create test jobs
	job1 := types.NewJob(1, types.Offsets{Min: 100, Max: 200})
	job2 := types.NewJob(2, types.Offsets{Min: 300, Max: 400})

	// Enqueue jobs
	err := env1.queue.Enqueue(job1)
	if err != nil {
		t.Fatalf("failed to enqueue job1: %v", err)
	}
	err = env1.queue.Enqueue(job2)
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
	err = env1.builder.CompleteJob(ctx, receivedJob1)
	if err != nil {
		t.Fatalf("builder1 failed to complete job: %v", err)
	}

	err = builder2.CompleteJob(ctx, receivedJob2)
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
