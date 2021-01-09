package bucketindex

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block/metadata"
)

const (
	MarkersPathname = "markers"
)

// BlockDeletionMarkFilepath returns the path, relative to the tenant's bucket location,
// of a block deletion mark in the bucket markers location.
func BlockDeletionMarkFilepath(blockID ulid.ULID) string {
	return fmt.Sprintf("%s/%s-%s", MarkersPathname, blockID.String(), metadata.DeletionMarkFilename)
}

// IsBlockDeletionMarkFilename returns whether the input filename matches the expected pattern
// of block deletion markers stored in the markers location.
func IsBlockDeletionMarkFilename(name string) (ulid.ULID, bool) {
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 {
		return ulid.ULID{}, false
	}

	// Ensure the 2nd part matches the block deletion mark filename.
	if parts[1] != metadata.DeletionMarkFilename {
		return ulid.ULID{}, false
	}

	// Ensure the 1st part is a valid block ID.
	id, err := ulid.Parse(filepath.Base(parts[0]))
	return id, err == nil
}
