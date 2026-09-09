package store_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/store"
)

func TestMeta_ShardFieldsRoundTrip(t *testing.T) {
	m := store.Meta{
		Date:           "2026-03-20",
		Hash:           "abc123",
		ShardCount:     4,
		ShardAlgorithm: "first_byte",
		ShardValue:     2,
	}

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var got store.Meta
	require.NoError(t, json.Unmarshal(data, &got))

	require.Equal(t, 4, got.ShardCount)
	require.Equal(t, "first_byte", got.ShardAlgorithm)
	require.Equal(t, 2, got.ShardValue)
}

func TestMeta_ShardFieldsZeroValueBackwardCompat(t *testing.T) {
	// Old meta.json without shard fields deserializes with zero values (unsharded).
	raw := `{"date":"2026-01-01","hash":"abc","version":"lidx-fast-v2","min_log_ts":"2026-01-01T00:00:00Z","max_log_ts":"2026-01-01T23:59:59Z","min_rec_ts":"2026-01-01T00:00:00Z","max_rec_ts":"2026-01-01T23:59:59Z","index_header":null}`
	var m store.Meta
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	require.Equal(t, 0, m.ShardCount)
	require.Equal(t, "", m.ShardAlgorithm)
	require.Equal(t, 0, m.ShardValue)
}

func TestMeta_ShardFieldsAlwaysSerialized(t *testing.T) {
	// Unsharded meta must serialize shard fields as zero (no omitempty).
	m := store.Meta{ShardCount: 0, ShardAlgorithm: "", ShardValue: 0}
	data, err := json.Marshal(m)
	require.NoError(t, err)
	require.Contains(t, string(data), `"shard_count":0`)
	require.Contains(t, string(data), `"shard_algorithm":""`)
	require.Contains(t, string(data), `"shard_value":0`)
}
