package deletion

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

const (
	tombstonePrefix = "markers/dataobj-tombstones"
	tombstoneSuffix = ".tomb"
)

// WriteTombstone writes a tombstone marker to object storage.
// This contains all objects and sections tombstoned from a single delete request.
// Path: markers/dataobj-tombstones/<tenant>/<request-id>.tomb
func WriteTombstone(ctx context.Context, objectClient client.ObjectClient, tombstone *deletionproto.DataObjectTombstone) error {
	data, err := proto.Marshal(tombstone)
	if err != nil {
		return fmt.Errorf("failed to marshal tombstone: %w", err)
	}

	key := buildTombstoneKey(tombstone.TenantID, tombstone.DeleteRequestID)
	if err := objectClient.PutObject(ctx, key, strings.NewReader(unsafeGetString(data))); err != nil {
		return fmt.Errorf("failed to write tombstone to %s: %w", key, err)
	}

	return nil
}

// ReadTombstone reads a tombstone marker from object storage.
func ReadTombstone(ctx context.Context, objectClient client.ObjectClient, key string) (*deletionproto.DataObjectTombstone, error) {
	reader, _, err := objectClient.GetObject(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get tombstone from %s: %w", key, err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read tombstone data: %w", err)
	}

	var tombstone deletionproto.DataObjectTombstone
	if err := proto.Unmarshal(data, &tombstone); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tombstone: %w", err)
	}

	return &tombstone, nil
}

// ListTombstones lists all tombstone markers for a tenant.
func ListTombstones(ctx context.Context, objectClient client.ObjectClient, tenantID string) ([]*deletionproto.DataObjectTombstone, error) {
	prefix := path.Join(tombstonePrefix, tenantID) + "/"

	objects, _, err := objectClient.List(ctx, prefix, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list tombstones in %s: %w", prefix, err)
	}

	tombstones := make([]*deletionproto.DataObjectTombstone, 0, len(objects))
	for _, obj := range objects {
		if !strings.HasSuffix(obj.Key, tombstoneSuffix) {
			continue
		}

		tombstone, err := ReadTombstone(ctx, objectClient, obj.Key)
		if err != nil {
			// Log and continue on individual failures
			continue
		}
		tombstones = append(tombstones, tombstone)
	}

	return tombstones, nil
}

// buildTombstoneKey creates the storage key for a tombstone marker.
// Format: markers/dataobj-tombstones/<tenant>/<request-id>.tomb
func buildTombstoneKey(tenantID, requestID string) string {
	return path.Join(tombstonePrefix, tenantID, requestID+tombstoneSuffix)
}
