package scheduler

import (
	"runtime"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// waitCollected forces GC repeatedly and reports whether done was closed (i.e.
// the finalizer for a released object fired) within a short window.
func waitCollected(done <-chan struct{}) bool {
	for range 100 {
		runtime.GC()
		select {
		case <-done:
			return true
		case <-time.After(5 * time.Millisecond):
		}
	}
	return false
}

// TestScheduler_TerminalCaptureIsCollectable proves the memory win of
// releaseTerminalResources: once a task reaches a terminal result, its per-task
// capture becomes unreachable and is collected by the GC even though the task
// entry (and its manifest) is still registered.
//
// Without the release, the capture stays reachable via s.tasks until
// UnregisterManifest, so the finalizer would not fire and this test would fail.
func TestScheduler_TerminalCaptureIsCollectable(t *testing.T) {
	sched := newTestScheduler(t)

	exampleTask := &workflow.Task{ULID: ulid.Make()}
	manifest := &workflow.Manifest{
		Tasks:               []*workflow.Task{exampleTask},
		StreamClosedHandler: nopStreamHandler,
		TaskResultHandler:   nopTaskHandler, // must NOT retain the delivered capture
	}
	require.NoError(t, sched.RegisterManifest(t.Context(), manifest))

	registered := sched.tasks[exampleTask.ULID]
	require.NotNil(t, registered.capture, "capture should be held while the task is live")

	collected := make(chan struct{})
	runtime.SetFinalizer(registered.capture, func(*xcap.Capture) { close(collected) })

	// Finish the task. The manifest (and the s.tasks entry) stays registered.
	require.NoError(t, sched.Cancel(t.Context(), exampleTask))

	require.True(t, waitCollected(collected),
		"per-task capture must be GC-eligible once the task is terminal; without releaseTerminalResources it stays reachable via s.tasks until UnregisterManifest")

	// The task entry and manifest are still live: the capture was freed even
	// though its owning task/manifest were not.
	require.Contains(t, sched.tasks, exampleTask.ULID)
	runtime.KeepAlive(manifest)
}
