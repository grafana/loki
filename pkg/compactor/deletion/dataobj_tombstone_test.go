package deletion

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
)

func TestWriteReadTombstone(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: tempDir,
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		tombstone *deletionproto.DataObjectTombstone
	}{
		{
			name: "single section",
			tombstone: &deletionproto.DataObjectTombstone{
				ObjectPath:            "data/tenant-1/12345.dataobj",
				DeletedSectionIndices: []uint32{1},
				DeleteRequestID:       "req-123",
				CreatedAt:             model.Now(),
				TenantID:              "tenant-1",
			},
		},
		{
			name: "multiple sections",
			tombstone: &deletionproto.DataObjectTombstone{
				ObjectPath:            "data/tenant-1/12346.dataobj",
				DeletedSectionIndices: []uint32{1, 2, 3, 5, 8},
				DeleteRequestID:       "req-456",
				CreatedAt:             model.Now(),
				TenantID:              "tenant-1",
			},
		},
		{
			name: "different tenant",
			tombstone: &deletionproto.DataObjectTombstone{
				ObjectPath:            "data/tenant-2/99999.dataobj",
				DeletedSectionIndices: []uint32{0, 1},
				DeleteRequestID:       "req-789",
				CreatedAt:             model.Now(),
				TenantID:              "tenant-2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write tombstone
			err := WriteTombstone(ctx, objectClient, tt.tombstone)
			require.NoError(t, err)

			// Build the expected key
			key := buildTombstoneKey(tt.tombstone.TenantID, tt.tombstone.ObjectPath, tt.tombstone.CreatedAt)

			// Read it back
			readTombstone, err := ReadTombstone(ctx, objectClient, key)
			require.NoError(t, err)

			// Verify fields match
			require.Equal(t, tt.tombstone.ObjectPath, readTombstone.ObjectPath)
			require.Equal(t, tt.tombstone.DeletedSectionIndices, readTombstone.DeletedSectionIndices)
			require.Equal(t, tt.tombstone.DeleteRequestID, readTombstone.DeleteRequestID)
			require.Equal(t, tt.tombstone.CreatedAt, readTombstone.CreatedAt)
			require.Equal(t, tt.tombstone.TenantID, readTombstone.TenantID)
		})
	}
}

func TestListTombstones(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: tempDir,
	})
	require.NoError(t, err)

	// Create tombstones for different tenants
	createdAt := model.Now()

	tombstones := []*deletionproto.DataObjectTombstone{
		{
			ObjectPath:            "data/tenant-1/12345.dataobj",
			DeletedSectionIndices: []uint32{1, 2},
			DeleteRequestID:       "req-1",
			CreatedAt:             createdAt,
			TenantID:              "tenant-1",
		},
		{
			ObjectPath:            "data/tenant-1/12346.dataobj",
			DeletedSectionIndices: []uint32{3, 4},
			DeleteRequestID:       "req-2",
			CreatedAt:             createdAt,
			TenantID:              "tenant-1",
		},
		{
			ObjectPath:            "data/tenant-2/99999.dataobj",
			DeletedSectionIndices: []uint32{0},
			DeleteRequestID:       "req-3",
			CreatedAt:             createdAt,
			TenantID:              "tenant-2",
		},
	}

	// Write all tombstones
	for _, tomb := range tombstones {
		err := WriteTombstone(ctx, objectClient, tomb)
		require.NoError(t, err)
	}

	// List tombstones for tenant-1
	listed, err := ListTombstones(ctx, objectClient, "tenant-1")
	require.NoError(t, err)
	require.Len(t, listed, 2)

	// Verify we got the right tombstones
	objectPaths := make(map[string]bool)
	for _, tomb := range listed {
		require.Equal(t, "tenant-1", tomb.TenantID)
		objectPaths[tomb.ObjectPath] = true
	}
	require.True(t, objectPaths["data/tenant-1/12345.dataobj"])
	require.True(t, objectPaths["data/tenant-1/12346.dataobj"])

	// List tombstones for tenant-2
	listed, err = ListTombstones(ctx, objectClient, "tenant-2")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "tenant-2", listed[0].TenantID)
	require.Equal(t, "data/tenant-2/99999.dataobj", listed[0].ObjectPath)

	// List tombstones for non-existent tenant
	listed, err = ListTombstones(ctx, objectClient, "tenant-3")
	require.NoError(t, err)
	require.Len(t, listed, 0)
}

func TestBuildTombstoneKey(t *testing.T) {
	tenantID := "tenant-1"
	objectPath := "data/tenant-1/12345.dataobj"
	createdAt := model.Time(1705329600000) // 2024-01-15 12:00:00 UTC

	key1 := buildTombstoneKey(tenantID, objectPath, createdAt)
	key2 := buildTombstoneKey(tenantID, objectPath, createdAt)

	// Same inputs should produce same key
	require.Equal(t, key1, key2)

	// Key should have expected format
	require.Contains(t, key1, "markers/dataobj-tombstones")
	require.Contains(t, key1, tenantID)
	require.Contains(t, key1, ".tomb")

	// Different timestamp should produce different key
	key3 := buildTombstoneKey(tenantID, objectPath, createdAt+1)
	require.NotEqual(t, key1, key3)

	// Different object path should produce different key
	key4 := buildTombstoneKey(tenantID, "data/tenant-1/99999.dataobj", createdAt)
	require.NotEqual(t, key1, key4)
}

func TestReadTombstoneNotFound(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: tempDir,
	})
	require.NoError(t, err)

	// Try to read non-existent tombstone
	_, err = ReadTombstone(ctx, objectClient, "markers/dataobj-tombstones/tenant-1/20240115/nonexistent.tomb")
	require.Error(t, err)
}
