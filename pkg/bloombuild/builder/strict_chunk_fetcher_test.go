package builder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
)

func TestStrictChunkFetcher(t *testing.T) {
	a := chunk.Chunk{ChunkRef: logproto.ChunkRef{UserID: "test", Fingerprint: 1, From: 1, Through: 2, Checksum: 3}}
	b := a
	b.Checksum++
	sentinel := errors.New("download failed")
	for _, tc := range []struct {
		name     string
		fetched  []chunk.Chunk
		fetchErr error
		wantErr  bool
	}{
		{"reordered complete input", []chunk.Chunk{b, a}, nil, false},
		{"partial input without error", []chunk.Chunk{a}, nil, true},
		{"empty input without error", nil, nil, true},
		{"duplicate replaces missing chunk", []chunk.Chunk{a, a}, nil, true},
		{"extra chunk", []chunk.Chunk{a, b, b}, nil, true},
		{"explicit failure", []chunk.Chunk{a}, sentinel, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := strictChunkFetcher{FetchFunc[chunk.Chunk, chunk.Chunk](func(context.Context, []chunk.Chunk) ([]chunk.Chunk, error) {
				return tc.fetched, tc.fetchErr
			})}
			got, err := fetcher.Fetch(context.Background(), []chunk.Chunk{a, b})
			if tc.wantErr {
				require.Error(t, err)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.fetched, got)
			}
			if tc.fetchErr != nil {
				require.ErrorIs(t, err, sentinel)
			}
		})
	}
}
