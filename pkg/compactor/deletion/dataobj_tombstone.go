package deletion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

const (
	tombstonePrefix = "markers/dataobj-tombstones"
	tombstoneSuffix = ".tomb"
)

// WriteTombstone writes a tombstone marker to object storage.
// Path: markers/dataobj-tombstones/<tenant>/<hash>.tomb
func WriteTombstone(ctx context.Context, objectClient client.ObjectClient, tombstone *deletionproto.DataObjectTombstone) error {
	data, err := proto.Marshal(tombstone)
	if err != nil {
		return fmt.Errorf("failed to marshal tombstone: %w", err)
	}

	key := buildTombstoneKey(tombstone.TenantID, tombstone.ObjectPath, tombstone.CreatedAt)
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

// ListTombstones lists all tombstones for a tenant.
// NOTE: If this becomes slow (>1000 tombstones/tenant), add window partitioning to the path.
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

// buildTombstoneKey creates the storage key for a tombstone.
// Format: markers/dataobj-tombstones/<tenant>/<hash>.tomb
func buildTombstoneKey(tenantID, objectPath string, createdAt model.Time) string {
	// Use object path + timestamp for hash to ensure uniqueness
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", objectPath, createdAt)))
	hashStr := hex.EncodeToString(hash[:8]) // Use first 8 bytes for shorter names

	return path.Join(tombstonePrefix, tenantID, hashStr+tombstoneSuffix)
}
