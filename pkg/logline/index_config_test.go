package logline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIndexConfigValidate_DocumentShards(t *testing.T) {
	for _, tt := range []struct {
		name       string
		version    string
		shards     int
		wantShards int
		wantErr    string
	}{
		{name: "v3 leaves shards unset", version: "v3", wantShards: 0},
		{name: "default version leaves shards unset", version: "", wantShards: 0},
		{name: "v3 rejects shards", version: "v3", shards: 32, wantErr: "document_shards requires index version v5"},
		{name: "v4 rejects shards", version: "v4", shards: 1, wantErr: "document_shards requires index version v5"},
		{name: "v5 defaults to 32", version: "v5", wantShards: DefaultDocumentShards},
		{name: "v5 accepts 1", version: "v5", shards: 1, wantShards: 1},
		{name: "v5 accepts 128", version: "v5", shards: 128, wantShards: 128},
		{name: "v5 rejects non power of two", version: "v5", shards: 24, wantErr: "power of two"},
		{name: "v5 rejects above max", version: "v5", shards: 256, wantErr: "power of two"},
		{name: "v5 rejects negative", version: "v5", shards: -1, wantErr: "power of two"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := IndexConfig{Version: tt.version, DocumentShards: tt.shards}
			err := cfg.Validate()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantShards, cfg.DocumentShards)
		})
	}
}

func TestIndexConfigValidate_DefaultDocumentInterval(t *testing.T) {
	for _, version := range AllVersions() {
		cfg := IndexConfig{Version: version}
		require.NoError(t, cfg.Validate())
		require.Equal(t, 16*time.Second, cfg.DocumentInterval, version)
	}
}
