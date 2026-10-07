package index

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccessStats_RequestTier(t *testing.T) {
	for name, tc := range map[string]struct {
		files    []string
		onDemand bool
		want     string
	}{
		"no files":                 {want: AccessTierNone},
		"memory only":              {files: []string{"memory", "memory"}, want: AccessTierMemory},
		"disk only":                {files: []string{"disk"}, want: AccessTierDisk},
		"memory and disk":          {files: []string{"memory", "disk", "memory"}, want: AccessTierDisk},
		"unknown tier counts disk": {files: []string{"memory", "elsewhere"}, want: AccessTierDisk},
		"on demand beats disk":     {files: []string{"disk"}, onDemand: true, want: AccessTierOnDemand},
		"on demand with no files":  {onDemand: true, want: AccessTierOnDemand},
		"on demand beats memory":   {files: []string{"memory"}, onDemand: true, want: AccessTierOnDemand},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, stats := NewContextWithAccessStats(context.Background())
			for _, tier := range tc.files {
				RecordFileAccess(ctx, tier)
			}
			if tc.onDemand {
				RecordOnDemand(ctx)
			}
			require.Equal(t, tc.want, stats.RequestTier())
		})
	}
}

func TestAccessStats_Concurrent(t *testing.T) {
	ctx, stats := NewContextWithAccessStats(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				RecordFileAccess(ctx, AccessTierMemory)
				RecordFileAccess(ctx, AccessTierDisk)
			}
		}()
	}
	wg.Wait()

	memory, disk := stats.FileAccesses()
	require.Equal(t, int64(800), memory)
	require.Equal(t, int64(800), disk)
}

func TestAccessStats_NoStatsInContext(t *testing.T) {
	// Recording into a context without AccessStats must be a no-op.
	require.NotPanics(t, func() {
		ctx := context.Background()
		RecordFileAccess(ctx, AccessTierMemory)
		RecordOnDemand(ctx)
	})
}
