package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
)

func TestComputeIndexHash_UsesXXH3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.idx")
	data := []byte("index-bytes-for-hashing")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	hash, err := computeIndexHash(f)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%016x", xxh3.Hash(data)), hash)
}

func TestNewStorageID_ReturnsULID(t *testing.T) {
	first, err := NewStorageID()
	require.NoError(t, err)
	second, err := NewStorageID()
	require.NoError(t, err)

	// ULIDs are 26-char Crockford base32.
	require.Len(t, first, 26)
	require.Len(t, second, 26)
	require.NotEqual(t, first, second)
	require.Regexp(t, "^[0-9A-Z]+$", first)
	require.Regexp(t, "^[0-9A-Z]+$", second)
}
