package push

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/common/model"
)

// HTTPHeaderBackfillShardKey is the HTTP header set by a backfill worker to mark a push as backfill
// data. When present and valid, Loki adds the internal labels constants.BackfillLabel and
// constants.BackfillShardLabel to every stream in the request.
//
// The value is opaque to Loki: it may be a uuid, a hash, a time bucket (e.g. "2026-06-10") or any
// other identifier. Its only purpose is to implement time sharding on the client side by producing
// an independent stream per value. The client must guarantee that, within a single shard value, all
// data is at most MaxChunkAge/2 older than the highest log-line timestamp already ingested for that
// value (otherwise entries are rejected as too far behind).
const HTTPHeaderBackfillShardKey = "X-Loki-Backfill-Shard"

// maxBackfillShardLen bounds the header value so it stays a reasonable stream label value.
const maxBackfillShardLen = 128

// backfillShardContextKey is used as a key for context values to avoid collisions.
type backfillShardContextKey int

const backfillShardKey backfillShardContextKey = 1

// ExtractAndValidateBackfillShard reads the X-Loki-Backfill-Shard header from r.
// It returns ("", false, nil) when the header is absent or empty. When the header is set it must be
// a valid, bounded-length Prometheus label value; otherwise it returns an error so the push is
// rejected with HTTP 400.
func ExtractAndValidateBackfillShard(r *http.Request) (string, bool, error) {
	shard := r.Header.Get(HTTPHeaderBackfillShardKey)
	if shard == "" {
		return "", false, nil
	}
	if len(shard) > maxBackfillShardLen {
		return "", false, fmt.Errorf("invalid %s header: value length %d exceeds limit of %d", HTTPHeaderBackfillShardKey, len(shard), maxBackfillShardLen)
	}
	if !model.LabelValue(shard).IsValid() {
		return "", false, fmt.Errorf("invalid %s header %q: must be a valid label value", HTTPHeaderBackfillShardKey, shard)
	}
	return shard, true, nil
}

// InjectBackfillShardContext returns a derived context carrying the validated backfill shard.
func InjectBackfillShardContext(ctx context.Context, shard string) context.Context {
	return context.WithValue(ctx, backfillShardKey, shard)
}

// ExtractBackfillShardContext returns the backfill shard stored in ctx, or "" if none is set.
func ExtractBackfillShardContext(ctx context.Context) string {
	shard, ok := ctx.Value(backfillShardKey).(string)
	if !ok {
		return ""
	}
	return shard
}
