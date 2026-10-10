package compactor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type deadlineRecorder struct {
	deadline    time.Time
	hasDeadline bool
}

func (d *deadlineRecorder) ReplaceIndexPointers(ctx context.Context, _ time.Time, _ string, _ []string, _ []metastore.TableOfContentsEntry) (bool, error) {
	d.deadline, d.hasDeadline = ctx.Deadline()
	return true, nil
}

func TestPublisherReplace(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	oldPaths := []string{"indexes/old"}
	newEntries := []metastore.TableOfContentsEntry{{Path: "indexes/new"}}

	t.Run("forwards the swap to the writer and reports it applied", func(t *testing.T) {
		writer := &fakeReplacer{swapped: true}
		p := &tocPublisher{writer: writer, timeout: time.Second}

		swapped, err := p.Replace(context.Background(), "acme", window, oldPaths, newEntries)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []replaceCall{{window, "acme", oldPaths, newEntries}}, writer.snapshot())
	})

	t.Run("reports not swapped when the writer loses the race", func(t *testing.T) {
		p := &tocPublisher{writer: &fakeReplacer{swapped: false}, timeout: time.Second}

		swapped, err := p.Replace(context.Background(), "acme", window, oldPaths, newEntries)
		require.NoError(t, err)
		require.False(t, swapped)
	})

	t.Run("returns the writer error", func(t *testing.T) {
		boom := errors.New("boom")
		p := &tocPublisher{writer: &fakeReplacer{err: boom}, timeout: time.Second}

		swapped, err := p.Replace(context.Background(), "acme", window, oldPaths, newEntries)
		require.ErrorIs(t, err, boom)
		require.False(t, swapped)
	})

	t.Run("skips the writer and reports not swapped in dry-run mode", func(t *testing.T) {
		writer := &fakeReplacer{swapped: true}
		p := &tocPublisher{writer: writer, timeout: time.Second, dryRun: true}

		swapped, err := p.Replace(context.Background(), "acme", window, oldPaths, newEntries)
		require.NoError(t, err)
		require.False(t, swapped)
		require.Empty(t, writer.snapshot())
	})

	t.Run("bounds the writer call with the timeout", func(t *testing.T) {
		const timeout = time.Minute
		writer := &deadlineRecorder{}
		p := &tocPublisher{writer: writer, timeout: timeout}

		before := time.Now()
		_, err := p.Replace(context.Background(), "acme", window, oldPaths, newEntries)
		after := time.Now()
		require.NoError(t, err)
		require.True(t, writer.hasDeadline)
		require.False(t, writer.deadline.Before(before.Add(timeout)), "deadline is earlier than the timeout allows")
		require.False(t, writer.deadline.After(after.Add(timeout)), "deadline is later than the timeout allows")
	})
}
