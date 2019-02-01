// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tsdb

import (
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/tsdb/chunks"
	"github.com/prometheus/tsdb/index"
	"github.com/prometheus/tsdb/labels"
	"github.com/prometheus/tsdb/testutil"
	"github.com/prometheus/tsdb/wal"
)

func openTestDB(t testing.TB, opts *Options) (db *DB, close func()) {
	tmpdir, err := ioutil.TempDir("", "test")
	testutil.Ok(t, err)

	db, err = Open(tmpdir, nil, nil, opts)
	testutil.Ok(t, err)

	// Do not close the test database by default as it will deadlock on test failures.
	return db, func() { os.RemoveAll(tmpdir) }
}

// query runs a matcher query against the querier and fully expands its data.
func query(t testing.TB, q Querier, matchers ...labels.Matcher) map[string][]sample {
	ss, err := q.Select(matchers...)
	testutil.Ok(t, err)

	result := map[string][]sample{}

	for ss.Next() {
		series := ss.At()

		samples := []sample{}
		it := series.Iterator()
		for it.Next() {
			t, v := it.At()
			samples = append(samples, sample{t: t, v: v})
		}
		testutil.Ok(t, it.Err())

		name := series.Labels().String()
		result[name] = samples
	}
	testutil.Ok(t, ss.Err())

	return result
}

// Ensure that blocks are held in memory in their time order
// and not in ULID order as they are read from the directory.
func TestDB_reloadOrder(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	metas := []*BlockMeta{
		{ULID: ulid.MustNew(100, nil), MinTime: 90, MaxTime: 100},
		{ULID: ulid.MustNew(200, nil), MinTime: 70, MaxTime: 80},
		{ULID: ulid.MustNew(300, nil), MinTime: 100, MaxTime: 110},
	}
	for _, m := range metas {
		bdir := filepath.Join(db.Dir(), m.ULID.String())
		createEmptyBlock(t, bdir, m)
	}

	testutil.Ok(t, db.reload())
	blocks := db.Blocks()
	testutil.Equals(t, 3, len(blocks))
	testutil.Equals(t, *metas[1], blocks[0].Meta())
	testutil.Equals(t, *metas[0], blocks[1].Meta())
	testutil.Equals(t, *metas[2], blocks[2].Meta())
}

func TestDataAvailableOnlyAfterCommit(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()

	_, err := app.Add(labels.FromStrings("foo", "bar"), 0, 0)
	testutil.Ok(t, err)

	querier, err := db.Querier(0, 1)
	testutil.Ok(t, err)
	seriesSet := query(t, querier, labels.NewEqualMatcher("foo", "bar"))

	testutil.Equals(t, map[string][]sample{}, seriesSet)
	testutil.Ok(t, querier.Close())

	err = app.Commit()
	testutil.Ok(t, err)

	querier, err = db.Querier(0, 1)
	testutil.Ok(t, err)
	defer querier.Close()

	seriesSet = query(t, querier, labels.NewEqualMatcher("foo", "bar"))

	testutil.Equals(t, map[string][]sample{`{foo="bar"}`: {{t: 0, v: 0}}}, seriesSet)
}

func TestDataNotAvailableAfterRollback(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()
	_, err := app.Add(labels.FromStrings("foo", "bar"), 0, 0)
	testutil.Ok(t, err)

	err = app.Rollback()
	testutil.Ok(t, err)

	querier, err := db.Querier(0, 1)
	testutil.Ok(t, err)
	defer querier.Close()

	seriesSet := query(t, querier, labels.NewEqualMatcher("foo", "bar"))

	testutil.Equals(t, map[string][]sample{}, seriesSet)
}

func TestDBAppenderAddRef(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app1 := db.Appender()

	ref1, err := app1.Add(labels.FromStrings("a", "b"), 123, 0)
	testutil.Ok(t, err)

	// Reference should already work before commit.
	err = app1.AddFast(ref1, 124, 1)
	testutil.Ok(t, err)

	err = app1.Commit()
	testutil.Ok(t, err)

	app2 := db.Appender()

	// first ref should already work in next transaction.
	err = app2.AddFast(ref1, 125, 0)
	testutil.Ok(t, err)

	ref2, err := app2.Add(labels.FromStrings("a", "b"), 133, 1)
	testutil.Ok(t, err)

	testutil.Assert(t, ref1 == ref2, "")

	// Reference must be valid to add another sample.
	err = app2.AddFast(ref2, 143, 2)
	testutil.Ok(t, err)

	err = app2.AddFast(9999999, 1, 1)
	testutil.Equals(t, ErrNotFound, errors.Cause(err))

	testutil.Ok(t, app2.Commit())

	q, err := db.Querier(0, 200)
	testutil.Ok(t, err)

	res := query(t, q, labels.NewEqualMatcher("a", "b"))

	testutil.Equals(t, map[string][]sample{
		labels.FromStrings("a", "b").String(): {
			{t: 123, v: 0},
			{t: 124, v: 1},
			{t: 125, v: 0},
			{t: 133, v: 1},
			{t: 143, v: 2},
		},
	}, res)

	testutil.Ok(t, q.Close())
}

func TestDeleteSimple(t *testing.T) {
	numSamples := int64(10)

	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()

	smpls := make([]float64, numSamples)
	for i := int64(0); i < numSamples; i++ {
		smpls[i] = rand.Float64()
		app.Add(labels.Labels{{"a", "b"}}, i, smpls[i])
	}

	testutil.Ok(t, app.Commit())
	cases := []struct {
		intervals Intervals
		remaint   []int64
	}{
		{
			intervals: Intervals{{1, 3}, {4, 7}},
			remaint:   []int64{0, 8, 9},
		},
	}

Outer:
	for _, c := range cases {
		// TODO(gouthamve): Reset the tombstones somehow.
		// Delete the ranges.
		for _, r := range c.intervals {
			testutil.Ok(t, db.Delete(r.Mint, r.Maxt, labels.NewEqualMatcher("a", "b")))
		}

		// Compare the result.
		q, err := db.Querier(0, numSamples)
		testutil.Ok(t, err)

		res, err := q.Select(labels.NewEqualMatcher("a", "b"))
		testutil.Ok(t, err)

		expSamples := make([]Sample, 0, len(c.remaint))
		for _, ts := range c.remaint {
			expSamples = append(expSamples, sample{ts, smpls[ts]})
		}

		expss := newMockSeriesSet([]Series{
			newSeries(map[string]string{"a": "b"}, expSamples),
		})

		if len(expSamples) == 0 {
			testutil.Assert(t, res.Next() == false, "")
			continue
		}

		for {
			eok, rok := expss.Next(), res.Next()
			testutil.Equals(t, eok, rok)

			if !eok {
				continue Outer
			}
			sexp := expss.At()
			sres := res.At()

			testutil.Equals(t, sexp.Labels(), sres.Labels())

			smplExp, errExp := expandSeriesIterator(sexp.Iterator())
			smplRes, errRes := expandSeriesIterator(sres.Iterator())

			testutil.Equals(t, errExp, errRes)
			testutil.Equals(t, smplExp, smplRes)
		}
	}
}

func TestAmendDatapointCausesError(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()
	_, err := app.Add(labels.Labels{}, 0, 0)
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	app = db.Appender()
	_, err = app.Add(labels.Labels{}, 0, 1)
	testutil.Equals(t, ErrAmendSample, err)
	testutil.Ok(t, app.Rollback())
}

func TestDuplicateNaNDatapointNoAmendError(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()
	_, err := app.Add(labels.Labels{}, 0, math.NaN())
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	app = db.Appender()
	_, err = app.Add(labels.Labels{}, 0, math.NaN())
	testutil.Ok(t, err)
}

func TestNonDuplicateNaNDatapointsCausesAmendError(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()
	_, err := app.Add(labels.Labels{}, 0, math.Float64frombits(0x7ff0000000000001))
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	app = db.Appender()
	_, err = app.Add(labels.Labels{}, 0, math.Float64frombits(0x7ff0000000000002))
	testutil.Equals(t, ErrAmendSample, err)
}

func TestSkippingInvalidValuesInSameTxn(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	// Append AmendedValue.
	app := db.Appender()
	_, err := app.Add(labels.Labels{{"a", "b"}}, 0, 1)
	testutil.Ok(t, err)
	_, err = app.Add(labels.Labels{{"a", "b"}}, 0, 2)
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	// Make sure the right value is stored.
	q, err := db.Querier(0, 10)
	testutil.Ok(t, err)

	ssMap := query(t, q, labels.NewEqualMatcher("a", "b"))

	testutil.Equals(t, map[string][]sample{
		labels.New(labels.Label{"a", "b"}).String(): {{0, 1}},
	}, ssMap)

	testutil.Ok(t, q.Close())

	// Append Out of Order Value.
	app = db.Appender()
	_, err = app.Add(labels.Labels{{"a", "b"}}, 10, 3)
	testutil.Ok(t, err)
	_, err = app.Add(labels.Labels{{"a", "b"}}, 7, 5)
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	q, err = db.Querier(0, 10)
	testutil.Ok(t, err)

	ssMap = query(t, q, labels.NewEqualMatcher("a", "b"))

	testutil.Equals(t, map[string][]sample{
		labels.New(labels.Label{"a", "b"}).String(): {{0, 1}, {10, 3}},
	}, ssMap)
	testutil.Ok(t, q.Close())
}

func TestDB_Snapshot(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()

	// append data
	app := db.Appender()
	mint := int64(1414141414000)
	for i := 0; i < 1000; i++ {
		_, err := app.Add(labels.FromStrings("foo", "bar"), mint+int64(i), 1.0)
		testutil.Ok(t, err)
	}
	testutil.Ok(t, app.Commit())
	testutil.Ok(t, app.Rollback())

	// create snapshot
	snap, err := ioutil.TempDir("", "snap")
	testutil.Ok(t, err)

	defer os.RemoveAll(snap)
	testutil.Ok(t, db.Snapshot(snap, true))
	testutil.Ok(t, db.Close())

	// reopen DB from snapshot
	db, err = Open(snap, nil, nil, nil)
	testutil.Ok(t, err)
	defer db.Close()

	querier, err := db.Querier(mint, mint+1000)
	testutil.Ok(t, err)
	defer querier.Close()

	// sum values
	seriesSet, err := querier.Select(labels.NewEqualMatcher("foo", "bar"))
	testutil.Ok(t, err)

	sum := 0.0
	for seriesSet.Next() {
		series := seriesSet.At().Iterator()
		for series.Next() {
			_, v := series.At()
			sum += v
		}
		testutil.Ok(t, series.Err())
	}
	testutil.Ok(t, seriesSet.Err())
	testutil.Equals(t, 1000.0, sum)
}

func TestDB_SnapshotWithDelete(t *testing.T) {
	numSamples := int64(10)

	db, close := openTestDB(t, nil)
	defer close()

	app := db.Appender()

	smpls := make([]float64, numSamples)
	for i := int64(0); i < numSamples; i++ {
		smpls[i] = rand.Float64()
		app.Add(labels.Labels{{"a", "b"}}, i, smpls[i])
	}

	testutil.Ok(t, app.Commit())
	cases := []struct {
		intervals Intervals
		remaint   []int64
	}{
		{
			intervals: Intervals{{1, 3}, {4, 7}},
			remaint:   []int64{0, 8, 9},
		},
	}

Outer:
	for _, c := range cases {
		// TODO(gouthamve): Reset the tombstones somehow.
		// Delete the ranges.
		for _, r := range c.intervals {
			testutil.Ok(t, db.Delete(r.Mint, r.Maxt, labels.NewEqualMatcher("a", "b")))
		}

		// create snapshot
		snap, err := ioutil.TempDir("", "snap")
		testutil.Ok(t, err)

		defer os.RemoveAll(snap)
		testutil.Ok(t, db.Snapshot(snap, true))
		testutil.Ok(t, db.Close())

		// reopen DB from snapshot
		db, err = Open(snap, nil, nil, nil)
		testutil.Ok(t, err)
		defer db.Close()

		// Compare the result.
		q, err := db.Querier(0, numSamples)
		testutil.Ok(t, err)
		defer q.Close()

		res, err := q.Select(labels.NewEqualMatcher("a", "b"))
		testutil.Ok(t, err)

		expSamples := make([]Sample, 0, len(c.remaint))
		for _, ts := range c.remaint {
			expSamples = append(expSamples, sample{ts, smpls[ts]})
		}

		expss := newMockSeriesSet([]Series{
			newSeries(map[string]string{"a": "b"}, expSamples),
		})

		if len(expSamples) == 0 {
			testutil.Assert(t, res.Next() == false, "")
			continue
		}

		for {
			eok, rok := expss.Next(), res.Next()
			testutil.Equals(t, eok, rok)

			if !eok {
				continue Outer
			}
			sexp := expss.At()
			sres := res.At()

			testutil.Equals(t, sexp.Labels(), sres.Labels())

			smplExp, errExp := expandSeriesIterator(sexp.Iterator())
			smplRes, errRes := expandSeriesIterator(sres.Iterator())

			testutil.Equals(t, errExp, errRes)
			testutil.Equals(t, smplExp, smplRes)
		}
	}
}

func TestDB_e2e(t *testing.T) {
	const (
		numDatapoints = 1000
		numRanges     = 1000
		timeInterval  = int64(3)
		maxTime       = int64(2 * 1000)
		minTime       = int64(200)
	)
	// Create 8 series with 1000 data-points of different ranges and run queries.
	lbls := [][]labels.Label{
		{
			{"a", "b"},
			{"instance", "localhost:9090"},
			{"job", "prometheus"},
		},
		{
			{"a", "b"},
			{"instance", "127.0.0.1:9090"},
			{"job", "prometheus"},
		},
		{
			{"a", "b"},
			{"instance", "127.0.0.1:9090"},
			{"job", "prom-k8s"},
		},
		{
			{"a", "b"},
			{"instance", "localhost:9090"},
			{"job", "prom-k8s"},
		},
		{
			{"a", "c"},
			{"instance", "localhost:9090"},
			{"job", "prometheus"},
		},
		{
			{"a", "c"},
			{"instance", "127.0.0.1:9090"},
			{"job", "prometheus"},
		},
		{
			{"a", "c"},
			{"instance", "127.0.0.1:9090"},
			{"job", "prom-k8s"},
		},
		{
			{"a", "c"},
			{"instance", "localhost:9090"},
			{"job", "prom-k8s"},
		},
	}

	seriesMap := map[string][]sample{}
	for _, l := range lbls {
		seriesMap[labels.New(l...).String()] = []sample{}
	}

	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()

	for _, l := range lbls {
		lset := labels.New(l...)
		series := []sample{}

		ts := rand.Int63n(300)
		for i := 0; i < numDatapoints; i++ {
			v := rand.Float64()

			series = append(series, sample{ts, v})

			_, err := app.Add(lset, ts, v)
			testutil.Ok(t, err)

			ts += rand.Int63n(timeInterval) + 1
		}

		seriesMap[lset.String()] = series
	}

	testutil.Ok(t, app.Commit())

	// Query each selector on 1000 random time-ranges.
	queries := []struct {
		ms []labels.Matcher
	}{
		{
			ms: []labels.Matcher{labels.NewEqualMatcher("a", "b")},
		},
		{
			ms: []labels.Matcher{
				labels.NewEqualMatcher("a", "b"),
				labels.NewEqualMatcher("job", "prom-k8s"),
			},
		},
		{
			ms: []labels.Matcher{
				labels.NewEqualMatcher("a", "c"),
				labels.NewEqualMatcher("instance", "localhost:9090"),
				labels.NewEqualMatcher("job", "prometheus"),
			},
		},
		// TODO: Add Regexp Matchers.
	}

	for _, qry := range queries {
		matched := labels.Slice{}
		for _, ls := range lbls {
			s := labels.Selector(qry.ms)
			if s.Matches(ls) {
				matched = append(matched, ls)
			}
		}

		sort.Sort(matched)

		for i := 0; i < numRanges; i++ {
			mint := rand.Int63n(300)
			maxt := mint + rand.Int63n(timeInterval*int64(numDatapoints))

			expected := map[string][]sample{}

			// Build the mockSeriesSet.
			for _, m := range matched {
				smpls := boundedSamples(seriesMap[m.String()], mint, maxt)
				if len(smpls) > 0 {
					expected[m.String()] = smpls
				}
			}

			q, err := db.Querier(mint, maxt)
			testutil.Ok(t, err)

			ss, err := q.Select(qry.ms...)
			testutil.Ok(t, err)

			result := map[string][]sample{}

			for ss.Next() {
				x := ss.At()

				smpls, err := expandSeriesIterator(x.Iterator())
				testutil.Ok(t, err)

				if len(smpls) > 0 {
					result[x.Labels().String()] = smpls
				}
			}

			testutil.Ok(t, ss.Err())
			testutil.Equals(t, expected, result)

			q.Close()
		}
	}

	return
}

func TestWALFlushedOnDBClose(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()

	dirDb := db.Dir()

	lbls := labels.Labels{labels.Label{Name: "labelname", Value: "labelvalue"}}

	app := db.Appender()
	_, err := app.Add(lbls, 0, 1)
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	testutil.Ok(t, db.Close())

	db, err = Open(dirDb, nil, nil, nil)
	testutil.Ok(t, err)
	defer db.Close()

	q, err := db.Querier(0, 1)
	testutil.Ok(t, err)

	values, err := q.LabelValues("labelname")
	testutil.Ok(t, err)
	testutil.Equals(t, []string{"labelvalue"}, values)
}

func TestWALSegmentSizeOption(t *testing.T) {
	options := *DefaultOptions
	options.WALSegmentSize = 2 * 32 * 1024
	db, close := openTestDB(t, &options)
	defer close()
	app := db.Appender()
	for i := int64(0); i < 155; i++ {
		_, err := app.Add(labels.Labels{labels.Label{Name: "wal", Value: "size"}}, i, rand.Float64())
		testutil.Ok(t, err)
		testutil.Ok(t, app.Commit())
	}

	dbDir := db.Dir()
	db.Close()
	files, err := ioutil.ReadDir(filepath.Join(dbDir, "wal"))
	testutil.Assert(t, len(files) > 1, "current WALSegmentSize should result in more than a single WAL file.")
	testutil.Ok(t, err)
	for i, f := range files {
		if len(files)-1 != i {
			testutil.Equals(t, int64(options.WALSegmentSize), f.Size(), "WAL file size doesn't match WALSegmentSize option, filename: %v", f.Name())
			continue
		}
		testutil.Assert(t, int64(options.WALSegmentSize) > f.Size(), "last WAL file size is not smaller than the WALSegmentSize option, filename: %v", f.Name())
	}
}

func TestTombstoneClean(t *testing.T) {
	numSamples := int64(10)

	db, close := openTestDB(t, nil)
	defer close()

	app := db.Appender()

	smpls := make([]float64, numSamples)
	for i := int64(0); i < numSamples; i++ {
		smpls[i] = rand.Float64()
		app.Add(labels.Labels{{"a", "b"}}, i, smpls[i])
	}

	testutil.Ok(t, app.Commit())
	cases := []struct {
		intervals Intervals
		remaint   []int64
	}{
		{
			intervals: Intervals{{1, 3}, {4, 7}},
			remaint:   []int64{0, 8, 9},
		},
	}

	for _, c := range cases {
		// Delete the ranges.

		// create snapshot
		snap, err := ioutil.TempDir("", "snap")
		testutil.Ok(t, err)

		defer os.RemoveAll(snap)
		testutil.Ok(t, db.Snapshot(snap, true))
		testutil.Ok(t, db.Close())

		// reopen DB from snapshot
		db, err = Open(snap, nil, nil, nil)
		testutil.Ok(t, err)
		defer db.Close()

		for _, r := range c.intervals {
			testutil.Ok(t, db.Delete(r.Mint, r.Maxt, labels.NewEqualMatcher("a", "b")))
		}

		// All of the setup for THIS line.
		testutil.Ok(t, db.CleanTombstones())

		// Compare the result.
		q, err := db.Querier(0, numSamples)
		testutil.Ok(t, err)
		defer q.Close()

		res, err := q.Select(labels.NewEqualMatcher("a", "b"))
		testutil.Ok(t, err)

		expSamples := make([]Sample, 0, len(c.remaint))
		for _, ts := range c.remaint {
			expSamples = append(expSamples, sample{ts, smpls[ts]})
		}

		expss := newMockSeriesSet([]Series{
			newSeries(map[string]string{"a": "b"}, expSamples),
		})

		if len(expSamples) == 0 {
			testutil.Assert(t, res.Next() == false, "")
			continue
		}

		for {
			eok, rok := expss.Next(), res.Next()
			testutil.Equals(t, eok, rok)

			if !eok {
				break
			}
			sexp := expss.At()
			sres := res.At()

			testutil.Equals(t, sexp.Labels(), sres.Labels())

			smplExp, errExp := expandSeriesIterator(sexp.Iterator())
			smplRes, errRes := expandSeriesIterator(sres.Iterator())

			testutil.Equals(t, errExp, errRes)
			testutil.Equals(t, smplExp, smplRes)
		}

		for _, b := range db.Blocks() {
			testutil.Equals(t, newMemTombstones(), b.tombstones)
		}
	}
}

// TestTombstoneCleanFail tests that a failing TombstoneClean doesn't leave any blocks behind.
// When TombstoneClean errors the original block that should be rebuilt doesn't get deleted so
// if TombstoneClean leaves any blocks behind these will overlap.
func TestTombstoneCleanFail(t *testing.T) {

	db, close := openTestDB(t, nil)
	defer close()

	var expectedBlockDirs []string

	// Create some empty blocks pending for compaction.
	// totalBlocks should be >=2 so we have enough blocks to trigger compaction failure.
	totalBlocks := 2
	for i := 0; i < totalBlocks; i++ {
		entropy := rand.New(rand.NewSource(time.Now().UnixNano()))
		uid := ulid.MustNew(ulid.Now(), entropy)
		meta := &BlockMeta{
			Version: 2,
			ULID:    uid,
		}
		blockDir := filepath.Join(db.Dir(), uid.String())
		block := createEmptyBlock(t, blockDir, meta)

		// Add some some fake tombstones to trigger the compaction.
		tomb := newMemTombstones()
		tomb.addInterval(0, Interval{0, 1})
		block.tombstones = tomb

		db.blocks = append(db.blocks, block)
		expectedBlockDirs = append(expectedBlockDirs, blockDir)
	}

	// Initialize the mockCompactorFailing with a room for a single compaction iteration.
	// mockCompactorFailing will fail on the second iteration so we can check if the cleanup works as expected.
	db.compactor = &mockCompactorFailing{
		t:      t,
		blocks: db.blocks,
		max:    totalBlocks + 1,
	}

	// The compactor should trigger a failure here.
	testutil.NotOk(t, db.CleanTombstones())

	// Now check that the CleanTombstones didn't leave any blocks behind after a failure.
	actualBlockDirs, err := blockDirs(db.dir)
	testutil.Ok(t, err)
	testutil.Equals(t, expectedBlockDirs, actualBlockDirs)
}

// mockCompactorFailing creates a new empty block on every write and fails when reached the max allowed total.
type mockCompactorFailing struct {
	t      *testing.T
	blocks []*Block
	max    int
}

func (*mockCompactorFailing) Plan(dir string) ([]string, error) {
	return nil, nil
}
func (c *mockCompactorFailing) Write(dest string, b BlockReader, mint, maxt int64, parent *BlockMeta) (ulid.ULID, error) {
	if len(c.blocks) >= c.max {
		return ulid.ULID{}, fmt.Errorf("the compactor already did the maximum allowed blocks so it is time to fail")
	}

	entropy := rand.New(rand.NewSource(time.Now().UnixNano()))
	uid := ulid.MustNew(ulid.Now(), entropy)
	meta := &BlockMeta{
		Version: 2,
		ULID:    uid,
	}

	block := createEmptyBlock(c.t, filepath.Join(dest, meta.ULID.String()), meta)
	testutil.Ok(c.t, block.Close()) // Close block as we won't be using anywhere.
	c.blocks = append(c.blocks, block)

	// Now check that all expected blocks are actually persisted on disk.
	// This way we make sure that the we have some blocks that are supposed to be removed.
	var expectedBlocks []string
	for _, b := range c.blocks {
		expectedBlocks = append(expectedBlocks, filepath.Join(dest, b.Meta().ULID.String()))
	}
	actualBlockDirs, err := blockDirs(dest)
	testutil.Ok(c.t, err)

	testutil.Equals(c.t, expectedBlocks, actualBlockDirs)

	return block.Meta().ULID, nil
}

func (*mockCompactorFailing) Compact(dest string, dirs []string, open []*Block) (ulid.ULID, error) {
	return ulid.ULID{}, nil

}

func TestDB_Retention(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()

	lbls := labels.Labels{labels.Label{Name: "labelname", Value: "labelvalue"}}

	app := db.Appender()
	_, err := app.Add(lbls, 0, 1)
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	// create snapshot to make it create a block.
	// TODO(gouthamve): Add a method to compact headblock.
	snap, err := ioutil.TempDir("", "snap")
	testutil.Ok(t, err)

	defer os.RemoveAll(snap)
	testutil.Ok(t, db.Snapshot(snap, true))
	testutil.Ok(t, db.Close())

	// reopen DB from snapshot
	db, err = Open(snap, nil, nil, nil)
	testutil.Ok(t, err)

	testutil.Equals(t, 1, len(db.blocks))

	app = db.Appender()
	_, err = app.Add(lbls, 100, 1)
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())

	// Snapshot again to create another block.
	snap, err = ioutil.TempDir("", "snap")
	testutil.Ok(t, err)
	defer os.RemoveAll(snap)

	testutil.Ok(t, db.Snapshot(snap, true))
	testutil.Ok(t, db.Close())

	// reopen DB from snapshot
	db, err = Open(snap, nil, nil, &Options{
		RetentionDuration: 10,
		BlockRanges:       []int64{50},
	})
	testutil.Ok(t, err)
	defer db.Close()

	testutil.Equals(t, 2, len(db.blocks))

	// Reload blocks, which should drop blocks beyond the retention boundary.
	testutil.Ok(t, db.reload())
	testutil.Equals(t, 1, len(db.blocks))
	testutil.Equals(t, int64(100), db.blocks[0].meta.MaxTime) // To verify its the right block.
}

func TestNotMatcherSelectsLabelsUnsetSeries(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()

	labelpairs := []labels.Labels{
		labels.FromStrings("a", "abcd", "b", "abcde"),
		labels.FromStrings("labelname", "labelvalue"),
	}

	app := db.Appender()
	for _, lbls := range labelpairs {
		_, err := app.Add(lbls, 0, 1)
		testutil.Ok(t, err)
	}
	testutil.Ok(t, app.Commit())

	cases := []struct {
		selector labels.Selector
		series   []labels.Labels
	}{{
		selector: labels.Selector{
			labels.Not(labels.NewEqualMatcher("lname", "lvalue")),
		},
		series: labelpairs,
	}, {
		selector: labels.Selector{
			labels.NewEqualMatcher("a", "abcd"),
			labels.Not(labels.NewEqualMatcher("b", "abcde")),
		},
		series: []labels.Labels{},
	}, {
		selector: labels.Selector{
			labels.NewEqualMatcher("a", "abcd"),
			labels.Not(labels.NewEqualMatcher("b", "abc")),
		},
		series: []labels.Labels{labelpairs[0]},
	}, {
		selector: labels.Selector{
			labels.Not(labels.NewMustRegexpMatcher("a", "abd.*")),
		},
		series: labelpairs,
	}, {
		selector: labels.Selector{
			labels.Not(labels.NewMustRegexpMatcher("a", "abc.*")),
		},
		series: labelpairs[1:],
	}, {
		selector: labels.Selector{
			labels.Not(labels.NewMustRegexpMatcher("c", "abd.*")),
		},
		series: labelpairs,
	}, {
		selector: labels.Selector{
			labels.Not(labels.NewMustRegexpMatcher("labelname", "labelvalue")),
		},
		series: labelpairs[:1],
	}}

	q, err := db.Querier(0, 10)
	testutil.Ok(t, err)
	defer q.Close()

	for _, c := range cases {
		ss, err := q.Select(c.selector...)
		testutil.Ok(t, err)

		lres, err := expandSeriesSet(ss)
		testutil.Ok(t, err)

		testutil.Equals(t, c.series, lres)
	}
}

func expandSeriesSet(ss SeriesSet) ([]labels.Labels, error) {
	result := []labels.Labels{}
	for ss.Next() {
		result = append(result, ss.At().Labels())
	}

	return result, ss.Err()
}

func TestOverlappingBlocksDetectsAllOverlaps(t *testing.T) {
	// Create 10 blocks that does not overlap (0-10, 10-20, ..., 100-110) but in reverse order to ensure our algorithm
	// will handle that.
	var metas = make([]BlockMeta, 11)
	for i := 10; i >= 0; i-- {
		metas[i] = BlockMeta{MinTime: int64(i * 10), MaxTime: int64((i + 1) * 10)}
	}

	testutil.Assert(t, len(OverlappingBlocks(metas)) == 0, "we found unexpected overlaps")

	// Add overlapping blocks. We've to establish order again since we aren't interested
	// in trivial overlaps caused by unorderedness.
	add := func(ms ...BlockMeta) []BlockMeta {
		repl := append(append([]BlockMeta{}, metas...), ms...)
		sort.Slice(repl, func(i, j int) bool {
			return repl[i].MinTime < repl[j].MinTime
		})
		return repl
	}

	// o1 overlaps with 10-20.
	o1 := BlockMeta{MinTime: 15, MaxTime: 17}
	testutil.Equals(t, Overlaps{
		{Min: 15, Max: 17}: {metas[1], o1},
	}, OverlappingBlocks(add(o1)))

	// o2 overlaps with 20-30 and 30-40.
	o2 := BlockMeta{MinTime: 21, MaxTime: 31}
	testutil.Equals(t, Overlaps{
		{Min: 21, Max: 30}: {metas[2], o2},
		{Min: 30, Max: 31}: {o2, metas[3]},
	}, OverlappingBlocks(add(o2)))

	// o3a and o3b overlaps with 30-40 and each other.
	o3a := BlockMeta{MinTime: 33, MaxTime: 39}
	o3b := BlockMeta{MinTime: 34, MaxTime: 36}
	testutil.Equals(t, Overlaps{
		{Min: 34, Max: 36}: {metas[3], o3a, o3b},
	}, OverlappingBlocks(add(o3a, o3b)))

	// o4 is 1:1 overlap with 50-60.
	o4 := BlockMeta{MinTime: 50, MaxTime: 60}
	testutil.Equals(t, Overlaps{
		{Min: 50, Max: 60}: {metas[5], o4},
	}, OverlappingBlocks(add(o4)))

	// o5 overlaps with 60-70, 70-80 and 80-90.
	o5 := BlockMeta{MinTime: 61, MaxTime: 85}
	testutil.Equals(t, Overlaps{
		{Min: 61, Max: 70}: {metas[6], o5},
		{Min: 70, Max: 80}: {o5, metas[7]},
		{Min: 80, Max: 85}: {o5, metas[8]},
	}, OverlappingBlocks(add(o5)))

	// o6a overlaps with 90-100, 100-110 and o6b, o6b overlaps with 90-100 and o6a.
	o6a := BlockMeta{MinTime: 92, MaxTime: 105}
	o6b := BlockMeta{MinTime: 94, MaxTime: 99}
	testutil.Equals(t, Overlaps{
		{Min: 94, Max: 99}:   {metas[9], o6a, o6b},
		{Min: 100, Max: 105}: {o6a, metas[10]},
	}, OverlappingBlocks(add(o6a, o6b)))

	// All together.
	testutil.Equals(t, Overlaps{
		{Min: 15, Max: 17}: {metas[1], o1},
		{Min: 21, Max: 30}: {metas[2], o2}, {Min: 30, Max: 31}: {o2, metas[3]},
		{Min: 34, Max: 36}: {metas[3], o3a, o3b},
		{Min: 50, Max: 60}: {metas[5], o4},
		{Min: 61, Max: 70}: {metas[6], o5}, {Min: 70, Max: 80}: {o5, metas[7]}, {Min: 80, Max: 85}: {o5, metas[8]},
		{Min: 94, Max: 99}: {metas[9], o6a, o6b}, {Min: 100, Max: 105}: {o6a, metas[10]},
	}, OverlappingBlocks(add(o1, o2, o3a, o3b, o4, o5, o6a, o6b)))

	// Additional case.
	var nc1 []BlockMeta
	nc1 = append(nc1, BlockMeta{MinTime: 1, MaxTime: 5})
	nc1 = append(nc1, BlockMeta{MinTime: 2, MaxTime: 3})
	nc1 = append(nc1, BlockMeta{MinTime: 2, MaxTime: 3})
	nc1 = append(nc1, BlockMeta{MinTime: 2, MaxTime: 3})
	nc1 = append(nc1, BlockMeta{MinTime: 2, MaxTime: 3})
	nc1 = append(nc1, BlockMeta{MinTime: 2, MaxTime: 6})
	nc1 = append(nc1, BlockMeta{MinTime: 3, MaxTime: 5})
	nc1 = append(nc1, BlockMeta{MinTime: 5, MaxTime: 7})
	nc1 = append(nc1, BlockMeta{MinTime: 7, MaxTime: 10})
	nc1 = append(nc1, BlockMeta{MinTime: 8, MaxTime: 9})
	testutil.Equals(t, Overlaps{
		{Min: 2, Max: 3}: {nc1[0], nc1[1], nc1[2], nc1[3], nc1[4], nc1[5]}, // 1-5, 2-3, 2-3, 2-3, 2-3, 2,6
		{Min: 3, Max: 5}: {nc1[0], nc1[5], nc1[6]},                         // 1-5, 2-6, 3-5
		{Min: 5, Max: 6}: {nc1[5], nc1[7]},                                 // 2-6, 5-7
		{Min: 8, Max: 9}: {nc1[8], nc1[9]},                                 // 7-10, 8-9
	}, OverlappingBlocks(nc1))
}

// Regression test for https://github.com/prometheus/tsdb/issues/347
func TestChunkAtBlockBoundary(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()

	blockRange := DefaultOptions.BlockRanges[0]
	label := labels.FromStrings("foo", "bar")

	for i := int64(0); i < 3; i++ {
		_, err := app.Add(label, i*blockRange, 0)
		testutil.Ok(t, err)
		_, err = app.Add(label, i*blockRange+1000, 0)
		testutil.Ok(t, err)
	}

	err := app.Commit()
	testutil.Ok(t, err)

	err = db.compact()
	testutil.Ok(t, err)

	for _, block := range db.Blocks() {
		r, err := block.Index()
		testutil.Ok(t, err)
		defer r.Close()

		meta := block.Meta()

		p, err := r.Postings(index.AllPostingsKey())
		testutil.Ok(t, err)

		var (
			lset labels.Labels
			chks []chunks.Meta
		)

		chunkCount := 0

		for p.Next() {
			err = r.Series(p.At(), &lset, &chks)
			testutil.Ok(t, err)
			for _, c := range chks {
				testutil.Assert(t, meta.MinTime <= c.MinTime && c.MaxTime <= meta.MaxTime,
					"chunk spans beyond block boundaries: [block.MinTime=%d, block.MaxTime=%d]; [chunk.MinTime=%d, chunk.MaxTime=%d]",
					meta.MinTime, meta.MaxTime, c.MinTime, c.MaxTime)
				chunkCount++
			}
		}
		testutil.Assert(t, chunkCount == 1, "expected 1 chunk in block %s, got %d", meta.ULID, chunkCount)
	}
}

func TestQuerierWithBoundaryChunks(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	app := db.Appender()

	blockRange := DefaultOptions.BlockRanges[0]
	label := labels.FromStrings("foo", "bar")

	for i := int64(0); i < 5; i++ {
		_, err := app.Add(label, i*blockRange, 0)
		testutil.Ok(t, err)
	}

	err := app.Commit()
	testutil.Ok(t, err)

	err = db.compact()
	testutil.Ok(t, err)

	testutil.Assert(t, len(db.blocks) >= 3, "invalid test, less than three blocks in DB")

	q, err := db.Querier(blockRange, 2*blockRange)
	testutil.Ok(t, err)
	defer q.Close()

	// The requested interval covers 2 blocks, so the querier should contain 2 blocks.
	count := len(q.(*querier).blocks)
	testutil.Assert(t, count == 2, "expected 2 blocks in querier, got %d", count)
}

// TestInitializeHeadTimestamp ensures that the h.minTime is set properly.
// 	- no blocks no WAL: set to the time of the first  appended sample
// 	- no blocks with WAL: set to the smallest sample from the WAL
//	- with blocks no WAL: set to the last block maxT
// 	- with blocks with WAL: same as above
func TestInitializeHeadTimestamp(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		dir, err := ioutil.TempDir("", "test_head_init")
		testutil.Ok(t, err)
		defer os.RemoveAll(dir)

		db, err := Open(dir, nil, nil, nil)
		testutil.Ok(t, err)

		// Should be set to init values if no WAL or blocks exist so far.
		testutil.Equals(t, int64(math.MaxInt64), db.head.MinTime())
		testutil.Equals(t, int64(math.MinInt64), db.head.MaxTime())

		// First added sample initializes the writable range.
		app := db.Appender()
		_, err = app.Add(labels.FromStrings("a", "b"), 1000, 1)
		testutil.Ok(t, err)

		testutil.Equals(t, int64(1000), db.head.MinTime())
		testutil.Equals(t, int64(1000), db.head.MaxTime())
	})
	t.Run("wal-only", func(t *testing.T) {
		dir, err := ioutil.TempDir("", "test_head_init")
		testutil.Ok(t, err)
		defer os.RemoveAll(dir)

		testutil.Ok(t, os.MkdirAll(path.Join(dir, "wal"), 0777))
		w, err := wal.New(nil, nil, path.Join(dir, "wal"))
		testutil.Ok(t, err)

		var enc RecordEncoder
		err = w.Log(
			enc.Series([]RefSeries{
				{Ref: 123, Labels: labels.FromStrings("a", "1")},
				{Ref: 124, Labels: labels.FromStrings("a", "2")},
			}, nil),
			enc.Samples([]RefSample{
				{Ref: 123, T: 5000, V: 1},
				{Ref: 124, T: 15000, V: 1},
			}, nil),
		)
		testutil.Ok(t, err)
		testutil.Ok(t, w.Close())

		db, err := Open(dir, nil, nil, nil)
		testutil.Ok(t, err)

		testutil.Equals(t, int64(5000), db.head.MinTime())
		testutil.Equals(t, int64(15000), db.head.MaxTime())
	})
	t.Run("existing-block", func(t *testing.T) {
		dir, err := ioutil.TempDir("", "test_head_init")
		testutil.Ok(t, err)
		defer os.RemoveAll(dir)

		id := ulid.MustNew(2000, nil)
		createEmptyBlock(t, path.Join(dir, id.String()), &BlockMeta{
			ULID:    id,
			MinTime: 1000,
			MaxTime: 2000,
		})

		db, err := Open(dir, nil, nil, nil)
		testutil.Ok(t, err)

		testutil.Equals(t, int64(2000), db.head.MinTime())
		testutil.Equals(t, int64(2000), db.head.MaxTime())
	})
	t.Run("existing-block-and-wal", func(t *testing.T) {
		dir, err := ioutil.TempDir("", "test_head_init")
		testutil.Ok(t, err)
		defer os.RemoveAll(dir)

		id := ulid.MustNew(2000, nil)
		createEmptyBlock(t, path.Join(dir, id.String()), &BlockMeta{
			ULID:    id,
			MinTime: 1000,
			MaxTime: 6000,
		})

		testutil.Ok(t, os.MkdirAll(path.Join(dir, "wal"), 0777))
		w, err := wal.New(nil, nil, path.Join(dir, "wal"))
		testutil.Ok(t, err)

		var enc RecordEncoder
		err = w.Log(
			enc.Series([]RefSeries{
				{Ref: 123, Labels: labels.FromStrings("a", "1")},
				{Ref: 124, Labels: labels.FromStrings("a", "2")},
			}, nil),
			enc.Samples([]RefSample{
				{Ref: 123, T: 5000, V: 1},
				{Ref: 124, T: 15000, V: 1},
			}, nil),
		)
		testutil.Ok(t, err)
		testutil.Ok(t, w.Close())

		r := prometheus.NewRegistry()

		db, err := Open(dir, nil, r, nil)
		testutil.Ok(t, err)

		testutil.Equals(t, int64(6000), db.head.MinTime())
		testutil.Equals(t, int64(15000), db.head.MaxTime())
		// Check that old series has been GCed.
		testutil.Equals(t, 1.0, prom_testutil.ToFloat64(db.head.metrics.series))
	})
}

func TestDB_LabelNames(t *testing.T) {
	tests := []struct {
		// Add 'sampleLabels1' -> Test Head -> Compact -> Test Disk ->
		// -> Add 'sampleLabels2' -> Test Head+Disk

		sampleLabels1 [][2]string // For checking head and disk separately.
		// To test Head+Disk, sampleLabels2 should have
		// at least 1 unique label name which is not in sampleLabels1.
		sampleLabels2 [][2]string // // For checking head and disk together.
		exp1          []string    // after adding sampleLabels1.
		exp2          []string    // after adding sampleLabels1 and sampleLabels2.
	}{
		{
			sampleLabels1: [][2]string{
				[2]string{"name1", ""},
				[2]string{"name3", ""},
				[2]string{"name2", ""},
			},
			sampleLabels2: [][2]string{
				[2]string{"name4", ""},
				[2]string{"name1", ""},
			},
			exp1: []string{"name1", "name2", "name3"},
			exp2: []string{"name1", "name2", "name3", "name4"},
		},
		{
			sampleLabels1: [][2]string{
				[2]string{"name2", ""},
				[2]string{"name1", ""},
				[2]string{"name2", ""},
			},
			sampleLabels2: [][2]string{
				[2]string{"name6", ""},
				[2]string{"name0", ""},
			},
			exp1: []string{"name1", "name2"},
			exp2: []string{"name0", "name1", "name2", "name6"},
		},
	}

	blockRange := DefaultOptions.BlockRanges[0]
	// Appends samples into the database.
	appendSamples := func(db *DB, mint, maxt int64, sampleLabels [][2]string) {
		t.Helper()
		app := db.Appender()
		for i := mint; i <= maxt; i++ {
			for _, tuple := range sampleLabels {
				label := labels.FromStrings(tuple[0], tuple[1])
				_, err := app.Add(label, i*blockRange, 0)
				testutil.Ok(t, err)
			}
		}
		err := app.Commit()
		testutil.Ok(t, err)
	}
	for _, tst := range tests {
		db, close := openTestDB(t, nil)
		defer close()
		defer db.Close()

		appendSamples(db, 0, 4, tst.sampleLabels1)

		// Testing head.
		headIndexr, err := db.head.Index()
		testutil.Ok(t, err)
		labelNames, err := headIndexr.LabelNames()
		testutil.Ok(t, err)
		testutil.Equals(t, tst.exp1, labelNames)
		testutil.Ok(t, headIndexr.Close())

		// Testing disk.
		err = db.compact()
		testutil.Ok(t, err)
		// All blocks have same label names, hence check them individually.
		// No need to aggregrate and check.
		for _, b := range db.Blocks() {
			blockIndexr, err := b.Index()
			testutil.Ok(t, err)
			labelNames, err = blockIndexr.LabelNames()
			testutil.Ok(t, err)
			testutil.Equals(t, tst.exp1, labelNames)
			testutil.Ok(t, blockIndexr.Close())
		}

		// Addings more samples to head with new label names
		// so that we can test (head+disk).LabelNames() (the union).
		appendSamples(db, 5, 9, tst.sampleLabels2)

		// Testing DB (union).
		q, err := db.Querier(math.MinInt64, math.MaxInt64)
		testutil.Ok(t, err)
		labelNames, err = q.LabelNames()
		testutil.Ok(t, err)
		testutil.Ok(t, q.Close())
		testutil.Equals(t, tst.exp2, labelNames)
	}
}

func TestCorrectNumTombstones(t *testing.T) {
	db, close := openTestDB(t, nil)
	defer close()
	defer db.Close()

	blockRange := DefaultOptions.BlockRanges[0]
	label := labels.FromStrings("foo", "bar")

	app := db.Appender()
	for i := int64(0); i < 3; i++ {
		for j := int64(0); j < 15; j++ {
			_, err := app.Add(label, i*blockRange+j, 0)
			testutil.Ok(t, err)
		}
	}
	testutil.Ok(t, app.Commit())

	err := db.compact()
	testutil.Ok(t, err)
	testutil.Equals(t, 1, len(db.blocks))

	testutil.Ok(t, db.Delete(0, 1, labels.NewEqualMatcher("foo", "bar")))
	testutil.Equals(t, uint64(1), db.blocks[0].meta.Stats.NumTombstones)

	// {0, 1} and {2, 3} are merged to form 1 tombstone.
	testutil.Ok(t, db.Delete(2, 3, labels.NewEqualMatcher("foo", "bar")))
	testutil.Equals(t, uint64(1), db.blocks[0].meta.Stats.NumTombstones)

	testutil.Ok(t, db.Delete(5, 6, labels.NewEqualMatcher("foo", "bar")))
	testutil.Equals(t, uint64(2), db.blocks[0].meta.Stats.NumTombstones)

	testutil.Ok(t, db.Delete(9, 11, labels.NewEqualMatcher("foo", "bar")))
	testutil.Equals(t, uint64(3), db.blocks[0].meta.Stats.NumTombstones)
}

// TestBlockRanges checks the following use cases:
//  - No samples can be added with timestamps lower than the last block maxt.
//  - The compactor doesn't create overlaping blocks
// even when the last blocks is not within the default boundaries.
//	- Lower bondary is based on the smallest sample in the head and
// upper boundary is rounded to the configured block range.
//
// This ensures that a snapshot that includes the head and creates a block with a custom time range
// will not overlap with the first block created by the next compaction.
func TestBlockRanges(t *testing.T) {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))

	dir, err := ioutil.TempDir("", "test_storage")
	if err != nil {
		t.Fatalf("Opening test dir failed: %s", err)
	}

	rangeToTriggercompaction := DefaultOptions.BlockRanges[0]/2*3 + 1

	// Test that the compactor doesn't create overlapping blocks
	// when a non standard block already exists.
	firstBlockMaxT := int64(3)
	createPopulatedBlock(t, dir, 1, 0, firstBlockMaxT)
	db, err := Open(dir, logger, nil, DefaultOptions)
	if err != nil {
		t.Fatalf("Opening test storage failed: %s", err)
	}
	defer func() {
		os.RemoveAll(dir)
	}()
	app := db.Appender()
	lbl := labels.Labels{{"a", "b"}}
	_, err = app.Add(lbl, firstBlockMaxT-1, rand.Float64())
	if err == nil {
		t.Fatalf("appending a sample with a timestamp covered by a previous block shouldn't be possible")
	}
	_, err = app.Add(lbl, firstBlockMaxT+1, rand.Float64())
	testutil.Ok(t, err)
	_, err = app.Add(lbl, firstBlockMaxT+2, rand.Float64())
	testutil.Ok(t, err)
	secondBlockMaxt := firstBlockMaxT + rangeToTriggercompaction
	_, err = app.Add(lbl, secondBlockMaxt, rand.Float64()) // Add samples to trigger a new compaction

	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())
	for x := 0; x < 100; x++ {
		if len(db.Blocks()) == 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	testutil.Equals(t, 2, len(db.Blocks()), "no new block created after the set timeout")

	if db.Blocks()[0].Meta().MaxTime > db.Blocks()[1].Meta().MinTime {
		t.Fatalf("new block overlaps  old:%v,new:%v", db.Blocks()[0].Meta(), db.Blocks()[1].Meta())
	}

	// Test that wal records are skipped when an existing block covers the same time ranges
	// and compaction doesn't create an overlapping block.
	db.DisableCompactions()
	_, err = app.Add(lbl, secondBlockMaxt+1, rand.Float64())
	testutil.Ok(t, err)
	_, err = app.Add(lbl, secondBlockMaxt+2, rand.Float64())
	testutil.Ok(t, err)
	_, err = app.Add(lbl, secondBlockMaxt+3, rand.Float64())
	testutil.Ok(t, err)
	_, err = app.Add(lbl, secondBlockMaxt+4, rand.Float64())
	testutil.Ok(t, app.Commit())
	testutil.Ok(t, db.Close())

	thirdBlockMaxt := secondBlockMaxt + 2
	createPopulatedBlock(t, dir, 1, secondBlockMaxt+1, thirdBlockMaxt)

	db, err = Open(dir, logger, nil, DefaultOptions)
	if err != nil {
		t.Fatalf("Opening test storage failed: %s", err)
	}
	defer db.Close()
	testutil.Equals(t, 3, len(db.Blocks()), "db doesn't include expected number of blocks")
	testutil.Equals(t, db.Blocks()[2].Meta().MaxTime, thirdBlockMaxt, "unexpected maxt of the last block")

	app = db.Appender()
	_, err = app.Add(lbl, thirdBlockMaxt+rangeToTriggercompaction, rand.Float64()) // Trigger a compaction
	testutil.Ok(t, err)
	testutil.Ok(t, app.Commit())
	for x := 0; x < 100; x++ {
		if len(db.Blocks()) == 4 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	testutil.Equals(t, 4, len(db.Blocks()), "no new block created after the set timeout")

	if db.Blocks()[2].Meta().MaxTime > db.Blocks()[3].Meta().MinTime {
		t.Fatalf("new block overlaps  old:%v,new:%v", db.Blocks()[2].Meta(), db.Blocks()[3].Meta())
	}
}
