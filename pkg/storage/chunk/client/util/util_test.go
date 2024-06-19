package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Directory to be created by EnsureDir
	dirPath := filepath.Join(tmpDir, "testdir")

	// Ensure the directory does not exist before the test
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Fatalf("Directory already exists: %v", err)
	}

	// create with default permissions
	require.NoError(t, EnsureDirectoryWithDefaultPermissions(dirPath, 0o640))

	// ensure the directory passes the permission check for more restrictive permissions
	require.NoError(t, RequirePermissions(dirPath, 0o600))

	// ensure the directory fails the permission check for less restrictive permissions
	require.Error(t, RequirePermissions(dirPath, 0o660))
}
