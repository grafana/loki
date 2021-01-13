package tsdb

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"time"

	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// Relative to user-specific prefix.
const TenantDeletionMarkPath = "markers/tenant-deletion-mark.json"

type TenantDeletionMark struct {
	// Unix timestamp when deletion marker was created.
	DeletionTime int64 `json:"deletion_time"`
}

// Checks for deletion mark for tenant. Errors other than "object not found" are returned.
func TenantDeletionMarkExists(ctx context.Context, bkt objstore.BucketReader, userID string) (bool, error) {
	markerFile := path.Join(userID, TenantDeletionMarkPath)

	return bkt.Exists(ctx, markerFile)
}

// Uploads deletion mark to the tenant "directory".
func WriteTenantDeletionMark(ctx context.Context, bkt objstore.Bucket, userID string) error {
	m := &TenantDeletionMark{DeletionTime: time.Now().Unix()}

	data, err := json.Marshal(m)
	if err != nil {
		return errors.Wrap(err, "serialize tenant deletion mark")
	}

	markerFile := path.Join(userID, TenantDeletionMarkPath)
	return errors.Wrap(bkt.Upload(ctx, markerFile, bytes.NewReader(data)), "upload tenant deletion mark")
}
