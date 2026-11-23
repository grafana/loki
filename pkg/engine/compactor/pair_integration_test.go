package compactor

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// compactPairsUntilIdle runs scans of the coupled planner and waits for their
// jobs, until a scan starts no job. Every job must commit, and after each
// scan every index run must form a pair with its log objects.
func (s *compactionScenario) compactPairsUntilIdle(tenant string) {
	s.t.Helper()
	p := s.coordinator.newPairCompactor(tenant)
	for range 10 {
		started := p.scan(s.ctx)
		p.jobs.Wait()
		s.requirePairs(tenant)
		for _, kind := range []string{pairJobKindMerge, pairJobKindSplit} {
			for _, outcome := range []string{pairJobOutcomeFailed, pairJobOutcomeStale, pairJobOutcomeNoop} {
				require.Zero(s.t, testutil.ToFloat64(s.coordinator.metrics.pairJobsTotal.WithLabelValues(tenant, kind, outcome)), "%s jobs with outcome %s", kind, outcome)
			}
		}
		if started == 0 {
			return
		}
	}
	s.t.Fatal("pair compaction did not reach a scan without jobs")
}

// requirePairs checks that every index run of tenant indexes exactly one log
// run, and returns the log runs.
func (s *compactionScenario) requirePairs(tenant string) []v2.Run {
	s.t.Helper()
	var logRuns []v2.Run
	for _, run := range groupIndexRuns(s.stored.Indexes(s.ctx, s.t, tenant)) {
		sections, _, err := logSectionRefsFor(s.ctx, s.stored.Bucket, tenant, run)
		require.NoError(s.t, err)
		runs := v2.CalculateRuns(sections, compareLogSortPrefix)
		require.Len(s.t, runs, 1, "index %s must index one log run", run)
		logRuns = append(logRuns, runs[0])
	}
	return logRuns
}

func TestE2EPairCompaction(t *testing.T) {
	layout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"})

	tests := []struct {
		name string

		// levelBase and levelRatio replace the default size levels when
		// levelBase is not zero.
		levelBase, levelRatio uint64

		logMaxTaskSize, indexMaxTaskSize uint64

		// mergeFirst runs an IndexMerge phase of the phased planner before
		// the coupled planner. It leaves one index run over every source, so
		// the coupled planner must split it into pairs first.
		mergeFirst bool

		wantPairs int

		// wantIndexMerges reports whether some merge must run IndexMerge
		// tasks after its LogMerge tasks.
		wantIndexMerges bool
	}{
		{
			name:      "pairs of one level converge to one pair",
			wantPairs: 1,
		},
		{
			name:            "a merge split into log tasks merges their indexes into one index run",
			logMaxTaskSize:  100_000,
			wantPairs:       1,
			wantIndexMerges: true,
		},
		{
			name:             "an index merge split by posting key writes one index run of several index objects",
			logMaxTaskSize:   100_000,
			indexMaxTaskSize: 1,
			wantPairs:        1,
			wantIndexMerges:  true,
		},
		{
			name:       "five pairs of 80KB with a 100KB level base and a ratio of 2 converge to one level 3 pair and one level 1 pair",
			levelBase:  100_000,
			levelRatio: 2,
			wantPairs:  2,
		},
		{
			name:       "an index run over several log runs splits into pairs and then converges",
			mergeFirst: true,
			wantPairs:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			const tenant = "acme"

			scenario, sourceLines := newFiveSourceScenario(ctx, t, tenant, layout)
			c := scenario.coordinator
			c.cfg.PlanVersion = planVersionCoupled
			c.cfg.LogMaxTaskSize = flagext.Bytes(test.logMaxTaskSize)
			c.cfg.MaxTaskSize = flagext.Bytes(test.indexMaxTaskSize)
			if test.levelBase != 0 {
				strategy, err := v2.NewSizeLeveledStrategy(test.levelBase, test.levelRatio, c.cfg.LogMaxRunsPerTask)
				require.NoError(t, err)
				c.logMergePlanningStrategy = strategy
			}
			actors := recordActors(c.pairDispatcher)
			if test.mergeFirst {
				require.Equal(t, phaseOutcomeSwapped, c.runIndexMergePhase(ctx, tenant, scenario.stored.Window, 0))
				require.Len(t, groupIndexRuns(scenario.stored.Indexes(ctx, t, tenant)), 1)
			}

			scenario.compactPairsUntilIdle(tenant)

			requireLogContents(t, scenario.stored.ReadReachableContents(ctx, t, tenant), sourceLines, layout)
			logRuns := scenario.requirePairs(tenant)
			require.Len(t, logRuns, test.wantPairs)
			require.False(t, c.logMergePlanningStrategy.NeedsCompaction(logRuns), "every size level must hold fewer than K pairs")
			require.Equal(t, test.wantIndexMerges, slices.Contains(actors(), "index-merge"))
			if test.mergeFirst {
				require.NotZero(t, testutil.ToFloat64(c.metrics.pairJobsTotal.WithLabelValues(tenant, pairJobKindSplit, pairJobOutcomeCommitted)))
			}
			if test.indexMaxTaskSize > 0 {
				indexes := scenario.stored.Indexes(ctx, t, tenant)
				require.Greater(t, len(indexes), 1, "the index run of the last merge must hold several index objects")
			}
		})
	}
}

// newFiveSourceScenario seeds five overlapping log objects of layout for
// tenant, each with its own index, and returns the scenario and the log lines
// of the sources. Each source holds about 80KB of log data.
func newFiveSourceScenario(ctx context.Context, t *testing.T, tenant string, layout streams.Layout) (*compactionScenario, []string) {
	t.Helper()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)
	base := window.Add(time.Hour)
	outputConfig := logsobj.BuilderBaseConfig{
		TargetPageSize: 2048, TargetSectionSize: 8 * 1024,
		TargetObjectSize: 16 * 1024, BufferSize: 2048,
		SectionStripeMergeLimit: 2, EstimatedCompressionRatio: 1,
	}
	input := scenarioInput{window: window, logsobjConfig: &outputConfig}
	payload := strings.Repeat("A", 1024)
	var sourceLines []string
	for file := range 5 {
		entries := fixtures.NewLogsFixtureBuilder(t, fixtures.WithLayout(layout))
		for app := 'a'; app <= 'z'; app++ {
			stream := entries.ForStream(fmt.Sprintf(`{app=%q}`, string(app)))
			for record := range 3 {
				line := fmt.Sprintf("file-%d/app-%c/record-%d:%s", file, app, record, payload)
				stream.Entry(int(base.Unix())+record, "{}", line)
				sourceLines = append(sourceLines, line)
			}
		}
		input.logs = append(input.logs, newScenarioLogSource(t, fmt.Sprintf("objects/source-%d", file), tenant, entries, layout))
	}
	return newCompactionScenario(ctx, t, input), sourceLines
}

func TestE2EPairCompactionRun(t *testing.T) {
	t.Run("coordinator Run with plan version 2 converges five pairs to one pair on a real worker", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		const tenant = "acme"
		layout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"})
		scenario, sourceLines := newFiveSourceScenario(ctx, t, tenant, layout)
		c := scenario.coordinator
		c.cfg.PlanVersion = planVersionCoupled
		c.cfg.RescanInterval = 10 * time.Millisecond
		c.cfg.PollingInterval = time.Hour
		c.sleep = sleepUntil
		c.sweeps = nil
		window := scenario.stored.Window

		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- c.Run(runCtx) }()
		require.Eventually(t, func() bool {
			entries, err := loadTenantIndexes(ctx, scenario.stored.Bucket, window, tenant)
			return err == nil && len(groupIndexRuns(entries)) == 1
		}, 30*time.Second, 10*time.Millisecond)
		stop()
		require.ErrorIs(t, <-done, context.Canceled)

		requireLogContents(t, scenario.stored.ReadReachableContents(ctx, t, tenant), sourceLines, layout)
		require.Len(t, scenario.requirePairs(tenant), 1)
	})
}
