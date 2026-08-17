package logql_test

import (
	"context"
	"errors"
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
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	ingesterclient "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier"
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
	fixtureVersion   = 5 // bump when the generation logic changes
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

	// Data-object fixture sizing: each object grows to ~32 MB (≈32x an average ~1 MB chunk) before it
	// is flushed, so the same 1 GB corpus becomes ~20 objects vs ~2000 chunks.
	benchDataObjTargetSize = 32 << 20
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
		counters                     = &benchCounters{}
		chunkQuerier, dataobjQuerier = openBenchStore(b, ensureBenchFixtures(b), counters)
		ctx                          = user.InjectOrgID(context.Background(), benchTenant)

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

		// benchSources compare backends and isolate cross-source dedup cost: *_without_duplicates reads
		// once; chunk_store_with_duplicates reads twice and merges, so the merge must dedup every sample.
		// The data-object source is read only under stream-ordered execution (requiresStreamOrdered),
		// so it is skipped in per-timestamp mode.
		benchSources = []struct {
			name                  string
			q                     logql.Querier
			requiresStreamOrdered bool
		}{
			{"chunk_store_without_duplicates", chunkQuerier, false},
			{"chunk_store_with_duplicates", newDuplicatingBenchQuerier(chunkQuerier), false},
			{"dataobj_store_without_duplicates", dataobjQuerier, true},
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
				b.Run("source="+src.name, func(b *testing.B) {
					if src.requiresStreamOrdered && !m.streamOrdered {
						b.Skipf("source %q is read only under stream-ordered execution", src.name)
					}
					engine := logql.NewEngine(logql.EngineOpts{StreamOrderedExecutionEnabled: m.streamOrdered}, src.q, logql.NoLimits, log.NewNopLogger())

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
									counters.latencyNs.Store(int64(lat.d))
									runQuery(b, engine, params) // untimed warmup: warm index/shipper state
									counters.Reset()
									b.ReportAllocs()
									b.ResetTimer()
									for i := 0; i < b.N; i++ {
										runQuery(b, engine, params)
									}
									b.StopTimer()
									b.ReportMetric(float64(counters.requests.Load())/float64(b.N), "store_reqs/op")
									b.ReportMetric(float64(counters.bytes.Load())/float64(b.N), "store_bytes/op")
								})
							}
						})
					}
				})
			}
		})
	}
}

// benchCounters holds the injected per-request object-storage latency plus request/byte tallies. It
// is shared by the chunk and data-object backends; since only one source runs per sub-benchmark, a
// single instance measures whichever backend is under test.
type benchCounters struct {
	latencyNs atomic.Int64
	requests  atomic.Int64
	bytes     atomic.Int64
}

func (c *benchCounters) sleep() {
	if d := c.latencyNs.Load(); d > 0 {
		time.Sleep(time.Duration(d))
	}
}

// Reset zeroes the request and byte tallies (not the injected latency) before a timed run.
func (c *benchCounters) Reset() {
	c.requests.Store(0)
	c.bytes.Store(0)
}

type benchCountingReadCloser struct {
	inner io.ReadCloser
	c     *benchCounters
}

func (r benchCountingReadCloser) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	r.c.bytes.Add(int64(n))
	return n, err
}
func (r benchCountingReadCloser) Close() error { return r.inner.Close() }

// benchCountingObjectClient counts and delays every chunk read, so the benchmark reports chunk
// object-storage requests and bytes alongside ns/op. Index reads are excluded: they are a fixed
// overhead, not the chunk-fetch cost under test (the data-object backend's metastore is in-memory, so
// only its object reads are counted, keeping the two comparable).
type benchCountingObjectClient struct {
	client.ObjectClient
	c *benchCounters
}

func (o *benchCountingObjectClient) tracked(key string) bool { return !strings.HasPrefix(key, "index") }

func (o *benchCountingObjectClient) GetObject(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if !o.tracked(key) {
		return o.ObjectClient.GetObject(ctx, key)
	}
	o.c.requests.Add(1)
	o.c.sleep()
	rc, sz, err := o.ObjectClient.GetObject(ctx, key)
	if err != nil {
		return rc, sz, err
	}
	return benchCountingReadCloser{rc, o.c}, sz, nil
}

func (o *benchCountingObjectClient) GetObjectRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error) {
	if !o.tracked(key) {
		return o.ObjectClient.GetObjectRange(ctx, key, off, length)
	}
	o.c.requests.Add(1)
	o.c.sleep()
	rc, err := o.ObjectClient.GetObjectRange(ctx, key, off, length)
	if err != nil {
		return rc, err
	}
	return benchCountingReadCloser{rc, o.c}, nil
}

// benchCountingBucket is the data-object counterpart of benchCountingObjectClient: it counts and delays
// every object read so the benchmark reports the same store_reqs/store_bytes metrics.
type benchCountingBucket struct {
	objstore.Bucket
	c *benchCounters
}

func (b *benchCountingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	b.c.requests.Add(1)
	b.c.sleep()
	rc, err := b.Bucket.Get(ctx, name)
	if err != nil {
		return rc, err
	}
	return benchCountingReadCloser{rc, b.c}, nil
}

func (b *benchCountingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	b.c.requests.Add(1)
	b.c.sleep()
	rc, err := b.Bucket.GetRange(ctx, name, off, length)
	if err != nil {
		return rc, err
	}
	return benchCountingReadCloser{rc, b.c}, nil
}

// benchMetastore filters section descriptors by the query's stream matchers, mirroring the postings
// index. It is enough to drive the data-object reader over the built objects.
type benchMetastore struct {
	objects []benchMetaObject
}

type benchMetaObject struct {
	path    string
	streams []streams.Stream
}

func (m *benchMetastore) Sections(_ context.Context, req metastore.SectionsRequest) (metastore.SectionsResponse, error) {
	var out []*metastore.DataobjSectionDescriptor
	for _, o := range m.objects {
		var ids []int64
		for _, s := range o.streams {
			if matchesAll(req.Matchers, s.Labels) {
				ids = append(ids, s.ID)
			}
		}
		if len(ids) > 0 {
			out = append(out, &metastore.DataobjSectionDescriptor{
				SectionKey: metastore.SectionKey{ObjectPath: o.path, SectionIdx: 0},
				StreamIDs:  ids,
			})
		}
	}
	return metastore.SectionsResponse{Sections: out}, nil
}

func (m *benchMetastore) GetIndexes(context.Context, metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	return metastore.GetIndexesResponse{}, nil
}
func (m *benchMetastore) IndexSectionsReader(context.Context, metastore.IndexSectionsReaderRequest) (metastore.IndexSectionsReaderResponse, error) {
	return metastore.IndexSectionsReaderResponse{}, nil
}
func (m *benchMetastore) CollectSections(context.Context, metastore.CollectSectionsRequest) (metastore.CollectSectionsResponse, error) {
	return metastore.CollectSectionsResponse{}, nil
}
func (m *benchMetastore) Labels(context.Context, time.Time, time.Time, ...*labels.Matcher) ([]string, error) {
	return nil, nil
}
func (m *benchMetastore) Values(context.Context, time.Time, time.Time, ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func matchesAll(matchers []*labels.Matcher, lbls labels.Labels) bool {
	for _, m := range matchers {
		if !m.Matches(lbls.Get(m.Name)) {
			return false
		}
	}
	return true
}

// benchStoreConfig builds the filesystem+TSDB store config rooted at dir, with optional per-read
// counting + latency injection.
func benchStoreConfig(dir string, counters *benchCounters) (storage.Config, config.SchemaConfig) {
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
	if counters != nil {
		storeConfig.ObjectClientDecorator = func(oc client.ObjectClient) client.ObjectClient {
			return &benchCountingObjectClient{ObjectClient: oc, c: counters}
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

// openBenchStore opens both backends over the fixture dir, sharing counters: the chunk LokiStore and a
// stream-first data-object reader over <dir>/dataobj.
func openBenchStore(b *testing.B, dir string, counters *benchCounters) (*storage.LokiStore, logql.Querier) {
	b.Helper()
	return openBenchChunkStore(b, dir, counters), openBenchDataObjStore(b, dir, counters)
}

// openBenchChunkStore opens a LokiStore over dir (fixtures already present). No chunk cache, so every
// fetch hits the object store.
func openBenchChunkStore(tb testing.TB, dir string, counters *benchCounters) *storage.LokiStore {
	tb.Helper()
	storeConfig, schemaCfg := benchStoreConfig(dir, counters)

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

// openBenchDataObjStore opens a stream-first data-object reader over the objects written to
// <dir>/dataobj by buildBenchDataObjects, through an instrumented bucket. The metastore is
// reconstructed in-memory from each object's streams section (untimed setup, via the raw bucket).
func openBenchDataObjStore(b *testing.B, dir string, counters *benchCounters) logql.Querier {
	b.Helper()
	ctx := user.InjectOrgID(context.Background(), benchTenant)

	fsBucket, err := filesystem.NewBucket(benchDataObjDir(dir))
	require.NoError(b, err)

	var metaObjects []benchMetaObject
	require.NoError(b, fsBucket.Iter(ctx, "", func(name string) error {
		obj, err := dataobj.FromBucket(ctx, fsBucket, name, 0)
		if err != nil {
			return err
		}
		metaObjects = append(metaObjects, benchMetaObject{path: name, streams: benchStreamsOf(ctx, b, obj)})
		return nil
	}))
	require.NotEmpty(b, metaObjects, "no data objects found in the fixtures; regenerate them")

	bucket := &benchCountingBucket{Bucket: fsBucket, c: counters}
	return querier.NewDataObjSampleStore(nil, bucket, &benchMetastore{objects: metaObjects}, false, log.NewNopLogger(), nil)
}

// benchDataObjDir is the sub-directory of a fixture dir that holds the data objects.
func benchDataObjDir(dir string) string { return filepath.Join(dir, "dataobj") }

func benchStreamsOf(ctx context.Context, b *testing.B, obj *dataobj.Object) []streams.Stream {
	b.Helper()
	var out []streams.Stream
	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		ss, err := streams.Open(ctx, sec)
		require.NoError(b, err)
		r := streams.NewRowReader(ss)
		buf := make([]streams.Stream, 1024)
		require.NoError(b, r.Open(ctx))
		for {
			n, err := r.Read(ctx, buf)
			if err != nil && !errors.Is(err, io.EOF) {
				require.NoError(b, err)
			}
			for i := range buf[:n] {
				out = append(out, streams.Stream{ID: buf[i].ID, Labels: buf[i].Labels})
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
		}
		r.Close()
	}
	return out
}

func newBenchMemChunk() *chunkenc.MemChunk {
	const (
		targetChunkSize = 1024 * 1024
		blockSize       = 256 * 1024
	)
	return chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.Snappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, targetChunkSize)
}

// buildBenchChunks writes the corpus into a filesystem chunk store + TSDB index rooted at dir, using
// the same deterministic streams as the data objects so the two backends answer identically. Each
// stream is encoded into one or more ~1 MB chunks.
func buildBenchChunks(tb testing.TB, dir string) {
	tb.Helper()
	store := openBenchChunkStore(tb, dir, nil) // nil counters: generation is neither counted nor delayed
	rng := newBenchRNG()

	for i := 0; i < benchNumStreams; i++ {
		stream := generateBenchStream(i, rng)
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
		for j := range stream.Entries {
			e := stream.Entries[j]
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

	store.Stop()
}

// buildBenchDataObjects writes the corpus as ~64 MB data objects into <dir>/dataobj, using the same
// deterministic streams as the chunk fixtures so the two backends answer identically.
func buildBenchDataObjects(tb testing.TB, dir string) {
	tb.Helper()
	ctx := user.InjectOrgID(context.Background(), benchTenant)

	bucket, err := filesystem.NewBucket(benchDataObjDir(dir))
	require.NoError(tb, err)

	cfg := logsobj.BuilderConfig{BuilderBaseConfig: logsobj.BuilderBaseConfig{
		TargetPageSize:          2 << 20,
		TargetObjectSize:        benchDataObjTargetSize,
		TargetSectionSize:       benchDataObjTargetSize,
		BufferSize:              4 << 20,
		SectionStripeMergeLimit: 2,
	}}

	newBuilder := func() *logsobj.Builder {
		builder, err := logsobj.NewBuilder(cfg, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(tb, err)
		return builder
	}

	objIdx := 0
	flush := func(builder *logsobj.Builder) {
		obj, closer, err := builder.Flush()
		require.NoError(tb, err)
		reader, err := obj.Reader(ctx)
		require.NoError(tb, err)
		require.NoError(tb, bucket.Upload(ctx, fmt.Sprintf("%04d", objIdx), reader))
		require.NoError(tb, reader.Close())
		require.NoError(tb, closer.Close())
		objIdx++
	}

	rng := newBenchRNG() // same sequence as the chunk fixtures, so windows/data match
	builder := newBuilder()
	for i := 0; i < benchNumStreams; i++ {
		// The builder reports full once its estimated size passes TargetObjectSize; flush it and start
		// a fresh object before appending the stream that would push it further over.
		if builder.IsFull() {
			flush(builder)
			builder = newBuilder()
		}
		require.NoError(tb, builder.Append(benchTenant, generateBenchStream(i, rng), benchStart))
	}
	flush(builder) // the final object is partial: flush whatever the last builder holds
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

	// Both backends over the same deterministic corpus, so a query can be benchmarked against either.
	buildBenchChunks(tb, tmp)
	buildBenchDataObjects(tb, tmp)

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
		entries[j] = logproto.Entry{
			Timestamp: ts,
			Line:      benchLine(i, j),
			StructuredMetadata: push.LabelsAdapter{
				{Name: "trace_id", Value: fmt.Sprintf("trace-%06d-%06d", i, j)}, // unique per line
				{Name: "span_id", Value: fmt.Sprintf("span-%06d-%06d", i, j)},   // unique per line
				{Name: "shard", Value: fmt.Sprintf("shard-%02d", j%50)},         // 50 unique values
			},
		}
	}
	return logproto.Stream{Labels: lbls.String(), Entries: entries}
}

// newBenchRNG returns the deterministic PRNG for fixture generation (window offsets).
func newBenchRNG() *rand.Rand {
	return rand.New(rand.NewSource(fixtureSeed)) //nolint:gosec // determinism, not security
}

// benchFixtureHash is the content-addressed cache key derived from the fixture parameters, including
// the data-object sizing so a change to it regenerates both backends' fixtures.
func benchFixtureHash() string {
	return fmt.Sprintf("v%d-s%d-str%d-lin%d-b%d-do%d",
		fixtureVersion, fixtureSeed, benchNumStreams, benchLinesPerStr, benchLineBytes, benchDataObjTargetSize)
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
