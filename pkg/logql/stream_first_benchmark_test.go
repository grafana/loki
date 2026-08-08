package logql_test

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	ingesterclient "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	benchTenant = "fake"

	// Fixture parameters: changing any of these changes the content hash, regenerating the cache.
	fixtureVersion   = 2 // bump when the generation logic changes
	fixtureSeed      = 1
	benchNumStreams  = 2000
	benchLinesPerStr = 5000
	benchLineBytes   = 100 // ~2000*5000*100 ≈ 1 GB uncompressed
	benchDay         = 24 * time.Hour
	benchDaySecs     = 24 * 60 * 60

	// Label schema: 10 names on every stream, values padded to ~20 B, with a cardinality mix.
	labelAllName        = "cluster" // cardinality 1: matches all streams (high input)
	labelAllValue       = "prod-us-central1-01"
	labelMediumCardName = "namespace" // sum by(...) grouping (medium output)
	numMediumCardValues = 30
	labelSubsetName     = "team" // labelSubsetValue matches ~40 streams (low input)
	labelSubsetValue    = "team-canary-00000001"
	labelSubsetPeriod   = 50 // 1 in 50 streams
	labelUniqueName     = "pod"
)

var (
	benchStart = time.Unix(0, 0).UTC() // 1970-01-01T00:00:00Z
	benchEnd   = benchStart.Add(benchDay)
)

// BenchmarkLogQLQueries runs each query through the real engine over a filesystem store (real
// chunks + TSDB index), in both execution modes.
func BenchmarkLogQLQueries(b *testing.B) {
	const benchStep = 5 * time.Minute // range-query step: 24h / 288

	// benchQuery is one query in the suite.
	type benchQuery struct {
		name    string
		query   string
		instant bool
	}

	var (
		latency = &atomic.Int64{}
		querier = openBenchStore(b, ensureBenchFixtures(b), latency)
		ctx     = user.InjectOrgID(context.Background(), benchTenant)

		all     = fmt.Sprintf("{%s=%q}", labelAllName, labelAllValue)
		subset  = fmt.Sprintf("{%s=%q}", labelSubsetName, labelSubsetValue)
		groupBy = labelMediumCardName

		// benchQueries names encode the setup: <input>-in_<output>-out_<range>[_instant]. Input:
		// high = all 2000 streams, low = ~40. Output: low = sum() → 1, med = sum by(namespace) → 30,
		// high = no grouping → one per stream.
		benchQueries = []benchQuery{
			{"high-in_low-out_5m", fmt.Sprintf("sum(count_over_time(%s[5m]))", all), false},
			{"high-in_low-out_30m", fmt.Sprintf("sum(count_over_time(%s[30m]))", all), false},
			{"high-in_med-out_5m", fmt.Sprintf("sum by(%s) (count_over_time(%s[5m]))", groupBy, all), false},
			{"high-in_med-out_30m", fmt.Sprintf("sum by(%s) (count_over_time(%s[30m]))", groupBy, all), false},
			{"high-in_high-out_5m", fmt.Sprintf("count_over_time(%s[5m])", all), false},
			{"high-in_high-out_30m", fmt.Sprintf("count_over_time(%s[30m])", all), false},
			{"low-in_low-out_5m", fmt.Sprintf("sum(count_over_time(%s[5m]))", subset), false},
			{"low-in_low-out_30m", fmt.Sprintf("sum(count_over_time(%s[30m]))", subset), false},
			{"high-in_low-out_24h_instant", fmt.Sprintf("sum(count_over_time(%s[24h]))", all), true},
			{"high-in_med-out_24h_instant", fmt.Sprintf("sum by(%s) (count_over_time(%s[24h]))", groupBy, all), true},
			{"high-in_high-out_24h_instant", fmt.Sprintf("count_over_time(%s[24h])", all), true},
		}

		// benchModes: the two execution paths, compared over the same fixtures.
		benchModes = []struct {
			name          string
			streamOrdered bool // false = per-timestamp (default), true = per-stream
		}{
			{"per-timestamp", false},
			{"per-stream", true},
		}

		// benchSources isolate cross-source dedup cost: store_without_duplicates reads once;
		// store_with_duplicates reads twice and merges, so the merge must dedup every sample.
		benchSources = []struct {
			name string
			q    logql.Querier
		}{
			{"store_without_duplicates", querier},
			{"store_with_duplicates", newDuplicatingBenchQuerier(querier)},
		}

		benchLatencies = []struct {
			name string
			d    time.Duration
		}{
			{"0s", 0},
			{"50ms", 50 * time.Millisecond},
			{"250ms", 250 * time.Millisecond},
		}
	)

	runQuery := func(b *testing.B, enging *logql.QueryEngine, params logql.Params) {
		res, err := enging.Query(params).Exec(ctx)
		require.NoError(b, err)
		require.NotNil(b, res.Data)
	}

	for _, m := range benchModes {
		b.Run("mode="+m.name, func(b *testing.B) {
			for _, src := range benchSources {
				engine := logql.NewEngine(logql.EngineOpts{StreamOrderedExecutionEnabled: m.streamOrdered}, src.q, logql.NoLimits, log.NewNopLogger())

				b.Run("source="+src.name, func(b *testing.B) {
					for _, q := range benchQueries {
						start, end, step := benchStart, benchEnd, benchStep
						if q.instant {
							start, end, step = benchEnd, benchEnd, 0
						}
						params, err := logql.NewLiteralParams(q.query, start, end, step, 0, logproto.FORWARD, 0, nil, nil)
						require.NoError(b, err)

						b.Run("query="+q.name, func(b *testing.B) {
							for _, lat := range benchLatencies {
								b.Run("latency="+lat.name, func(b *testing.B) {
									latency.Store(int64(lat.d))
									runQuery(b, engine, params) // untimed warmup: warm index/shipper state
									b.ReportAllocs()
									b.ResetTimer()
									for i := 0; i < b.N; i++ {
										runQuery(b, engine, params)
									}
								})
							}
						})
					}
				})
			}
		})
	}
}

// latencyObjectClient sleeps a fixed per-GET latency before each chunk read (index reads excluded),
// so the benchmark isolates chunk-fetch latency. The delay is a shared atomic, mutated per scenario.
type latencyObjectClient struct {
	client.ObjectClient
	latencyNs *atomic.Int64
}

// applyChunkLatency sleeps the configured per-GET latency for chunk objects (index reads excluded).
func (c *latencyObjectClient) applyChunkLatency(key string) {
	if strings.HasPrefix(key, "index") {
		return // index reads are not the thing under test
	}
	if d := c.latencyNs.Load(); d > 0 {
		time.Sleep(time.Duration(d))
	}
}

func (c *latencyObjectClient) GetObject(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	c.applyChunkLatency(key)
	return c.ObjectClient.GetObject(ctx, key)
}

func (c *latencyObjectClient) GetObjectRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error) {
	c.applyChunkLatency(key)
	return c.ObjectClient.GetObjectRange(ctx, key, off, length)
}

// benchStoreConfig builds the filesystem+TSDB store config rooted at dir, with optional latency injection.
func benchStoreConfig(dir string, latencyNs *atomic.Int64) (storage.Config, config.SchemaConfig) {
	storeConfig := storage.Config{
		MaxChunkBatchSize:   50,
		MaxParallelGetChunk: 150, // production default; the literal Config skips RegisterFlags
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory: filepath.Join(dir, "index"),
			Mode:                 indexshipper.ModeReadWrite,
			IngesterName:         "bench",
			CacheLocation:        filepath.Join(dir, "cache"),
			ResyncInterval:       5 * time.Minute,
			CacheTTL:             24 * time.Hour,
		},
		FSConfig: local.FSConfig{Directory: filepath.Join(dir, "storage")},
	}
	if latencyNs != nil {
		storeConfig.ObjectClientDecorator = func(oc client.ObjectClient) client.ObjectClient {
			return &latencyObjectClient{ObjectClient: oc, latencyNs: latencyNs}
		}
	}
	period := config.PeriodConfig{
		From:       config.DayTime{Time: model.Earliest},
		IndexType:  "tsdb",
		ObjectType: "filesystem",
		Schema:     "v13",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix:          "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: 24 * time.Hour},
		},
	}
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{period}}
	return storeConfig, schemaCfg
}

// openBenchStore opens a LokiStore over dir (fixtures already present). No chunk cache, so every
// fetch hits the object store.
func openBenchStore(tb testing.TB, dir string, latencyNs *atomic.Int64) *storage.LokiStore {
	tb.Helper()
	storeConfig, schemaCfg := benchStoreConfig(dir, latencyNs)

	// Pre-warm the memoized schema version single-threaded.
	for i := range schemaCfg.Configs {
		_, err := schemaCfg.Configs[i].VersionAsInt()
		require.NoError(tb, err)
	}

	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	overrides, err := validation.NewOverrides(limits, nil)
	require.NoError(tb, err)

	store, err := storage.NewStore(storeConfig, config.ChunkStoreConfig{}, schemaCfg, overrides, storage.ClientMetrics{}, prometheus.NewRegistry(), util_log.Logger, "cortex")
	require.NoError(tb, err)
	return store
}

func newBenchMemChunk() *chunkenc.MemChunk {
	const (
		targetChunkSize = 1024 * 1024
		blockSize       = 256 * 1024
	)
	return chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.Snappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, targetChunkSize)
}

// writeBenchStream encodes one stream into one or more chunks and Puts them into the store.
func writeBenchStream(tb testing.TB, store *storage.LokiStore, stream logproto.Stream) {
	tb.Helper()
	lbs, err := syntax.ParseLabels(stream.Labels)
	require.NoError(tb, err)
	metric := labels.NewBuilder(lbs).Set(model.MetricNameLabel, "logs").Labels()
	fp := ingesterclient.Fingerprint(lbs)

	put := func(mc *chunkenc.MemChunk) {
		require.NoError(tb, mc.Close())
		firstTime, lastTime := util.RoundToMilliseconds(mc.Bounds())
		c := chunk.NewChunk(benchTenant, fp, metric, chunkenc.NewFacade(mc, 0, 0), firstTime, lastTime)
		require.NoError(tb, c.Encode())
		require.NoError(tb, store.Put(context.Background(), []chunk.Chunk{c}))
	}

	mc := newBenchMemChunk()
	for i := range stream.Entries {
		e := stream.Entries[i]
		if !mc.SpaceFor(&e) {
			put(mc)
			mc = newBenchMemChunk()
		}
		dup, err := mc.Append(&e)
		require.NoError(tb, err)
		require.Falsef(tb, dup, "duplicate entry in stream %s at %s", stream.Labels, e.Timestamp)
	}
	put(mc)
}

// ensureBenchFixtures generates the fixtures once into a content-addressed dir (reused if its
// ".done" sentinel exists) and returns its path. Concurrent generators race via a temp dir + rename.
func ensureBenchFixtures(tb testing.TB) string {
	tb.Helper()
	final := filepath.Join(os.TempDir(), "loki-sfbench-"+benchFixtureHash())
	if _, err := os.Stat(filepath.Join(final, ".done")); err == nil {
		return final
	}

	tmp, err := os.MkdirTemp(os.TempDir(), "loki-sfbench-gen-")
	require.NoError(tb, err)
	defer os.RemoveAll(tmp)

	genLatency := &atomic.Int64{} // zero: generation is not delayed
	store := openBenchStore(tb, tmp, genLatency)

	rng := newBenchRNG()
	for i := 0; i < benchNumStreams; i++ {
		writeBenchStream(tb, store, generateBenchStream(i, rng))
	}
	store.Stop()

	require.NoError(tb, os.WriteFile(filepath.Join(tmp, ".done"), []byte(benchFixtureHash()), 0o644))
	if err := os.Rename(tmp, final); err != nil {
		// A concurrent generator won the race; reuse the final dir if it now exists.
		if _, statErr := os.Stat(filepath.Join(final, ".done")); statErr == nil {
			return final
		}
		require.NoError(tb, err)
	}
	return final
}

// benchPadValue right-pads v toward ~20 B so label values average ~20 B.
func benchPadValue(v string) string {
	const target = 20
	if len(v) >= target {
		return v
	}
	return v + strings.Repeat("x", target-len(v))
}

// benchStreamLabels builds the 10-label set for stream i, deterministically.
func benchStreamLabels(i int) labels.Labels {
	b := labels.NewBuilder(labels.EmptyLabels())
	b.Set(labelAllName, labelAllValue)
	b.Set("region", benchPadValue(fmt.Sprintf("region-%d", i%2)))
	b.Set("tier", benchPadValue(fmt.Sprintf("tier-%d", i%3)))
	b.Set("service", benchPadValue(fmt.Sprintf("svc-%d", i%8)))
	b.Set(labelMediumCardName, benchPadValue(fmt.Sprintf("ns-%02d", i%numMediumCardValues)))
	b.Set("job", benchPadValue(fmt.Sprintf("job-%02d", i%50)))
	b.Set("version", benchPadValue(fmt.Sprintf("v-%d", i%3)))
	if i%labelSubsetPeriod == 0 {
		b.Set(labelSubsetName, labelSubsetValue)
	} else {
		b.Set(labelSubsetName, benchPadValue(fmt.Sprintf("team-%02d", i%13)))
	}
	b.Set("zone", benchPadValue(fmt.Sprintf("zone-%d", i%4)))
	b.Set(labelUniqueName, benchPadValue(fmt.Sprintf("pod-%06d", i)))
	return b.Labels()
}

// benchStreamWindow returns the [offset, offset+dur) window (whole seconds) stream i's lines span:
// 50% of streams cover the full 24h, 30% cover 50%, 20% cover 10%, at a deterministic offset. Whole
// seconds avoid the float-seconds precision loss in chunk metadata.
func benchStreamWindow(i int, rng *rand.Rand) (offsetSecs, durSecs int64) {
	switch {
	case i%10 < 5: // 50%
		return 0, benchDaySecs
	case i%10 < 8: // 30%
		durSecs = benchDaySecs / 2
	default: // 20%
		durSecs = benchDaySecs / 10
	}
	offsetSecs = rng.Int63n(benchDaySecs - durSecs + 1) // whole-second offset in [0, day-dur]
	return offsetSecs, durSecs
}

// benchLine builds a ~benchLineBytes line, unique per (stream, line) so chunk dedup never drops it.
func benchLine(streamIdx, lineIdx int) string {
	prefix := fmt.Sprintf("level=info stream=%d line=%d msg=\"request served\" ", streamIdx, lineIdx)
	if len(prefix) >= benchLineBytes {
		return prefix[:benchLineBytes]
	}
	return prefix + strings.Repeat("=", benchLineBytes-len(prefix))
}

// generateBenchStream builds stream i: its labels and benchLinesPerStr entries evenly spaced (whole
// seconds, distinct) across its window. One stream at a time, so the ~1 GB corpus is never fully resident.
func generateBenchStream(i int, rng *rand.Rand) logproto.Stream {
	lbls := benchStreamLabels(i)
	offsetSecs, durSecs := benchStreamWindow(i, rng)
	stepSecs := durSecs / int64(benchLinesPerStr)
	if stepSecs < 1 {
		stepSecs = 1 // keep timestamps distinct even if lines > window-seconds
	}

	entries := make([]logproto.Entry, benchLinesPerStr)
	for j := 0; j < benchLinesPerStr; j++ {
		ts := benchStart.Add(time.Duration(offsetSecs+int64(j)*stepSecs) * time.Second)
		entries[j] = logproto.Entry{Timestamp: ts, Line: benchLine(i, j)}
	}
	return logproto.Stream{Labels: lbls.String(), Entries: entries}
}

// newBenchRNG returns the deterministic PRNG for fixture generation (window offsets).
func newBenchRNG() *rand.Rand {
	return rand.New(rand.NewSource(fixtureSeed)) //nolint:gosec // determinism, not security
}

// benchFixtureHash is the content-addressed cache key derived from the fixture parameters.
func benchFixtureHash() string {
	return fmt.Sprintf("v%d-s%d-str%d-lin%d-b%d", fixtureVersion, fixtureSeed, benchNumStreams, benchLinesPerStr, benchLineBytes)
}

// duplicatingBenchQuerier reads the store twice with the same request and merges the two identical
// results through the real merge iterator, so every sample is duplicated and the merge must dedup
// each pair — modeling replica (RF>1) dedup with production code.
type duplicatingBenchQuerier struct {
	store logql.Querier
}

func newDuplicatingBenchQuerier(store logql.Querier) *duplicatingBenchQuerier {
	return &duplicatingBenchQuerier{store: store}
}

func (q *duplicatingBenchQuerier) SelectLogs(context.Context, logql.SelectLogParams) (iter.EntryIterator, error) {
	return nil, fmt.Errorf("SelectLogs not implemented in duplicatingBenchQuerier")
}

func (q *duplicatingBenchQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	first, err := q.store.SelectSamples(ctx, params)
	if err != nil {
		return nil, err
	}
	second, err := q.store.SelectSamples(ctx, params)
	if err != nil {
		return nil, err
	}

	iters := []iter.SampleIterator{first, second}
	if params.Order == logproto.SAMPLE_ORDER_BY_STREAM {
		return iter.NewStreamFirstMergeSampleIterator(ctx, iters), nil
	}
	return iter.NewTimestampFirstMergeSampleIterator(ctx, iters), nil
}
