// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storetestutil

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/prometheus/prometheus/tsdb/wal"
	"github.com/thanos-io/thanos/pkg/store/hintspb"
	"github.com/thanos-io/thanos/pkg/store/labelpb"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/thanos-io/thanos/pkg/testutil"
)

const (
	// LabelLongSuffix is a label with ~50B in size, to emulate real-world high cardinality.
	LabelLongSuffix = "aaaaaaaaaabbbbbbbbbbccccccccccdddddddddd"
)

func allPostings(t testing.TB, ix tsdb.IndexReader) index.Postings {
	k, v := index.AllPostingsKey()
	p, err := ix.Postings(k, v)
	testutil.Ok(t, err)
	return p
}

const RemoteReadFrameLimit = 1048576

type HeadGenOptions struct {
	TSDBDir                  string
	SamplesPerSeries, Series int

	WithWAL       bool
	PrependLabels labels.Labels
	SkipChunks    bool // Skips chunks in returned slice (not in generated head!).

	Random *rand.Rand
}

// CreateHeadWithSeries returns head filled with given samples and same series returned in separate list for assertion purposes.
// Returned series list has "ext1"="1" prepended. Each series looks as follows:
// {foo=bar,i=000001aaaaaaaaaabbbbbbbbbbccccccccccdddddddddd} <random value> where number indicate sample number from 0.
// Returned series are frame in same way as remote read would frame them.
func CreateHeadWithSeries(t testing.TB, j int, opts HeadGenOptions) (*tsdb.Head, []*storepb.Series) {
	if opts.SamplesPerSeries < 1 || opts.Series < 1 {
		t.Fatal("samples and series has to be 1 or more")
	}

	fmt.Printf("Creating %d %d-sample series in %s\n", opts.Series, opts.SamplesPerSeries, opts.TSDBDir)

	var w *wal.WAL
	var err error
	if opts.WithWAL {
		w, err = wal.New(nil, nil, filepath.Join(opts.TSDBDir, "wal"), true)
		testutil.Ok(t, err)
	} else {
		testutil.Ok(t, os.MkdirAll(filepath.Join(opts.TSDBDir, "wal"), os.ModePerm))
	}

	h, err := tsdb.NewHead(nil, nil, w, tsdb.DefaultBlockDuration, opts.TSDBDir, nil, tsdb.DefaultStripeSize, nil)
	testutil.Ok(t, err)

	app := h.Appender(context.Background())
	for i := 0; i < opts.Series; i++ {
		ts := int64(j*opts.Series*opts.SamplesPerSeries + i*opts.SamplesPerSeries)
		ref, err := app.Add(labels.FromStrings("foo", "bar", "i", fmt.Sprintf("%07d%s", ts, LabelLongSuffix)), ts, opts.Random.Float64())
		testutil.Ok(t, err)

		for is := 1; is < opts.SamplesPerSeries; is++ {
			testutil.Ok(t, app.AddFast(ref, ts+int64(is), opts.Random.Float64()))
		}
	}
	testutil.Ok(t, app.Commit())

	// Use TSDB and get all series for assertion.
	chks, err := h.Chunks()
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, chks.Close()) }()

	ir, err := h.Index()
	testutil.Ok(t, err)
	defer func() { testutil.Ok(t, ir.Close()) }()

	var (
		lset       labels.Labels
		chunkMetas []chunks.Meta
		expected   = make([]*storepb.Series, 0, opts.Series)
	)

	all := allPostings(t, ir)
	for all.Next() {
		testutil.Ok(t, ir.Series(all.At(), &lset, &chunkMetas))
		expected = append(expected, &storepb.Series{Labels: labelpb.ZLabelsFromPromLabels(append(opts.PrependLabels.Copy(), lset...))})

		if opts.SkipChunks {
			continue
		}

		for _, c := range chunkMetas {
			chEnc, err := chks.Chunk(c.Ref)
			testutil.Ok(t, err)

			// Open Chunk.
			if c.MaxTime == math.MaxInt64 {
				c.MaxTime = c.MinTime + int64(chEnc.NumSamples()) - 1
			}

			expected[len(expected)-1].Chunks = append(expected[len(expected)-1].Chunks, storepb.AggrChunk{
				MinTime: c.MinTime,
				MaxTime: c.MaxTime,
				Raw:     &storepb.Chunk{Type: storepb.Chunk_XOR, Data: chEnc.Bytes()},
			})
		}
	}
	testutil.Ok(t, all.Err())
	return h, expected
}

// SeriesServer is test gRPC storeAPI series server.
type SeriesServer struct {
	// This field just exist to pseudo-implement the unused methods of the interface.
	storepb.Store_SeriesServer

	ctx context.Context

	SeriesSet []*storepb.Series
	Warnings  []string
	HintsSet  []*types.Any

	Size int64
}

func NewSeriesServer(ctx context.Context) *SeriesServer {
	return &SeriesServer{ctx: ctx}
}

func (s *SeriesServer) Send(r *storepb.SeriesResponse) error {
	s.Size += int64(r.Size())

	if r.GetWarning() != "" {
		s.Warnings = append(s.Warnings, r.GetWarning())
		return nil
	}

	if r.GetSeries() != nil {
		s.SeriesSet = append(s.SeriesSet, r.GetSeries())
		return nil
	}

	if r.GetHints() != nil {
		s.HintsSet = append(s.HintsSet, r.GetHints())
		return nil
	}
	// Unsupported field, skip.
	return nil
}

func (s *SeriesServer) Context() context.Context {
	return s.ctx
}

func RunSeriesInterestingCases(t testutil.TB, maxSamples, maxSeries int, f func(t testutil.TB, samplesPerSeries, series int)) {
	for _, tc := range []struct {
		samplesPerSeries int
		series           int
	}{
		{
			samplesPerSeries: 1,
			series:           maxSeries,
		},
		{
			samplesPerSeries: maxSamples / (maxSeries / 10),
			series:           maxSeries / 10,
		},
		{
			samplesPerSeries: maxSamples,
			series:           1,
		},
	} {
		if ok := t.Run(fmt.Sprintf("%dSeriesWith%dSamples", tc.series, tc.samplesPerSeries), func(t testutil.TB) {
			f(t, tc.samplesPerSeries, tc.series)
		}); !ok {
			return
		}
		runtime.GC()
	}
}

// SeriesCase represents single test/benchmark case for testing storepb series.
type SeriesCase struct {
	Name string
	Req  *storepb.SeriesRequest

	// Exact expectations are checked only for tests. For benchmarks only length is assured.
	ExpectedSeries   []*storepb.Series
	ExpectedWarnings []string
	ExpectedHints    []hintspb.SeriesResponseHints
}

// TestServerSeries runs tests against given cases.
func TestServerSeries(t testutil.TB, store storepb.StoreServer, cases ...*SeriesCase) {
	for _, c := range cases {
		t.Run(c.Name, func(t testutil.TB) {
			t.ResetTimer()
			for i := 0; i < t.N(); i++ {
				srv := NewSeriesServer(context.Background())
				testutil.Ok(t, store.Series(c.Req, srv))
				testutil.Equals(t, len(c.ExpectedWarnings), len(srv.Warnings), "%v", srv.Warnings)
				testutil.Equals(t, len(c.ExpectedSeries), len(srv.SeriesSet))
				testutil.Equals(t, len(c.ExpectedHints), len(srv.HintsSet))

				if !t.IsBenchmark() {
					if len(c.ExpectedSeries) == 1 {
						// For bucketStoreAPI chunks are not sorted within response. TODO: Investigate: Is this fine?
						sort.Slice(srv.SeriesSet[0].Chunks, func(i, j int) bool {
							return srv.SeriesSet[0].Chunks[i].MinTime < srv.SeriesSet[0].Chunks[j].MinTime
						})
					}

					// Huge responses can produce unreadable diffs - make it more human readable.
					if len(c.ExpectedSeries) > 4 {
						for j := range c.ExpectedSeries {
							testutil.Equals(t, c.ExpectedSeries[j].Labels, srv.SeriesSet[j].Labels, "%v series chunks mismatch", j)
							if len(c.ExpectedSeries[j].Chunks) > 20 {
								testutil.Equals(t, len(c.ExpectedSeries[j].Chunks), len(srv.SeriesSet[j].Chunks), "%v series chunks number mismatch", j)
							}
							testutil.Equals(t, c.ExpectedSeries[j].Chunks, srv.SeriesSet[j].Chunks, "%v series chunks mismatch", j)
						}
					} else {
						testutil.Equals(t, c.ExpectedSeries, srv.SeriesSet)
					}

					var actualHints []hintspb.SeriesResponseHints
					for _, anyHints := range srv.HintsSet {
						hints := hintspb.SeriesResponseHints{}
						testutil.Ok(t, types.UnmarshalAny(anyHints, &hints))
						actualHints = append(actualHints, hints)
					}
					testutil.Equals(t, c.ExpectedHints, actualHints)
				}
			}
		})
	}
}
