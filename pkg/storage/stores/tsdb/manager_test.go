package tsdb

import (
	"os"
	"path"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
)

func TestMigrateMultitenantDir(t *testing.T) {
	tableRange := config.TableRange{
		Start: 10,
		End:   20,
		PeriodConfig: &config.PeriodConfig{IndexTables: config.PeriodicTableConfig{
			Prefix: "index_",
			Period: 24 * time.Hour,
		}},
	}

	t.Run("migrate tables in range", func(t *testing.T) {
		parentDir := t.TempDir()
		multitenantDir := managerMultitenantDir(parentDir)

		for _, name := range []string{
			"index_9", "index_24", // outside of table range
			"index_12", "index_16", // within table range
			"table_16", // prefix mismatch
			"foobar",   // invalid table name
		} {
			assert.NoError(t, os.MkdirAll(path.Join(multitenantDir, name), 0755))
		}

		// files should be ignored during migration
		_, err := os.Create(path.Join(multitenantDir, "index_13"))
		assert.NoError(t, err)

		dir := path.Join(parentDir, "fs_1234")
		assert.NoError(t, util.EnsureDirectory(managerMultitenantDir(dir)))

		// perform migration
		assert.NoError(t, migrateMultitenantDir(dir, tableRange))

		files, err := os.ReadDir(managerMultitenantDir(dir))
		assert.NoError(t, err)

		var got []string
		for _, f := range files {
			if !f.IsDir() {
				t.Error("migrateMultitenantDir should not migrate files")
			}

			got = append(got, f.Name())
		}

		assert.ElementsMatch(t, got, []string{"index_12", "index_16"})
	})

	t.Run("parent multitenant dir does not exist", func(t *testing.T) {
		parentDir := t.TempDir()
		dir := path.Join(parentDir, "fs_1234")
		// should handle gracefully
		assert.NoError(t, migrateMultitenantDir(dir, tableRange))
	})

}
