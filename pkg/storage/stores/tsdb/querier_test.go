package tsdb

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
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
	b := NewBuilder()
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
		b.AddSeries(s.labels, model.Fingerprint(s.labels.Hash()), s.chunks)
	}

	dst, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			Tenant:   "fake",
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return newPrefixedIdentifier(id, dir, dir)
	})
	require.Nil(t, err)

	reader, err := index.NewFileReader(dst.Path())
	require.Nil(t, err)

	p, err := PostingsForMatchers(reader, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
	require.Nil(t, err)

	var (
		chks []index.ChunkMeta
		ls   labels.Labels
	)

	require.True(t, p.Next())
	_, err = reader.Series(p.At(), &ls, &chks)
	require.Nil(t, err)
	require.Equal(t, cases[0].labels.String(), ls.String())
	require.Equal(t, cases[0].chunks, chks)
	require.True(t, p.Next())
	_, err = reader.Series(p.At(), &ls, &chks)
	require.Nil(t, err)
	require.Equal(t, cases[1].labels.String(), ls.String())
	require.Equal(t, cases[1].chunks, chks)
	require.False(t, p.Next())

	mint, maxt := reader.Bounds()
	require.Equal(t, int64(1), mint)
	require.Equal(t, int64(50), maxt)
}
