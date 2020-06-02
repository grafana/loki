package querier

import (
	"time"

	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block/metadata"
)

// BlockMeta is a struct extending the Thanos block metadata and adding
// Cortex-specific data too.
type BlockMeta struct {
	metadata.Meta

	// UploadedAt is the timestamp when the block has been completed to be uploaded
	// to the storage.
	UploadedAt time.Time
}

func getULIDsFromBlockMetas(metas []*BlockMeta) []ulid.ULID {
	ids := make([]ulid.ULID, len(metas))
	for i, m := range metas {
		ids[i] = m.ULID
	}
	return ids
}
