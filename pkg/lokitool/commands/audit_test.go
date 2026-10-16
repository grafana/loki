package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const auditConfig = `
tenant: "12345"
period: "19000"
schema_config:
  configs:
    - from: "2023-08-21"
      index:
        period: 24h
        prefix: loki_index_
      object_store: filesystem
      schema: v13
      store: tsdb
storage_config:
  filesystem:
    directory: /tmp/chunks
`

func writeAuditConfig(t *testing.T, content string) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

func TestAuditConfig(t *testing.T) {
	t.Run("configuration file", func(t *testing.T) {
		a := AuditCommand{configFile: writeAuditConfig(t, auditConfig)}

		cfg, err := a.config()
		require.NoError(t, err)
		require.Equal(t, "12345", cfg.Tenant)
		require.Equal(t, "19000", cfg.Period)
		require.Equal(t, "/tmp/chunks", cfg.StorageConfig.FSConfig.Directory)
	})

	t.Run("period flag takes precedence", func(t *testing.T) {
		a := AuditCommand{configFile: writeAuditConfig(t, auditConfig), period: "19959"}

		cfg, err := a.config()
		require.NoError(t, err)
		require.Equal(t, "19959", cfg.Period)
	})

	t.Run("additional flags", func(t *testing.T) {
		a := AuditCommand{
			configFile: writeAuditConfig(t, auditConfig),
			extraArgs:  []string{"-tenant=other", "-concurrency=5"},
		}

		cfg, err := a.config()
		require.NoError(t, err)
		require.Equal(t, "other", cfg.Tenant)
		require.Equal(t, 5, cfg.Concurrency)
	})

	t.Run("invalid configuration", func(t *testing.T) {
		a := AuditCommand{configFile: writeAuditConfig(t, "tenant: \"12345\"\n")}

		_, err := a.config()
		require.ErrorContains(t, err, "failed validating config")
	})

	t.Run("missing configuration file", func(t *testing.T) {
		a := AuditCommand{configFile: filepath.Join(t.TempDir(), "missing.yaml")}

		_, err := a.config()
		require.ErrorContains(t, err, "failed parsing config")
	})
}

func TestAuditFlags(t *testing.T) {
	app, global := NewApplication()
	defer global.Close()

	// The period can be passed as flag, other configuration flags are passed after the separator.
	_, err := app.ParseContext([]string{
		"audit", "index",
		"--config.file=config.yaml", "--index.file=index/table/file.tsdb.gz", "--period=19959",
		"--", "-tenant=12345",
	})
	require.NoError(t, err)
}
