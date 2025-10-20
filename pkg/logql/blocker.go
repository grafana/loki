package logql

import (
	"context"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	logutil "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type queryBlocker struct {
	ctx    context.Context
	q      *query
	logger log.Logger
}

func newQueryBlocker(ctx context.Context, q *query) *queryBlocker {
	return &queryBlocker{
		ctx:    ctx,
		q:      q,
		logger: logutil.WithContext(ctx, q.logger),
	}
}

func (qb *queryBlocker) isBlocked(ctx context.Context, tenant string) bool {
	blocks := qb.q.limits.BlockedQueries(ctx, tenant)
	if len(blocks) <= 0 {
		return false
	}

	query := qb.q.params.QueryString()
	typ, err := QueryType(qb.q.params.GetExpression())
	if err != nil {
		typ = "unknown"
	}

	logger := log.With(qb.logger, "user", tenant, "type", typ)

	for _, b := range blocks {

		if b.Hash > 0 {
			if b.Hash == util.HashedQuery(query) {
				typesMatched, tagsMatched, blocked := qb.block(ctx, b, typ, logger)
				level.Warn(logger).Log("msg", "query blocker matched with hash policy", "hash", b.Hash, "query", query, "typesMatched", typesMatched, "tagsMatched", tagsMatched, "blocked", blocked)
				return blocked
			}

			return false
		}

		// if no pattern is given, assume we want to match all queries
		if b.Pattern == "" {
			b.Pattern = ".*"
			b.Regex = true
		}

		if strings.TrimSpace(b.Pattern) == strings.TrimSpace(query) {
			typesMatched, tagsMatched, blocked := qb.block(ctx, b, typ, logger)
			level.Warn(logger).Log("msg", "query blocker matched with exact match policy", "query", query, "typesMatched", typesMatched, "tagsMatched", tagsMatched, "blocked", blocked)
			return blocked
		}

		if b.Regex {
			r, err := regexp.Compile(b.Pattern)
			if err != nil {
				level.Error(logger).Log("msg", "query blocker regex does not compile", "pattern", b.Pattern, "err", err)
				continue
			}

			if r.MatchString(query) {
				typesMatched, tagsMatched, blocked := qb.block(ctx, b, typ, logger)
				level.Warn(logger).Log("msg", "query blocker matched with regex policy", "pattern", b.Pattern, "query", query, "typesMatched", typesMatched, "tagsMatched", tagsMatched, "blocked", blocked)
				return blocked
			}
		}
	}

	return false
}

func (qb *queryBlocker) block(ctx context.Context, q *validation.BlockedQuery, typ string, logger log.Logger) (bool, bool, bool) {
	// returns: (typesMatched, tagsMatched, blocked)
	// no specific types to validate against, so only tags (if any) need to match
	if len(q.Types) == 0 {
		tagsMatched := qb.tagsMatch(ctx, q, logger)
		return true, tagsMatched, tagsMatched
	}

	matched := false
	for _, qt := range q.Types {
		if qt == typ {
			matched = true
			break
		}
	}

	// query would be blocked, but it didn't match specified types
	if !matched {
		level.Debug(logger).Log("msg", "query blocker matched pattern, but not specified types", "pattern", q.Pattern, "regex", q.Regex, "hash", q.Hash, "types", q.Types.String(), "queryType", typ)
		return false, false, false
	}

	// Types matched; ensure tags (if any) also match
	tagsMatched := qb.tagsMatch(ctx, q, logger)
	return true, tagsMatched, tagsMatched
}

func (qb *queryBlocker) tagsMatch(ctx context.Context, q *validation.BlockedQuery, logger log.Logger) bool {
	// if no tags are expected, we treat all queries as matching
	if len(q.Tags) == 0 {
		return true
	}

	raw := httpreq.ExtractQueryTagsFromContext(ctx)
	// TagsToKeyValues is expected to always return an even set of key value pairs
	kvs := httpreq.TagsToKeyValues(raw)

	// Build a lowercased expected map once (size m) and scan kvs once (size n)
	expected := make(map[string]string, len(q.Tags))
	for k, v := range q.Tags {
		expected[strings.ToLower(k)] = v
	}

	// iterate over the keys in the context and see if they match the expected tags
	for i := 0; i+1 < len(kvs) && len(expected) > 0; i += 2 {
		k, okK := kvs[i].(string)
		v, okV := kvs[i+1].(string)
		if !okK || !okV {
			continue
		}

		keyLower := strings.ToLower(k)
		if expVal, ok := expected[keyLower]; ok {
			if strings.EqualFold(v, expVal) {
				// this key and value match, remove this key from the expected map of tags
				delete(expected, keyLower)
			}
		}
	}

	// if all expect tags matched, they would all have been removed from the map
	// we only block the query if all expected tags matched
	if len(expected) == 0 {
		return true
	}

	for k := range expected {
		level.Debug(logger).Log("msg", "query blocker tags mismatch: missing or mismatched key", "key", k, "tagsRaw", raw)
	}
	return false
}
