package bloomgateway

import (
	"context"
	"sort"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
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

func (bq *BloomQuerier) FilterChunkRefs(ctx context.Context, tenant string, from, through model.Time, chunkRefs []*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]*logproto.ChunkRef, error) {
	// Shortcut that does not require any filtering
	if len(chunkRefs) == 0 || len(filters) == 0 {
		return chunkRefs, nil
	}
	// TODO(chaudum): Make buffer pool to reduce allocations.
	// The indexes of the chunks slice correspond to the indexes of the fingerprint slice.
	fingerprints := make([]uint64, 0, len(chunkRefs))
	chunks := make([][]*logproto.ChunkRef, 0, len(chunkRefs))

	// Sort the chunkRefs by their stream fingerprint
	// so we can easily append them to the target slice by iterating over them.
	sort.Slice(chunkRefs, func(i, j int) bool {
		return chunkRefs[i].Fingerprint < chunkRefs[j].Fingerprint
	})

	for _, chunkRef := range chunkRefs {
		idx := len(fingerprints) - 1
		if idx == -1 || fingerprints[idx] < chunkRef.Fingerprint {
			fingerprints = append(fingerprints, chunkRef.Fingerprint)
			chunks = append(chunks, []*logproto.ChunkRef{chunkRef})
			continue
		}
		chunks[idx] = append(chunks[idx], chunkRef)
	}

	// Drop series fingerprints, because they are not used (yet).
	_, refs, err := bq.c.FilterChunks(ctx, tenant, from, through, fingerprints, chunks, filters...)
	if err != nil {
		return nil, err
	}

	// TODO(chaudum): Cache response

	// Flatten response from client and return
	result := make([]*logproto.ChunkRef, 0, len(chunkRefs))
	for i := range refs {
		result = append(result, refs[i]...)
	}
	return result, nil
}
