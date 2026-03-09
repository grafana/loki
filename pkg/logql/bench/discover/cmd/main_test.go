package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain_StorageModeMissingBucket(t *testing.T) {
	t.Setenv("LOKI_ADDR", "")
	t.Setenv("LOKI_ORG_ID", "")

	code := run([]string{
		"--storage-type", "s3",
		"--tenant", "test-tenant",
	})
	require.Equal(t, 1, code, "run() should return 1 when --bucket is missing in storage mode")
}

func TestMain_StorageModeMissingTenant(t *testing.T) {
	t.Setenv("LOKI_ADDR", "")
	t.Setenv("LOKI_ORG_ID", "")

	code := run([]string{
		"--storage-type", "s3",
		"--bucket", "index-bucket",
	})
	require.Equal(t, 1, code, "run() should return 1 when --tenant is missing in storage mode")
}

func TestMain_StorageModeMissingAddress(t *testing.T) {
	t.Setenv("LOKI_ADDR", "")
	t.Setenv("LOKI_ORG_ID", "")

	code := run([]string{
		"--storage-type", "filesystem",
		"--bucket", t.TempDir(),
		"--tenant", "test-tenant",
		"--selector", `namespace="test"`,
	})

	require.Equal(t, 1, code, "run() should return 1 when --address is missing")
}

func TestMain_StorageModeMissingSelector(t *testing.T) {
	t.Setenv("LOKI_ADDR", "")
	t.Setenv("LOKI_ORG_ID", "")

	code := run([]string{
		"--storage-type", "filesystem",
		"--bucket", t.TempDir(),
		"--tenant", "test-tenant",
		"--address", "http://localhost:3100",
	})

	require.Equal(t, 1, code, "run() should return 1 when --selector is missing")
}

// TestMain_InvalidFromFlag verifies that run() exits with code 1 when --from
// is provided but cannot be parsed as RFC3339.
func TestMain_InvalidFromFlag(t *testing.T) {
	code := run([]string{
		"--storage-type", "filesystem",
		"--bucket", t.TempDir(),
		"--tenant", "test-tenant",
		"--from", "not-a-date",
	})
	require.Equal(t, 1, code, "run() should return 1 for invalid --from")
}

// TestMain_InvalidToFlag verifies that run() exits with code 1 when --to is
// provided but cannot be parsed as RFC3339.
func TestMain_InvalidToFlag(t *testing.T) {
	code := run([]string{
		"--storage-type", "filesystem",
		"--bucket", t.TempDir(),
		"--tenant", "test-tenant",
		"--to", "not-a-date",
	})
	require.Equal(t, 1, code, "run() should return 1 for invalid --to")
}
