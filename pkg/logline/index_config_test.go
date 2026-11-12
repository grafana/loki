package logline

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIndexConfigValidate_DocumentShardBits(t *testing.T) {
	for _, tt := range []struct {
		name     string
		version  string
		bits     int
		wantBits int
		wantErr  string
	}{
		{name: "v3 ignores shard bits", version: "v3", bits: DefaultDocumentShardBits, wantBits: 0},
		{name: "default version ignores shard bits", version: "", bits: DefaultDocumentShardBits, wantBits: 0},
		{name: "v4 ignores shard bits", version: "v4", bits: 3, wantBits: 0},
		{name: "v5 accepts 0", version: "v5", bits: 0, wantBits: 0},
		{name: "v5 accepts the default", version: "v5", bits: DefaultDocumentShardBits, wantBits: DefaultDocumentShardBits},
		{name: "v5 accepts the max", version: "v5", bits: MaxDocumentShardBits, wantBits: MaxDocumentShardBits},
		{name: "v5 rejects above max", version: "v5", bits: MaxDocumentShardBits + 1, wantErr: "document_shard_bits must be from 0 to 7"},
		{name: "v5 rejects negative", version: "v5", bits: -1, wantErr: "document_shard_bits must be from 0 to 7"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := IndexConfig{Version: tt.version, DocumentShardBits: tt.bits}
			err := cfg.Validate()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantBits, cfg.DocumentShardBits)
		})
	}
}

func TestIndexConfig_DocumentShardBitsFlagDefault(t *testing.T) {
	var cfg IndexConfig
	cfg.RegisterFlagsWithPrefix("logline-index", flag.NewFlagSet("test", flag.PanicOnError))
	require.Equal(t, DefaultDocumentShardBits, cfg.DocumentShardBits)
	require.Equal(t, 5, DefaultDocumentShardBits, "32 document shards")
}

func TestIndexConfigValidate_DefaultDocumentInterval(t *testing.T) {
	for _, version := range AllVersions() {
		cfg := IndexConfig{Version: version}
		require.NoError(t, cfg.Validate())
		require.Equal(t, 16*time.Second, cfg.DocumentInterval, version)
	}
}
