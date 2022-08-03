package tsdb

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

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
		{
			desc: "multi file",
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
				{
					{
						Labels: mustParseLabels(`{foo="bar", a="b"}`),
						Chunks: []index.ChunkMeta{
							{
								MinTime:  10,
								MaxTime:  11,
								Checksum: 0,
							},
							{
								MinTime:  11,
								MaxTime:  14,
								Checksum: 1,
							},
							{
								MinTime:  12,
								MaxTime:  15,
								Checksum: 2,
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
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", a="b"}`).Hash()),
					Start:       10,
					End:         11,
					Checksum:    0,
				},
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", a="b"}`).Hash()),
					Start:       11,
					End:         14,
					Checksum:    1,
				},
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", a="b"}`).Hash()),
					Start:       12,
					End:         15,
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
		{
			desc: "multi file dedupe",
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
				// duplicate of first index
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
				{
					{
						Labels: mustParseLabels(`{foo="bar", a="b"}`),
						Chunks: []index.ChunkMeta{
							{
								MinTime:  10,
								MaxTime:  11,
								Checksum: 0,
							},
							{
								MinTime:  11,
								MaxTime:  14,
								Checksum: 1,
							},
							{
								MinTime:  12,
								MaxTime:  15,
								Checksum: 2,
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
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", a="b"}`).Hash()),
					Start:       10,
					End:         11,
					Checksum:    0,
				},
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", a="b"}`).Hash()),
					Start:       11,
					End:         14,
					Checksum:    1,
				},
				{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", a="b"}`).Hash()),
					Start:       12,
					End:         15,
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
				casted, ok := idx.Index.(*TSDBIndex)
				require.Equal(t, true, ok)
				indices = append(indices, casted)
			}

			out, err := c.Compact(context.Background(), indices...)
			if tc.err {
				require.NotNil(t, err)
				return
			}
			require.Nil(t, err)

			idx, err := NewShippableTSDBFile(out, false)
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
