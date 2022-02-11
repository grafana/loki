package tsdb

import (
	"context"
	"testing"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

const (
	testBlockSize  = 256 * 1024
	testTargetSize = 1500 * 1024
)

func mustParseLabels(s string) labels.Labels {
	ls, err := logql.ParseLabels(s)
	if err != nil {
		panic(err)
	}
	return ls
}

func TestIdk(t *testing.T) {
	dir := t.TempDir()
	b := index.NewBuilder()
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
				},
				{
					Checksum: 2,
					MinTime:  5,
					MaxTime:  15,
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
				},
				{
					Checksum: 4,
					MinTime:  40,
					MaxTime:  50,
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
				},
				{
					Checksum: 2,
					MinTime:  5,
					MaxTime:  15,
				},
			},
		},
	}
	for _, s := range cases {
		b.AddSeries(s.labels, s.chunks)
	}

	require.Nil(t, b.Build(context.Background(), dir))

	reader, err := index.NewFileReader(dir)
	require.Nil(t, err)

	p, err := PostingsForMatchers(reader, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
	require.Nil(t, err)

	var (
		chks []index.ChunkMeta
		ls   labels.Labels
	)

	require.True(t, p.Next())
	require.Nil(t, reader.Series(p.At(), &ls, &chks))
	// the second series should be the first returned as it's lexicographically sorted
	// and bazz < foo
	require.Equal(t, cases[1].labels.String(), ls.String())
	require.Equal(t, cases[1].chunks, chks)
	require.True(t, p.Next())
	require.Nil(t, reader.Series(p.At(), &ls, &chks))
	// Now we should encounter the series "added" first.
	require.Equal(t, cases[0].labels.String(), ls.String())
	require.Equal(t, cases[0].chunks, chks)
	require.False(t, p.Next())
}
