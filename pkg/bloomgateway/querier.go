package bloomgateway

import (
	"context"
	"sort"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
)

// BloomQuerier is a store-level abstraction on top of Client
// It is used by the index gateway to filter ChunkRefs based on given line fiter expression.
type BloomQuerier struct {
	c      Client
	logger log.Logger
}

func NewBloomQuerier(c Client, logger log.Logger) *BloomQuerier {
	return &BloomQuerier{c: c, logger: logger}
}

func convertToShortRef(ref *logproto.ChunkRef) *logproto.ShortRef {
	return &logproto.ShortRef{From: ref.From, Through: ref.Through, Checksum: ref.Checksum}
}

func (bq *BloomQuerier) FilterChunkRefs(ctx context.Context, tenant string, from, through model.Time, chunkRefs []*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]*logproto.ChunkRef, error) {
	// Shortcut that does not require any filtering
	if len(chunkRefs) == 0 || len(filters) == 0 {
		return chunkRefs, nil
	}

	// TODO(chaudum): Make buffer pool to reduce allocations.
	// The indexes of the chunks slice correspond to the indexes of the fingerprint slice.
	grouped := make([]*logproto.GroupedChunkRefs, 0, len(chunkRefs))
	grouped = groupChunkRefs(chunkRefs, grouped)

	refs, err := bq.c.FilterChunks(ctx, tenant, from, through, grouped, filters...)
	if err != nil {
		return nil, err
	}

	// TODO(chaudum): Cache response

	// Flatten response from client and return
	result := make([]*logproto.ChunkRef, 0, len(chunkRefs))
	for i := range refs {
		for _, ref := range refs[i].Refs {
			result = append(result, &logproto.ChunkRef{
				Fingerprint: refs[i].Fingerprint,
				UserID:      tenant,
				From:        ref.From,
				Through:     ref.Through,
				Checksum:    ref.Checksum,
			})
		}
	}
	return result, nil
}

func groupChunkRefs(chunkRefs []*logproto.ChunkRef, grouped []*logproto.GroupedChunkRefs) []*logproto.GroupedChunkRefs {
	// Sort the chunkRefs by their stream fingerprint
	// so we can easily append them to the target slice by iterating over them.
	sort.Slice(chunkRefs, func(i, j int) bool {
		return chunkRefs[i].Fingerprint < chunkRefs[j].Fingerprint
	})

	for _, chunkRef := range chunkRefs {
		idx := len(grouped) - 1
		if idx == -1 || grouped[idx].Fingerprint < chunkRef.Fingerprint {
			grouped = append(grouped, &logproto.GroupedChunkRefs{
				Fingerprint: chunkRef.Fingerprint,
				Tenant:      chunkRef.UserID,
				Refs:        []*logproto.ShortRef{convertToShortRef(chunkRef)},
			})
			continue
		}
		grouped[idx].Refs = append(grouped[idx].Refs, convertToShortRef(chunkRef))
	}
	return grouped
}
