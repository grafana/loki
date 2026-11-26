package tsdb

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func mustParseLabels(s string) labels.Labels {
	ls, err := syntax.ParseLabels(s)
	if err != nil {
		panic(err)
	}
	return ls
}

func TestQueryIndex(t *testing.T) {
	dir := t.TempDir()
	b := NewBuilder(index.FormatV3)
	cases := []struct {
		labels labels.Labels
		chunks []index.ChunkMeta
	}{
		{
			labels: mustParseLabels(`{foo="bar"}`),
			chunks: []index.ChunkMeta{
				{
					Checksum: 1,
					MinTime:  1,
					MaxTime:  10,
					KB:       10,
					Entries:  10,
				},
				{
					Checksum: 2,
					MinTime:  5,
					MaxTime:  15,
					KB:       10,
					Entries:  10,
				},
			},
		},
		{
			labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
			chunks: []index.ChunkMeta{
				{
					Checksum: 3,
					MinTime:  20,
					MaxTime:  30,
					KB:       10,
					Entries:  10,
				},
				{
					Checksum: 4,
					MinTime:  40,
					MaxTime:  50,
					KB:       10,
					Entries:  10,
				},
			},
		},
		{
			labels: mustParseLabels(`{unrelated="true"}`),
			chunks: []index.ChunkMeta{
				{
					Checksum: 1,
					MinTime:  1,
					MaxTime:  10,
					KB:       10,
					Entries:  10,
				},
				{
					Checksum: 2,
					MinTime:  5,
					MaxTime:  15,
					KB:       10,
					Entries:  10,
				},
			},
		},
	}
	for _, s := range cases {
		b.AddSeries(s.labels, model.Fingerprint(labels.StableHash(s.labels)), s.chunks)
	}

	dst, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			TS:       time.Now(),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return NewPrefixedIdentifier(id, dir, dir)
	})
	require.Nil(t, err)

	reader, err := index.NewMmapFileReader(dst.Path())
	require.Nil(t, err)

	p, err := PostingsForMatchers(reader, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
	require.Nil(t, err)

	var (
		chks []index.ChunkMeta
		ls   labels.Labels
	)

	require.True(t, p.Next())
	_, err = reader.Series(p.At(), 0, math.MaxInt64, &ls, &chks, nil)
	require.Nil(t, err)
	require.Equal(t, cases[0].labels.String(), ls.String())
	require.Equal(t, cases[0].chunks, chks)
	require.True(t, p.Next())
	_, err = reader.Series(p.At(), 0, math.MaxInt64, &ls, &chks, nil)
	require.Nil(t, err)
	require.Equal(t, cases[1].labels.String(), ls.String())
	require.Equal(t, cases[1].chunks, chks)
	require.False(t, p.Next())

	mint, maxt := reader.Bounds()
	require.Equal(t, int64(1), mint)
	require.Equal(t, int64(50), maxt)
}

// closeTrackingReader wraps every postings list its reader hands out, to
// count which of them get closed. Postings fails on call failOn, if set.
type closeTrackingReader struct {
	IndexReader
	calls, failOn  int
	opened, closed int
}

type closeTrackingPostings struct {
	index.Postings
	r      *closeTrackingReader
	closed bool
}

func (p *closeTrackingPostings) Close() {
	if !p.closed {
		p.closed = true
		p.r.closed++
	}
	p.Postings.Close()
}

func (r *closeTrackingReader) Postings(name string, filter index.FingerprintFilter, values ...string) (index.Postings, error) {
	r.calls++
	if r.calls == r.failOn {
		return nil, errors.New("postings failed")
	}
	p, err := r.IndexReader.Postings(name, filter, values...)
	if err != nil {
		return nil, err
	}
	r.opened++
	return &closeTrackingPostings{Postings: p, r: r}, nil
}

func TestPostingsAreClosedWhenQueriesFinish(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "api"),
		labels.MustNewMatcher(labels.MatchRegexp, "id", "1.*"),
	}
	// readOne stops after the first series, leaving every list part-read.
	readOne := func(p index.Postings) error {
		p.Next()
		return p.Err()
	}
	inner := newPostingsReaderSpy(t).IndexReader

	for _, tc := range []struct {
		name   string
		failOn int
		query  func(r IndexReader) error
	}{
		{name: "sharded forPostings", query: func(r IndexReader) error {
			return (&TSDBIndex{reader: r}).forPostings(context.Background(), index.NewShard(0, 4), 0, 10, matchers, readOne)
		}},
		{name: "cached forPostings", query: func(r IndexReader) error {
			c := newPostingsCache(&postingsTestCache{}, "test", prometheus.NewRegistry(), log.NewNopLogger())
			idx := &TSDBIndex{reader: r, postingsCache: c, postingsID: "file"}
			return idx.forPostings(context.Background(), index.NewShard(0, 4), 0, 10, matchers, readOne)
		}},
		{name: "label values", query: func(r IndexReader) error {
			_, err := labelValuesWithMatchers(r, "id", matchers...)
			return err
		}},
		{name: "label names", query: func(r IndexReader) error {
			_, err := labelNamesWithMatchers(r, matchers...)
			return err
		}},
		{name: "second matcher fails", failOn: 2, query: func(r IndexReader) error {
			_, err := PostingsForMatchers(r, nil, matchers...)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &closeTrackingReader{IndexReader: inner, failOn: tc.failOn}
			err := tc.query(r)
			if tc.failOn > 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Positive(t, r.opened)
			require.Equal(t, r.opened, r.closed, "every postings list must be closed")
		})
	}
}
