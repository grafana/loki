package bench

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParquetStore(t *testing.T) {

	dir := filepath.Join(filepath.Dir(os.Args[0]), "data/parquet")
	store, err := NewParquetStore(dir, "test")
	require.NoError(t, err)

	builder := NewBuilder(dir, DefaultOpt(), store)

	err = builder.Generate(context.Background(), 100*1024*1024)
	require.NoError(t, err)

	require.NoError(t, store.Close())
}
