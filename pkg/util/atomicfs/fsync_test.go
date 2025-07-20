// SPDX-License-Identifier: AGPL-3.0-only

package atomicfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "TestCreateFile")
	require.NoError(t, CreateFile(path, strings.NewReader("test")))

	// Ensure the temporary file created by CreateFile has been removed.
	_, err := os.Stat(tempPath(path))
	require.ErrorIs(t, err, os.ErrNotExist)

	// Ensure the directory entry for the file exists.
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	requireContainsFile(t, entries, path)

	// Check the contents of the file.
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "test", string(contents))
}

func TestCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "TestCreate")
		f, err := Create(path)
		require.NoError(t, err)

		_, err = f.WriteString("test")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		// Ensure the directory entry for the file exists.
		entries, err := os.ReadDir(filepath.Dir(path))
		require.NoError(t, err)
		requireContainsFile(t, entries, path)

		// Check the contents of the file.
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "test", string(contents))
	})

	t.Run("duplicate close", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "TestCreate")
		f, err := Create(path)
		require.NoError(t, err)

		_, err = f.WriteString("test")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		// File has already been closed, our attempt to fsync and close again should fail.
		require.ErrorIs(t, f.Close(), os.ErrClosed)

		// Original file _should not_ have been modified by trying to close again.
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "test", string(contents))
	})

}

func requireContainsFile(t *testing.T, entries []os.DirEntry, path string) {
	name := filepath.Base(path)

	for _, entry := range entries {
		if entry.Name() == name {
			return
		}
	}

	t.Fatalf("expected to find %s in %+v", name, entries)
}
