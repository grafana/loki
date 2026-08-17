package blocker

import (
	"context"
	"slices"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/regexp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	logutil "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type Limits interface {
	BlockedQueries(context.Context, string) []*validation.BlockedQuery
}

var queriesBlocked = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "loki_blocked_queries",
	Help: "count of queries blocked by per-tenant policy",
}, []string{"user"})

// Matches returns true if the query matches any of the tenant's query blocker
// policies.
func Matches(ctx context.Context, limits Limits, logger log.Logger, tenant, query, queryType string) bool {
	blocks := limits.BlockedQueries(ctx, tenant)
	if len(blocks) <= 0 {
		return false
	}

	logger = log.With(logutil.WithContext(ctx, logger), "user", tenant, "type", queryType)

	for _, b := range blocks {
		var (
			matched   bool
			matchKind string
			extra     []any
		)

		if b.Hash > 0 {
			if b.Hash == util.HashedQuery(query) {
				matched = true
				matchKind = "hash"
				extra = []any{"hash", b.Hash}
			}
		} else {
			// Use local copies to avoid mutating the shared config object.
			pattern := b.Pattern
			isRegex := b.Regex

			// if no pattern is given, assume we want to match all queries
			if pattern == "" {
				pattern = ".*"
				isRegex = true
			}

			if strings.TrimSpace(pattern) == strings.TrimSpace(query) {
				matched = true
				matchKind = "exact match"
			} else if isRegex {
				r, err := regexp.Compile(pattern)
				if err != nil {
					level.Error(logger).Log("msg", "query blocker regex does not compile", "pattern", pattern, "err", err)
					continue
				}
				if r.MatchString(query) {
					matched = true
					matchKind = "regex"
				}
			}

			if matched {
				extra = []any{"pattern", pattern}
			}
		}

		if !matched {
			continue
		}

		if !queryTypeMatch(b, queryType) {
			level.Debug(logger).Log("msg", "query blocker types mismatch", "types", b.Types, "queryType", queryType)
			continue
		}

		if !tagsMatch(ctx, b, logger) {
			level.Debug(logger).Log("msg", "query blocker tags mismatch", "tags", b.Tags)
			continue
		}

		logKeys := append([]any{
			"msg", "query blocker matched with " + matchKind + " policy",
			"query", query,
		}, extra...)
		level.Warn(logger).Log(logKeys...)

		queriesBlocked.WithLabelValues(tenant).Inc()
		return true
	}

	return false
}

// queryTypeMatch checks if the query type matches any of the types specified in the blocker definition.
func queryTypeMatch(bq *validation.BlockedQuery, queryType string) bool {
	if len(bq.Types) == 0 {
		return true
	}

	return slices.Contains(bq.Types, queryType)
}

// tagsMatch checks if the query tags match the tags specified in the blocker definition.
// All of the tags in the blocker definition must be present in the query tags and have the same value (case-insensitive).
func tagsMatch(ctx context.Context, q *validation.BlockedQuery, logger log.Logger) bool {
	if len(q.Tags) == 0 {
		return true
	}

	raw := httpreq.ExtractQueryTagsFromContext(ctx)
	// TagsToKeyValues is expected to always return an even set of key value pairs
	kvs := httpreq.TagsToKeyValues(raw)

	expected := make(map[string]string, len(q.Tags))
	for k, v := range q.Tags {
		expected[strings.ToLower(k)] = v
	}

	for i := 0; i+1 < len(kvs) && len(expected) > 0; i += 2 {
		k, okK := kvs[i].(string)
		v, okV := kvs[i+1].(string)
		if !okK || !okV {
			continue
		}

		keyLower := strings.ToLower(k)
		if expVal, ok := expected[keyLower]; ok {
			if strings.EqualFold(v, expVal) {
				delete(expected, keyLower)
			}
		}
	}

	if len(expected) == 0 {
		return true
	}

	for k := range expected {
		level.Debug(logger).Log("msg", "query blocker tags mismatch: missing or mismatched key", "key", k, "tagsRaw", raw)
	}
	return false
}
