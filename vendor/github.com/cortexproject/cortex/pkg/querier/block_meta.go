package querier

import (
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block/metadata"

	"github.com/cortexproject/cortex/pkg/util"
)

// BlockMeta is a struct extending the Thanos block metadata and adding
// Cortex-specific data too.
type BlockMeta struct {
	metadata.Meta

	// UploadedAt is the timestamp when the block has been completed to be uploaded
	// to the storage.
	UploadedAt time.Time
}

func (m BlockMeta) String() string {
	minT := util.TimeFromMillis(m.MinTime).UTC()
	maxT := util.TimeFromMillis(m.MaxTime).UTC()

	return fmt.Sprintf("%s (min time: %s max time: %s)", m.ULID, minT.String(), maxT.String())
}

type BlockMetas []*BlockMeta

func (s BlockMetas) String() string {
	b := strings.Builder{}

	for idx, m := range s {
		if idx > 0 {
			b.WriteString(", ")
		}
		b.WriteString(m.String())
	}

	return b.String()
}

func getULIDsFromBlockMetas(metas []*BlockMeta) []ulid.ULID {
	ids := make([]ulid.ULID, len(metas))
	for i, m := range metas {
		ids[i] = m.ULID
	}
	return ids
}
