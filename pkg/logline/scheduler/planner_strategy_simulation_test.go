package scheduler

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/grafana/loki/v3/pkg/logline/store"
)

// estimateDurationSeconds returns estimated total wall-clock seconds for a
// compaction job (download + merge + dedup + upload). Linear in input size.
//
// Recalibrated 2026-03-05 from a development environment's per-job logs after streaming merge
// with bounded block cache (n=75 jobs: 70 raw pairs ~980 MiB + 5 compacted
// pairs 3000-3700 MiB, all 2-source):
//
//	least-squares fit: 29.1 + 0.0811*MiB
//	median |residual| = 9.7s, p95 |residual| = 20.8s
//
// The streaming merge is ~30% faster per MiB than the batch merge (old slope
// 0.135 vs new 0.081). The higher intercept (29.1 vs 5.5) captures fixed costs
// (download, upload, iterator init) now visible with the wider input range.
// Observed: raw pairs p50=105s at ~980 MiB, compacted pairs p50=281s at ~3200 MiB.
// The per-source penalty (2.3s) is retained from prior calibration
// (2026-03-04, dateGroup planner with multi-source jobs).
func estimateDurationSeconds(totalBytes int64, sourceCount int) float64 {
	totalMiB := float64(totalBytes) / float64(mib)
	d := 29.1 + 0.0811*totalMiB
	if sourceCount > 2 {
		d += float64(sourceCount-2) * 2.3
	}
	return d
}

// ---------------------------------------------------------------------------
// Time-stepping compaction simulation
//
// Models continuous builder output, compaction with configurable dedup
// cost/benefit, and projects active index count + size over time.
//
// All tunables are in simConfig with defaults derived from measured
// data (2026-03-04).
// ---------------------------------------------------------------------------

const mib = 1024 * 1024

// simConfigEnv names the environment variable that points to a YAML config
// file. If set, loadSimConfig reads it and overlays values onto the defaults.
//
//	SIM_CONFIG=testdata/sim_config.yaml go test -v -run TestCompactionSim ./target/scheduler/...
const simConfigEnv = "SIM_CONFIG"

// simConfig holds every tunable for the compaction simulation.
type simConfig struct {
	// pods
	BuilderCount int `yaml:"builder_count"` // default: 20
	WorkerCount  int `yaml:"worker_count"`  // default: 10

	// output — calibrated from a development environment's metrics 2026-03-06 (3h window, 7 active builders);
	// size distribution: weighted-bucket model (see simSizeBuckets)
	IndexesPerBuilderPerHour float64 `yaml:"indexes_per_builder_per_hour"` // default: 44.0 (fleet ~310/hr ÷ 7 active builders)

	// config
	PollInterval    time.Duration `yaml:"poll_interval"`      // default: 15m
	MaxJobSizeBytes int64         `yaml:"max_job_size_bytes"` // default: 512 MiB

	// model — recalibrated from a development environment's per-job logs 2026-03-05 (n=75 jobs, overlap planner)
	// verified 2026-03-06: fleet output ratio median=0.86, consistent with overlap model
	BaseReductionRatio float64 `yaml:"base_reduction_ratio"` // default: 0.97
	Seed               int64   `yaml:"seed"`                 // default: 42
}

func defaultSimConfig() simConfig {
	return simConfig{
		BuilderCount:             7,
		WorkerCount:              30,
		PollInterval:             15 * time.Minute,
		IndexesPerBuilderPerHour: 44.0,
		MaxJobSizeBytes:          8192 * mib,
		BaseReductionRatio:       0.97,
		Seed:                     42,
	}
}

// loadSimConfig returns defaultSimConfig() with any overrides from
// simConfigPath applied on top. If the file does not exist, pure defaults
// are returned. Any field present in the YAML replaces the default; fields
// absent from the YAML keep their default value.
func loadSimConfig(t *testing.T) simConfig {
	t.Helper()
	cfg := defaultSimConfig()

	path := os.Getenv(simConfigEnv)
	if path == "" {
		return cfg
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading sim config from %s", path)
	require.NoError(t, yaml.Unmarshal(data, &cfg), "parsing sim config from %s", path)

	t.Logf("loaded sim config overrides from %s", path)
	return cfg
}

// overlapFraction computes the average pairwise overlap between source time
// ranges. For each pair (i, j), overlap = intersection / union of their time
// ranges. The result is the mean across all pairs.
//
// This captures partial-overlap scenarios: if 2 of 3 sources overlap heavily
// but the 3rd is disjoint, the average reflects the overlap that exists
// rather than returning 0 (as a full-intersection approach would).
func overlapFraction(sources []store.Meta) float64 {
	if len(sources) < 2 {
		return 0
	}

	for _, s := range sources {
		if s.MaxLogTs.Before(s.MinLogTs) {
			panic(fmt.Sprintf(
				"simulation invariant violated: reversed time range for source %q (min=%s max=%s)",
				s.ID(), s.MinLogTs, s.MaxLogTs,
			))
		}
	}

	var sum float64
	var pairs int
	for i := 0; i < len(sources); i++ {
		a := sources[i]
		if a.MinLogTs.IsZero() || a.MaxLogTs.IsZero() || !a.MaxLogTs.After(a.MinLogTs) {
			continue
		}
		for j := i + 1; j < len(sources); j++ {
			b := sources[j]
			if b.MinLogTs.IsZero() || b.MaxLogTs.IsZero() || !b.MaxLogTs.After(b.MinLogTs) {
				continue
			}
			pairs++

			// intersection
			intMin := a.MinLogTs
			if b.MinLogTs.After(intMin) {
				intMin = b.MinLogTs
			}
			intMax := a.MaxLogTs
			if b.MaxLogTs.Before(intMax) {
				intMax = b.MaxLogTs
			}
			if !intMax.After(intMin) {
				continue // disjoint
			}

			// union
			uMin := a.MinLogTs
			if b.MinLogTs.Before(uMin) {
				uMin = b.MinLogTs
			}
			uMax := a.MaxLogTs
			if b.MaxLogTs.After(uMax) {
				uMax = b.MaxLogTs
			}
			union := uMax.Sub(uMin).Seconds()
			if union <= 0 {
				continue
			}
			sum += intMax.Sub(intMin).Seconds() / union
		}
	}
	if pairs == 0 {
		return 0
	}
	return sum / float64(pairs)
}

// outputSizeRatio returns the output/input size ratio for a merge.
// The ratio depends on time range overlap between sources:
// more overlap → more duplicate documents → smaller output.
//
// Model: ratio = base_ratio * (1 - overlap * 0.5)
//
// At overlap=0 (disjoint time ranges): ratio = base_ratio (no dedup benefit)
// At overlap=1 (identical time ranges): ratio = base_ratio * 0.5 (half the docs are dupes)
func outputSizeRatio(sources []store.Meta, cfg simConfig) float64 {
	overlap := overlapFraction(sources)
	return cfg.BaseReductionRatio * (1 - overlap*0.5)
}

// simSnapshot records simulation state at a point in time.
type simSnapshot struct {
	Elapsed          time.Duration
	ActiveCount      int
	ActiveSizeBytes  int64
	BusyWorkers      int
	BusySecondsCum   float64
	JobsCompletedCum int
	BytesWrittenCum  int64
	SkippedOversized int
}

// simResult holds the full simulation output.
type simResult struct {
	Label       string // human-readable planner name for reports
	Config      simConfig
	Snapshots   []simSnapshot
	FinalActive int
	FinalSize   int64
	TotalJobs   int
}

type inFlightJob struct {
	sourceIDs  []string
	outputSize int64
	outputMinT time.Time
	outputMaxT time.Time
	finishAt   time.Duration
}

// plannerFactory creates a CompactionPlanner from the current active set.
type plannerFactory func(metas []store.Meta, maxJobSizeBytes int64) CompactionPlanner

func dateGroupPlannerFactory(metas []store.Meta, maxJobSizeBytes int64) CompactionPlanner {
	return newDateGroupPlanner(metas, maxJobSizeBytes)
}

// naivePlanner pairs indexes sequentially in arrival order (first with second,
// third with fourth, etc.), skipping any pair that would exceed maxJobSizeBytes.
// This is the simplest possible strategy — no size-aware grouping, no
// prioritization. It exists as a baseline to measure how much the smarter
// dateGroupPlanner improves compaction.
type naivePlanner struct {
	groups  [][]string
	idx     int
	skipped int
}

func newNaivePlanner(metas []store.Meta, maxJobSizeBytes int64) CompactionPlanner {
	byDate := make(map[string][]store.Meta)
	for _, m := range metas {
		byDate[m.Date] = append(byDate[m.Date], m)
	}

	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var groups [][]string
	var skipped int
	for _, d := range dates {
		ms := byDate[d]
		if len(ms) < 2 {
			continue
		}

		// Sort by ID for deterministic pairing.
		sort.Slice(ms, func(i, j int) bool { return ms[i].ID() < ms[j].ID() })

		i := 0
		for i+1 < len(ms) {
			a, b := ms[i], ms[i+1]
			if a.SizeBytes+b.SizeBytes > maxJobSizeBytes {
				skipped++
				i++
				continue
			}
			ids := []string{a.ID(), b.ID()}
			sort.Strings(ids)
			groups = append(groups, ids)
			i += 2
		}
		if i < len(ms) {
			skipped++ // leftover singleton
		}
	}

	return &naivePlanner{groups: groups, skipped: skipped}
}

func (p *naivePlanner) Plan() []string {
	if p.idx >= len(p.groups) {
		return nil
	}
	group := p.groups[p.idx]
	p.idx++
	return group
}

func (p *naivePlanner) Groups() [][]string { return p.groups }

func naivePlannerFactory(metas []store.Meta, maxJobSizeBytes int64) CompactionPlanner {
	return newNaivePlanner(metas, maxJobSizeBytes)
}

func overlapPlannerFactory(metas []store.Meta, maxJobSizeBytes int64) CompactionPlanner {
	return newOverlapPlanner(metas, maxJobSizeBytes)
}

// simSizeBucket is one bucket in the production raw-index-size histogram.
// Weights need not sum to any specific value; only their relative magnitudes
// matter for sampling.
type simSizeBucket struct {
	minBytes int64
	maxBytes int64
	weight   float64
}

// simSizeBuckets is a measured index size histogram (n=5,367
// indexes, 2026-03-06). Weights are raw counts per bucket.
var simSizeBuckets = []simSizeBucket{
	{0 * mib, 1 * mib, 124},
	{1 * mib, 5 * mib, 1},
	{5 * mib, 10 * mib, 4},
	{10 * mib, 20 * mib, 408},
	{20 * mib, 40 * mib, 1641},
	{40 * mib, 60 * mib, 1387},
	{60 * mib, 80 * mib, 723},
	{80 * mib, 100 * mib, 390},
	{100 * mib, 150 * mib, 644},
	{150 * mib, 256 * mib, 44},
}

// simSizeCumWeights holds the precomputed prefix sums of bucket weights.
// simSizeCumWeights[i] is the total weight of buckets [0, i);
// the final entry equals totalWeight.
var simSizeCumWeights = func() []float64 {
	cw := make([]float64, len(simSizeBuckets)+1)
	for i, b := range simSizeBuckets {
		cw[i+1] = cw[i] + b.weight
	}
	return cw
}()

// simRandomSize returns a random index size sampled from the production
// histogram. It selects a bucket in proportion to its weight, then samples
// uniformly within that bucket's byte range.
func simRandomSize(rng *rand.Rand) int64 {
	totalWeight := simSizeCumWeights[len(simSizeCumWeights)-1]
	r := rng.Float64() * totalWeight

	// Binary search for the bucket whose cumulative range contains r.
	lo, hi := 0, len(simSizeBuckets)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if simSizeCumWeights[mid+1] <= r {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	b := simSizeBuckets[lo]
	span := b.maxBytes - b.minBytes
	if span <= 0 {
		return b.minBytes
	}
	return b.minBytes + rng.Int63n(span)
}

func simPartitionFinishedJobs(until time.Duration, inFlight []inFlightJob) ([]inFlightJob, []inFlightJob, map[string]struct{}) {
	remaining := inFlight[:0]
	finished := make([]inFlightJob, 0, len(inFlight))
	remove := make(map[string]struct{})

	for _, j := range inFlight {
		if j.finishAt > until {
			remaining = append(remaining, j)
			continue
		}

		finished = append(finished, j)
		for _, id := range j.sourceIDs {
			remove[id] = struct{}{}
		}
	}

	return remaining, finished, remove
}

func simFilterActive(active []store.Meta, remove map[string]struct{}) []store.Meta {
	if len(remove) == 0 {
		return active
	}

	filtered := active[:0]
	for _, m := range active {
		if _, ok := remove[m.ID()]; ok {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func simAppendFinishedOutputs(active *[]store.Meta, finished []inFlightJob, rng *rand.Rand, jobsDone *int, bytesOut *int64) {
	for _, j := range finished {
		if len(j.sourceIDs) == 0 {
			panic("simulation invariant violated: finished job has no source IDs")
		}
		date, _, ok := strings.Cut(j.sourceIDs[0], "/")
		if !ok || date == "" {
			panic(fmt.Sprintf("simulation invariant violated: invalid source ID %q", j.sourceIDs[0]))
		}

		*active = append(*active, store.Meta{
			Version:   "v3",
			Date:      date,
			Hash:      fmt.Sprintf("%016x", rng.Uint64()),
			SizeBytes: j.outputSize,
			MinLogTs:  j.outputMinT,
			MaxLogTs:  j.outputMaxT,
		})
		*jobsDone = *jobsDone + 1
		*bytesOut += j.outputSize
	}
}

func simCompleteFinished(
	until time.Duration,
	active *[]store.Meta,
	inFlight *[]inFlightJob,
	rng *rand.Rand,
	jobsDone *int,
	bytesOut *int64,
) {
	if len(*inFlight) == 0 {
		return
	}

	remaining, finished, remove := simPartitionFinishedJobs(until, *inFlight)
	*inFlight = remaining

	if len(finished) == 0 {
		return
	}

	*active = simFilterActive(*active, remove)
	simAppendFinishedOutputs(active, finished, rng, jobsDone, bytesOut)
}

type simPlannerSnapshot struct {
	planner  CompactionPlanner
	metaByID map[string]store.Meta
	skipped  int
}

func simInFlightSourceIDs(inFlight []inFlightJob) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, j := range inFlight {
		for _, id := range j.sourceIDs {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func simPlannable(active []store.Meta, inFlightSourceIDs map[string]struct{}) []store.Meta {
	plannable := make([]store.Meta, 0, len(active))
	for _, m := range active {
		if _, ok := inFlightSourceIDs[m.ID()]; ok {
			continue
		}
		plannable = append(plannable, m)
	}
	return plannable
}

func simMetaByID(metas []store.Meta) map[string]store.Meta {
	metaByID := make(map[string]store.Meta, len(metas))
	for _, m := range metas {
		metaByID[m.ID()] = m
	}
	return metaByID
}

func simBuildPlannerSnapshot(cfg simConfig, active []store.Meta, inFlight []inFlightJob, pf plannerFactory) simPlannerSnapshot {
	plannable := simPlannable(active, simInFlightSourceIDs(inFlight))

	if len(plannable) < 2 {
		return simPlannerSnapshot{}
	}

	// Sort by ID to match production ordering: the store's Active() returns
	// metas in lexicographic order (from object storage listing).
	sort.Slice(plannable, func(i, j int) bool {
		return plannable[i].ID() < plannable[j].ID()
	})

	metaByID := simMetaByID(plannable)
	planner := pf(plannable, cfg.MaxJobSizeBytes)

	var skipped int
	switch p := planner.(type) {
	case *dateGroupPlanner:
		skipped = p.skipped
	case *naivePlanner:
		skipped = p.skipped
	case *overlapPlanner:
		skipped = p.skipped
	}

	return simPlannerSnapshot{
		planner:  planner,
		metaByID: metaByID,
		skipped:  skipped,
	}
}

func simGroupSources(group []string, metaByID map[string]store.Meta) ([]store.Meta, int64) {
	sources := make([]store.Meta, 0, len(group))
	var totalInput int64

	for _, id := range group {
		m, ok := metaByID[id]
		if !ok {
			panic(fmt.Sprintf("simulation invariant violated: planner returned unknown source ID %q", id))
		}
		sources = append(sources, m)
		totalInput += m.SizeBytes
	}

	return sources, totalInput
}

func simOutputTimeRange(sources []store.Meta) (time.Time, time.Time) {
	var outMinT, outMaxT time.Time
	for _, s := range sources {
		if outMinT.IsZero() || (!s.MinLogTs.IsZero() && s.MinLogTs.Before(outMinT)) {
			outMinT = s.MinLogTs
		}
		if outMaxT.IsZero() || s.MaxLogTs.After(outMaxT) {
			outMaxT = s.MaxLogTs
		}
	}
	return outMinT, outMaxT
}

func simAssignJobs(now time.Duration, cfg simConfig, snap simPlannerSnapshot, inFlight *[]inFlightJob) {
	if snap.planner == nil {
		return
	}

	for cfg.WorkerCount-len(*inFlight) > 0 {
		group := snap.planner.Plan()
		if group == nil {
			break
		}
		if len(group) < 2 {
			panic(fmt.Sprintf("simulation invariant violated: planner returned group with %d source(s)", len(group)))
		}

		// Look up source metas for size, time range, and overlap computation.
		sources, totalInput := simGroupSources(group, snap.metaByID)
		if totalInput <= 0 {
			panic(fmt.Sprintf("simulation invariant violated: non-positive total input for group %v", group))
		}

		outSize := int64(float64(totalInput) * outputSizeRatio(sources, cfg))
		dur := estimateDurationSeconds(totalInput, len(group))

		// Output time range is the union of all source ranges.
		outMinT, outMaxT := simOutputTimeRange(sources)

		*inFlight = append(*inFlight, inFlightJob{
			sourceIDs:  group,
			outputSize: outSize,
			outputMinT: outMinT,
			outputMaxT: outMaxT,
			finishAt:   now + time.Duration(dur*float64(time.Second)),
		})
	}
}

func simAppendRawIndexes(
	now time.Duration,
	tickDur time.Duration,
	cfg simConfig,
	indexesPerTick int,
	rng *rand.Rand,
	active *[]store.Meta,
	epoch time.Time,
) {
	// Model production reality: builders flush independently but cover the same
	// log time ranges. Each flush produces one index per builder, and all
	// builders covering the same period produce overlapping time ranges.
	//
	// Instead of perfectly staggering indexes across the tick window, we model
	// per-builder flushes: each builder flushes indexesPerBuilder times per tick,
	// and all builders flush at roughly the same wall-clock times (±jitter).
	// This creates groups of BuilderCount indexes with nearly identical time
	// ranges, which is what production actually looks like.

	indexesPerBuilder := indexesPerTick / cfg.BuilderCount
	if indexesPerBuilder < 1 {
		indexesPerBuilder = 1
	}
	flushWindow := time.Duration(float64(time.Hour) / cfg.IndexesPerBuilderPerHour)

	for flush := 0; flush < indexesPerBuilder; flush++ {
		// Base time for this flush across all builders.
		baseOffset := now + time.Duration(float64(flush)/float64(indexesPerBuilder)*float64(tickDur))

		for b := 0; b < cfg.BuilderCount; b++ {
			// Each builder's flush time has slight jitter (±10% of flush window)
			// to model independent flush timing.
			jitter := time.Duration(float64(flushWindow) * 0.1 * (rng.Float64()*2 - 1))
			maxT := baseOffset + jitter
			if maxT < flushWindow {
				maxT = flushWindow // ensure minT >= 0
			}
			minT := maxT - flushWindow

			*active = append(*active, store.Meta{
				Version:   "v3",
				Date:      "day-000",
				Hash:      fmt.Sprintf("%016x", rng.Uint64()),
				SizeBytes: simRandomSize(rng),
				MinLogTs:  epoch.Add(minT),
				MaxLogTs:  epoch.Add(maxT),
			})
		}
	}
}

func simTotalSize(active []store.Meta) int64 {
	var total int64
	for _, m := range active {
		total += m.SizeBytes
	}
	return total
}

func simNextFinish(inFlight []inFlightJob) (time.Duration, bool) {
	if len(inFlight) == 0 {
		return 0, false
	}

	next := inFlight[0].finishAt
	for _, j := range inFlight[1:] {
		if j.finishAt < next {
			next = j.finishAt
		}
	}
	return next, true
}

func simAccumulateBusy(until time.Duration, inFlight []inFlightJob, accountedUntil *time.Duration, busySeconds *float64) {
	if until < *accountedUntil {
		panic(fmt.Sprintf(
			"simulation invariant violated: time moved backwards (from %s to %s)",
			*accountedUntil, until,
		))
	}
	if until == *accountedUntil {
		return
	}

	dtSeconds := (until - *accountedUntil).Seconds()
	*busySeconds += float64(len(inFlight)) * dtSeconds
	*accountedUntil = until
}

// runSimulation models one day of builder output followed by compaction settling.
//
// Builders produce raw indexes for 24 hours on a single date ("day-000").
// Compaction runs continuously. After 24h builders stop and compaction
// continues until all workers are idle (no more mergeable groups).
//
// Each tick (pollInterval):
//  1. New raw builder indexes arrive (first 24h only).
//  2. Completed jobs produce merged output, remove sources.
//  3. Planner snapshot is built from current active set (minus in-flight).
//  4. Workers consume planner groups continuously until the next tick.
//  5. Snapshot is recorded.
func runSimulation(cfg simConfig, pf plannerFactory) simResult {
	rng := rand.New(rand.NewSource(cfg.Seed))

	var (
		active    []store.Meta
		inFlight  []inFlightJob
		snapshots []simSnapshot
		jobsDone  int
		bytesOut  int64

		accountedUntil time.Duration
		busySecondsCum float64
	)

	tickDur := cfg.PollInterval
	if tickDur <= 0 {
		panic(fmt.Sprintf("simulation invariant violated: poll_interval must be > 0 (got %s)", cfg.PollInterval))
	}
	indexesPerTick := int(float64(cfg.BuilderCount) * cfg.IndexesPerBuilderPerHour * tickDur.Hours())

	const builderDuration = 24 * time.Hour
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for tick := 0; ; tick++ {
		now := time.Duration(tick) * tickDur
		buildersActive := now < builderDuration

		// Track worker-seconds over elapsed wall time since the last event.
		simAccumulateBusy(now, inFlight, &accountedUntil, &busySecondsCum)

		// 1. Generate new raw builder indexes (first 24h only).
		// Each raw index covers the time since the builder's previous flush.
		if buildersActive {
			simAppendRawIndexes(now, tickDur, cfg, indexesPerTick, rng, &active, epoch)
		}

		// 2. Complete finished jobs.
		simCompleteFinished(now, &active, &inFlight, rng, &jobsDone, &bytesOut)

		// 3. Build planner snapshot and 4. assign to currently idle workers.
		planSnap := simBuildPlannerSnapshot(cfg, active, inFlight, pf)
		simAssignJobs(now, cfg, planSnap, &inFlight)

		// 5. Record snapshot.
		snapshots = append(snapshots, simSnapshot{
			Elapsed:          now,
			ActiveCount:      len(active),
			ActiveSizeBytes:  simTotalSize(active),
			BusyWorkers:      len(inFlight),
			BusySecondsCum:   busySecondsCum,
			JobsCompletedCum: jobsDone,
			BytesWrittenCum:  bytesOut,
			SkippedOversized: planSnap.skipped,
		})

		// Stop once builders are done and all workers are idle.
		if !buildersActive && len(inFlight) == 0 {
			break
		}

		// Between ticks, workers continuously request new jobs from the same
		// planner snapshot (mirrors production assign-on-request behavior).
		nextTick := time.Duration(tick+1) * tickDur
		for {
			if len(inFlight) == 0 || planSnap.planner == nil {
				break
			}

			nextFinish, ok := simNextFinish(inFlight)
			if !ok {
				break
			}
			if nextFinish > nextTick {
				break
			}

			simAccumulateBusy(nextFinish, inFlight, &accountedUntil, &busySecondsCum)
			simCompleteFinished(nextFinish, &active, &inFlight, rng, &jobsDone, &bytesOut)
			simAssignJobs(nextFinish, cfg, planSnap, &inFlight)
		}
	}

	return simResult{
		Config:      cfg,
		Snapshots:   snapshots,
		FinalActive: len(active),
		FinalSize:   simTotalSize(active),
		TotalJobs:   jobsDone,
	}
}

// logSimTable prints a summary table at hourly intervals.
func logSimTable(t *testing.T, label string, r simResult) {
	t.Helper()
	t.Logf("=== %s (builders=%d workers=%d) ===",
		label, r.Config.BuilderCount, r.Config.WorkerCount)
	t.Logf("%-8s %8s %10s %7s %8s %8s", "time", "active", "size_gib", "busy", "jobs_cum", "skipped")
	t.Logf("%-8s %8s %10s %7s %8s %8s", "----", "------", "--------", "----", "--------", "-------")

	for _, s := range r.Snapshots {
		if s.Elapsed%(1*time.Hour) != 0 {
			continue
		}
		t.Logf("%-8s %8d %10.1f %7d %8d %8d",
			s.Elapsed, s.ActiveCount,
			float64(s.ActiveSizeBytes)/float64(1024*1024*1024),
			s.BusyWorkers, s.JobsCompletedCum, s.SkippedOversized)
	}
	t.Logf("final: active=%d size=%.1f GiB jobs=%d",
		r.FinalActive, float64(r.FinalSize)/float64(1024*1024*1024), r.TotalJobs)
}

// ---------------------------------------------------------------------------
// Simulation tests
// ---------------------------------------------------------------------------

func TestSimUtilizationPoints_TimeWeighted(t *testing.T) {
	sampled := []simSnapshot{
		{Elapsed: 0, BusyWorkers: 10, BusySecondsCum: 0},
		{Elapsed: 1 * time.Hour, BusyWorkers: 10, BusySecondsCum: 3000}, // 10 workers * 5 min
		{Elapsed: 2 * time.Hour, BusyWorkers: 10, BusySecondsCum: 9000}, // +10 workers * 10 min
	}

	points := simUtilizationPoints(sampled, 10)
	require.Len(t, points, 3)
	require.Equal(t, 0.0, points[0].Value)
	require.InDelta(t, 8.333333, points[1].Value, 1e-6)
	require.InDelta(t, 16.666667, points[2].Value, 1e-6)
}

// simReportEnv names the environment variable that points to an output HTML file.
// If set, tests that call writeSimReport will write a self-contained HTML report.
//
//	SIM_REPORT=report.html go test -v -run TestCompactionSim_SteadyState ./target/scheduler/...
const simReportEnv = "SIM_REPORT"

// simReportSeries holds one time series for the report.
type simReportSeries struct {
	Label string
	Color string
	YAxis string // "y" (left) or "y1" (right)
	Data  []simReportPoint
}

type simReportPoint struct {
	Hours float64
	Value float64
}

func simUtilizationPoints(sampled []simSnapshot, workerCount int) []simReportPoint {
	if len(sampled) == 0 {
		return nil
	}

	points := []simReportPoint{{
		Hours: sampled[0].Elapsed.Hours(),
		Value: 0,
	}}
	if workerCount <= 0 {
		for i := 1; i < len(sampled); i++ {
			points = append(points, simReportPoint{
				Hours: sampled[i].Elapsed.Hours(),
				Value: 0,
			})
		}
		return points
	}

	for i := 1; i < len(sampled); i++ {
		dtSeconds := (sampled[i].Elapsed - sampled[i-1].Elapsed).Seconds()
		if dtSeconds <= 0 {
			continue
		}
		busyDelta := sampled[i].BusySecondsCum - sampled[i-1].BusySecondsCum
		util := busyDelta / (dtSeconds * float64(workerCount)) * 100
		if util < 0 {
			util = 0
		}
		if util > 100 {
			util = 100
		}

		points = append(points, simReportPoint{
			Hours: sampled[i].Elapsed.Hours(),
			Value: util,
		})
	}

	return points
}

// writeSimReport writes a self-contained HTML report with Chart.js graphs.
func writeSimReport(t *testing.T, label string, results ...simResult) {
	t.Helper()

	path := os.Getenv(simReportEnv)
	if path == "" {
		return
	}

	type chartDef struct {
		Title  string
		YLabel string
		Y1     string // optional right axis label
		Series []simReportSeries
	}

	colors := []string{
		"rgb(100, 160, 220)", // muted blue
		"rgb(200, 110, 110)", // muted red
		"rgb(90, 180, 130)",  // muted green
		"rgb(160, 120, 200)", // muted purple
		"rgb(200, 150, 90)",  // muted orange
		"rgb(80, 170, 160)",  // muted teal
	}

	// Pre-compute per-result sampled data and derived series.
	type resultData struct {
		label          string
		color          string
		sampled        []simSnapshot
		jobsPerHourPts []simReportPoint
		utilizationPts []simReportPoint
		toPoints       func(fn func(s simSnapshot) float64) []simReportPoint
	}

	allData := make([]resultData, len(results))
	for ri, r := range results {
		color := colors[ri%len(colors)]
		rlabel := r.Label
		if rlabel == "" {
			rlabel = fmt.Sprintf("run-%d", ri)
		}

		const interval = 1 * time.Hour
		var sampled []simSnapshot
		for _, s := range r.Snapshots {
			if s.Elapsed%interval == 0 {
				sampled = append(sampled, s)
			}
		}

		sampledCopy := sampled // capture for closure
		toPoints := func(fn func(s simSnapshot) float64) []simReportPoint {
			pts := make([]simReportPoint, len(sampledCopy))
			for i, s := range sampledCopy {
				pts[i] = simReportPoint{Hours: s.Elapsed.Hours(), Value: fn(s)}
			}
			return pts
		}

		var jobsPerHourPts []simReportPoint
		for i := 1; i < len(sampled); i++ {
			dt := (sampled[i].Elapsed - sampled[i-1].Elapsed).Hours()
			if dt <= 0 {
				continue
			}
			jph := float64(sampled[i].JobsCompletedCum-sampled[i-1].JobsCompletedCum) / dt
			jobsPerHourPts = append(jobsPerHourPts, simReportPoint{
				Hours: sampled[i].Elapsed.Hours(),
				Value: jph,
			})
		}

		utilizationPts := simUtilizationPoints(sampled, r.Config.WorkerCount)

		allData[ri] = resultData{
			label:          rlabel,
			color:          color,
			sampled:        sampled,
			jobsPerHourPts: jobsPerHourPts,
			utilizationPts: utilizationPts,
			toPoints:       toPoints,
		}
	}

	// Build shared charts — each metric gets one chart with one series per result.
	type metricDef struct {
		title  string
		yLabel string
		series func(rd resultData) []simReportSeries
	}

	metrics := []metricDef{
		{
			title: "Active Indexes", yLabel: "count",
			series: func(rd resultData) []simReportSeries {
				return []simReportSeries{
					{Label: rd.label + " active", Color: rd.color, YAxis: "y", Data: rd.toPoints(func(s simSnapshot) float64 { return float64(s.ActiveCount) })},
				}
			},
		},
		{
			title: "Skipped Oversized", yLabel: "count",
			series: func(rd resultData) []simReportSeries {
				return []simReportSeries{
					{Label: rd.label + " skipped", Color: rd.color, YAxis: "y", Data: rd.toPoints(func(s simSnapshot) float64 { return float64(s.SkippedOversized) })},
				}
			},
		},
		{
			title: "Total Size (GiB)", yLabel: "GiB",
			series: func(rd resultData) []simReportSeries {
				return []simReportSeries{
					{Label: rd.label, Color: rd.color, YAxis: "y", Data: rd.toPoints(func(s simSnapshot) float64 { return float64(s.ActiveSizeBytes) / float64(1024*1024*1024) })},
				}
			},
		},
		{
			title: "Jobs / Hour", yLabel: "jobs/h",
			series: func(rd resultData) []simReportSeries {
				return []simReportSeries{
					{Label: rd.label, Color: rd.color, YAxis: "y", Data: rd.jobsPerHourPts},
				}
			},
		},
		{
			title: "Worker Utilization", yLabel: "%",
			series: func(rd resultData) []simReportSeries {
				return []simReportSeries{
					{Label: rd.label, Color: rd.color, YAxis: "y", Data: rd.utilizationPts},
				}
			},
		},
		{
			title: "Cumulative Jobs Completed", yLabel: "jobs",
			series: func(rd resultData) []simReportSeries {
				return []simReportSeries{
					{Label: rd.label, Color: rd.color, YAxis: "y", Data: rd.toPoints(func(s simSnapshot) float64 { return float64(s.JobsCompletedCum) })},
				}
			},
		},
		{
			title: "Cumulative Bytes Written (GiB)", yLabel: "GiB",
			series: func(rd resultData) []simReportSeries {
				return []simReportSeries{
					{Label: rd.label, Color: rd.color, YAxis: "y", Data: rd.toPoints(func(s simSnapshot) float64 { return float64(s.BytesWrittenCum) / float64(1024*1024*1024) })},
				}
			},
		},
	}

	var charts []chartDef
	for _, m := range metrics {
		ch := chartDef{Title: m.title, YLabel: m.yLabel}
		for _, rd := range allData {
			ch.Series = append(ch.Series, m.series(rd)...)
		}
		charts = append(charts, ch)
	}

	// Build HTML.
	var buf strings.Builder
	buf.WriteString(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Compaction Simulation Report</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; margin: 20px; background: #1a1a2e; color: #a0a0b8; }
h1 { color: #c0c0d0; }
.info { background: #22223a; padding: 16px; border-radius: 8px; margin-bottom: 20px; border: 1px solid #2a2a45; font-size: 14px; }
.info code { background: #2a2a45; padding: 2px 6px; border-radius: 4px; color: #b0b0c8; }
.chart-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
.chart-box { background: #22223a; padding: 16px; border-radius: 8px; border: 1px solid #2a2a45; }
canvas { width: 100% !important; height: 300px !important; }
@media (max-width: 900px) { .chart-grid { grid-template-columns: 1fr; } }
</style></head><body>
`)
	buf.WriteString(fmt.Sprintf("<h1>%s</h1>\n", label))

	// Config summary.
	if len(results) > 0 {
		cfg := results[0].Config
		buf.WriteString(`<div class="info">`)
		buf.WriteString(fmt.Sprintf(
			"<b>Config:</b> builders=<code>%d</code> workers=<code>%d</code> poll=<code>%s</code> "+
				"max_job=<code>%dMiB</code> "+
				"indexes/builder/h=<code>%.1f</code> raw_size=<code>weighted-bucket</code> "+
				"base_reduction=<code>%.2f</code>",
			cfg.BuilderCount, cfg.WorkerCount, cfg.PollInterval,
			cfg.MaxJobSizeBytes/mib,
			cfg.IndexesPerBuilderPerHour,
			cfg.BaseReductionRatio,
		))
		buf.WriteString("</div>\n")
	}

	// Global Chart.js dark theme defaults.
	buf.WriteString("<script>\n")
	buf.WriteString("Chart.defaults.color = '#8080a0';\n")
	buf.WriteString("Chart.defaults.borderColor = '#2a2a45';\n")
	buf.WriteString("Chart.defaults.plugins.title.color = '#b0b0c8';\n")
	buf.WriteString("Chart.defaults.plugins.legend.labels.color = '#8080a0';\n")
	buf.WriteString("Chart.defaults.plugins.tooltip.backgroundColor = '#2a2a45';\n")
	buf.WriteString("Chart.defaults.plugins.tooltip.titleColor = '#c0c0d0';\n")
	buf.WriteString("Chart.defaults.plugins.tooltip.bodyColor = '#a0a0b8';\n")
	buf.WriteString("Chart.defaults.plugins.tooltip.borderColor = '#3a3a55';\n")
	buf.WriteString("Chart.defaults.plugins.tooltip.borderWidth = 1;\n")
	buf.WriteString("</script>\n")

	buf.WriteString(`<div class="chart-grid">` + "\n")

	for i, ch := range charts {
		canvasID := fmt.Sprintf("chart%d", i)
		buf.WriteString(fmt.Sprintf(`<div class="chart-box"><canvas id="%s"></canvas></div>`+"\n", canvasID))
		buf.WriteString("<script>\n")

		// Build datasets JSON.
		buf.WriteString(fmt.Sprintf("new Chart(document.getElementById('%s'), {\n", canvasID))
		buf.WriteString("  type: 'line',\n")
		buf.WriteString("  data: { datasets: [\n")

		for _, s := range ch.Series {
			buf.WriteString("    {\n")
			buf.WriteString(fmt.Sprintf("      label: '%s',\n", s.Label))
			buf.WriteString(fmt.Sprintf("      borderColor: '%s',\n", s.Color))
			buf.WriteString(fmt.Sprintf("      backgroundColor: '%s',\n", s.Color))
			buf.WriteString("      borderWidth: 2, pointRadius: 0, tension: 0.2, fill: false,\n")
			if s.YAxis == "y1" {
				buf.WriteString("      yAxisID: 'y1',\n")
			} else {
				buf.WriteString("      yAxisID: 'y',\n")
			}
			buf.WriteString("      data: [")
			for j, pt := range s.Data {
				if j > 0 {
					buf.WriteString(",")
				}
				buf.WriteString(fmt.Sprintf("{x:%.2f,y:%.2f}", pt.Hours, pt.Value))
			}
			buf.WriteString("]\n    },\n")
		}

		buf.WriteString("  ]},\n")
		buf.WriteString("  options: {\n")
		buf.WriteString("    responsive: true, maintainAspectRatio: false, interaction: { mode: 'index', intersect: false },\n")
		buf.WriteString(fmt.Sprintf("    plugins: { title: { display: true, text: '%s' }, legend: { position: 'bottom' } },\n", ch.Title))
		buf.WriteString("    scales: {\n")
		buf.WriteString(fmt.Sprintf("      x: { type: 'linear', title: { display: true, text: 'hours' } },\n"))
		buf.WriteString(fmt.Sprintf("      y: { type: 'linear', position: 'left', title: { display: true, text: '%s' }, beginAtZero: true },\n", ch.YLabel))
		if ch.Y1 != "" {
			buf.WriteString(fmt.Sprintf("      y1: { type: 'linear', position: 'right', title: { display: true, text: '%s' }, beginAtZero: true, grid: { drawOnChartArea: false } },\n", ch.Y1))
		}
		buf.WriteString("    }\n  }\n});\n")
		buf.WriteString("</script>\n")
	}

	buf.WriteString("</div>\n</body></html>\n")

	require.NoError(t, os.WriteFile(path, []byte(buf.String()), 0644))
	t.Logf("wrote simulation report to %s", path)
}

// TestCompactionSim_SteadyState models one day of builder output, then watches
// compaction settle until only non-compactable indexes remain. Runs all
// registered planners and produces an overlay report comparing them.
func TestCompactionSim_SteadyState(t *testing.T) {
	cfg := loadSimConfig(t)

	planners := []struct {
		label   string
		factory plannerFactory
	}{
		{"dateGroup", dateGroupPlannerFactory},
		{"naive", naivePlannerFactory},
		{"overlap", overlapPlannerFactory},
	}

	var results []simResult
	for _, p := range planners {
		r := runSimulation(cfg, p.factory)
		r.Label = p.label
		logSimTable(t, p.label, r)
		results = append(results, r)

		lastSnap := r.Snapshots[len(r.Snapshots)-1]
		t.Logf("[%s] final: active=%d skipped=%d busy=%d jobs=%d",
			p.label, lastSnap.ActiveCount, lastSnap.SkippedOversized, lastSnap.BusyWorkers, lastSnap.JobsCompletedCum)
		if lastSnap.ActiveCount > 0 {
			t.Logf("[%s] residue: %.0f%% of active indexes are unmergeable",
				p.label, float64(lastSnap.SkippedOversized)/float64(lastSnap.ActiveCount)*100)
		}

		require.NotEmpty(t, r.Snapshots)
		require.Greater(t, r.TotalJobs, 0)
		require.Zero(t, lastSnap.BusyWorkers, "workers should be idle after settling")
	}

	writeSimReport(t, "Planner Comparison: Single-Day Compaction", results...)
}
