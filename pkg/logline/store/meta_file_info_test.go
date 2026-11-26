package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zeebo/xxh3"
)

func TestMeta_SetFileInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.idx")
	data := []byte("some-index-payload-bytes")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var meta Meta
	require.NoError(t, meta.SetFileInfo(f))

	require.Equal(t, fmt.Sprintf("%016x", xxh3.Hash(data)), meta.Hash)
	require.Equal(t, int64(len(data)), meta.SizeBytes)

	// IndexHeader is the caller's responsibility, so it stays nil here.
	require.Nil(t, meta.IndexHeader)

	// The read offset is reset so the same handle can be passed to PutIndex.
	offset, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	require.Zero(t, offset)
}

func TestMeta_SetFileInfo_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.idx")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var meta Meta
	require.NoError(t, meta.SetFileInfo(f))
	require.Zero(t, meta.SizeBytes)
	require.Equal(t, fmt.Sprintf("%016x", xxh3.Hash(nil)), meta.Hash)
}

func TestMeta_SetFileInfo_ClosedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.idx")
	require.NoError(t, os.WriteFile(path, []byte("payload"), 0o644))

	f, err := os.Open(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	var meta Meta
	require.Error(t, meta.SetFileInfo(f))
}
