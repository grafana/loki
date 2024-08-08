package logql

import (
	"context"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/regexp"

	"github.com/grafana/loki/v3/pkg/util"
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
				level.Warn(logger).Log("msg", "query blocker matched with hash policy", "hash", b.Hash, "query", query)
				return qb.block(b, typ, logger)
			}

			return false
		}

		// if no pattern is given, assume we want to match all queries
		if b.Pattern == "" {
			b.Pattern = ".*"
			b.Regex = true
		}

		if strings.TrimSpace(b.Pattern) == strings.TrimSpace(query) {
			level.Warn(logger).Log("msg", "query blocker matched with exact match policy", "query", query)
			return qb.block(b, typ, logger)
		}

		if b.Regex {
			r, err := regexp.Compile(b.Pattern)
			if err != nil {
				level.Error(logger).Log("msg", "query blocker regex does not compile", "pattern", b.Pattern, "err", err)
				continue
			}

			if r.MatchString(query) {
				level.Warn(logger).Log("msg", "query blocker matched with regex policy", "pattern", b.Pattern, "query", query)
				return qb.block(b, typ, logger)
			}
		}
	}

	return false
}

func (qb *queryBlocker) block(q *validation.BlockedQuery, typ string, logger log.Logger) bool {
	// no specific types to validate against, so query is blocked
	if len(q.Types) == 0 {
		return true
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
		return false
	}

	return true
}
