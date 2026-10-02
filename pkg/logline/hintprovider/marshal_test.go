package hintprovider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

func TestToFromProtoIndexMeta(t *testing.T) {
	header := &format.HeaderInfo{Version: 3, DocumentCount: 4, TermCount: 12}
	meta := store.Meta{
		Date:           "2026-02-26",
		StorageID:      "aaaaaaaaaaaaaaaa",
		Version:        "v3",
		SizeBytes:      4096,
		MinLogTs:       time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC),
		MaxLogTs:       time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC),
		ShardCount:     10,
		ShardAlgorithm: "murmur3_mix",
		ShardValue:     3,
		IndexHeader:    header,
	}

	got := fromProtoIndexMeta(toProtoIndexMeta(meta))
	require.Equal(t, meta.ID(), got.ID())
	require.Equal(t, meta.IndexPath(), got.IndexPath())
	require.Equal(t, meta.Version, got.Version)
	require.Equal(t, meta.SizeBytes, got.SizeBytes)
	require.Equal(t, meta.MinLogTs, got.MinLogTs)
	require.Equal(t, meta.MaxLogTs, got.MaxLogTs)
	require.Equal(t, meta.ShardCount, got.ShardCount)
	require.Equal(t, meta.ShardAlgorithm, got.ShardAlgorithm)
	require.Equal(t, meta.ShardValue, got.ShardValue)
	require.Equal(t, *header, *got.IndexHeader)
}

func TestFromProtoIndexMeta_NilHeader(t *testing.T) {
	got := fromProtoIndexMeta(toProtoIndexMeta(store.Meta{
		Date:      "2026-02-26",
		StorageID: "bbbbbbbbbbbbbbbb",
		Version:   "v3",
		SizeBytes: 1,
	}))
	require.Nil(t, got.IndexHeader)
	require.Equal(t, "2026-02-26/bbbbbbbbbbbbbbbb", got.ID())
}
