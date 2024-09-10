// SPDX-License-Identifier: AGPL-3.0-only

package shutdownmarker

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShutdownMarker_GetPath(t *testing.T) {
	dir := "/a/b/c"
	expectedPath := filepath.Join(dir, shutdownMarkerFilename)
	require.Equal(t, expectedPath, GetPath(dir))
}

func TestShutdownMarker_Create(t *testing.T) {
	dir := t.TempDir()
	shutdownMarkerPath := GetPath(dir)
	exists, err := Exists(shutdownMarkerPath)
	require.NoError(t, err)
	require.False(t, exists)

	err = Create(shutdownMarkerPath)
	require.NoError(t, err)

	exists, err = Exists(shutdownMarkerPath)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestShutdownMarker_Remove(t *testing.T) {
	dir := t.TempDir()
	shutdownMarkerPath := GetPath(dir)
	exists, err := Exists(shutdownMarkerPath)
	require.NoError(t, err)
	require.False(t, exists)

	require.Nil(t, Create(shutdownMarkerPath))
	exists, err = Exists(shutdownMarkerPath)
	require.NoError(t, err)
	require.True(t, exists)

	require.Nil(t, Remove(shutdownMarkerPath))
	exists, err = Exists(shutdownMarkerPath)
	require.NoError(t, err)
	require.False(t, exists)
}
