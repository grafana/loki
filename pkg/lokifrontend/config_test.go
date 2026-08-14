package lokifrontend

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_RegisterFlags(t *testing.T) {
	t.Run("-frontend.support-parquet-encoding", func(t *testing.T) {
		var cfg Config
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg.RegisterFlags(fs)

		require.True(t, cfg.CompressResponses)
		require.False(t, cfg.SupportParquetEncoding)

		require.NoError(t, fs.Parse([]string{"-frontend.support-parquet-encoding=true"}))

		require.True(t, cfg.SupportParquetEncoding)
		require.True(t, cfg.CompressResponses)
	})
}
