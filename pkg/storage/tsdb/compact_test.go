package tsdb

import (
	"context"
	"testing"

	"github.com/grafana/loki/pkg/storage/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

// no file error
// single file passthrough
// multi file works
// multi file dedupes

func TestCompactor(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		err   bool
		input [][]LoadableSeries
		exp   []ChunkRef
	}{
		{
			desc: "errors no file",
			err:  true,
		},
		{
			desc: "single file",
			err:  false,
			input: [][]LoadableSeries{
				{
					{
						Labels: mustParseLabels(`{foo="bar"}`),
						Chunks: []index.ChunkMeta{
							{
								MinTime:  0,
								MaxTime:  3,
								Checksum: 0,
							},
							{
								MinTime:  1,
								MaxTime:  4,
								Checksum: 1,
							},
							{
								MinTime:  2,
								MaxTime:  5,
								Checksum: 2,
							},
						},
					},
					{
						Labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
						Chunks: []index.ChunkMeta{
							{
								MinTime:  1,
								MaxTime:  10,
								Checksum: 3,
							},
						},
					},
				},
			},
			exp: []ChunkRef{
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
					Start:       0,
					End:         3,
					Checksum:    0,
				},
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
					Start:       1,
					End:         4,
					Checksum:    1,
				},
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
					Start:       2,
					End:         5,
					Checksum:    2,
				},
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
					Start:       1,
					End:         10,
					Checksum:    3,
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			dir := t.TempDir()
			c := NewCompactor("fake", dir)

			var indices []*TSDBIndex
			for _, cases := range tc.input {
				idx := BuildIndex(t, dir, "fake", cases)
				defer idx.Close()
				indices = append(indices, idx)
			}

			out, err := c.Compact(context.Background(), indices...)
			if tc.err {
				require.NotNil(t, err)
				return
			}

			idx, err := LoadTSDBIdentifier(dir, out)
			require.Nil(t, err)
			defer idx.Close()

			from, through := inclusiveBounds(idx)
			res, err := idx.GetChunkRefs(
				context.Background(),
				"fake",
				from,
				through,
				nil,
				nil,
				labels.MustNewMatcher(labels.MatchEqual, "", ""),
			)

			require.Nil(t, err)
			require.Equal(t, tc.exp, res)

		})
	}

}
