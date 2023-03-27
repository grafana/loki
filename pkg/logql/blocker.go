package logql

import (
	"context"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/regexp"

	logutil "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/validation"
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
	patterns := qb.q.limits.BlockedQueries(ctx, tenant)
	if len(patterns) <= 0 {
		return false
	}

	typ, err := QueryType(qb.q.params.Query())
	if err != nil {
		typ = "unknown"
	}

	logger := log.With(qb.logger, "user", tenant, "type", typ)

	query := qb.q.params.Query()
	for _, p := range patterns {

		// if no pattern is given, assume we want to match all queries
		if p.Pattern == "" {
			p.Pattern = ".*"
			p.Regex = true
		}

		if strings.TrimSpace(p.Pattern) == strings.TrimSpace(query) {
			level.Warn(logger).Log("msg", "query blocker matched with exact match policy", "query", query)
			return qb.block(p, typ, logger)
		}

		if p.Regex {
			r, err := regexp.Compile(p.Pattern)
			if err != nil {
				level.Error(logger).Log("msg", "query blocker regex does not compile", "pattern", p.Pattern, "err", err)
				continue
			}

			if r.MatchString(query) {
				level.Warn(logger).Log("msg", "query blocker matched with regex policy", "pattern", p.Pattern, "query", query)
				return qb.block(p, typ, logger)
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
		level.Debug(logger).Log("msg", "query blocker matched pattern, but not specified types", "pattern", q.Pattern, "types", q.Types.String(), "queryType", typ)
		return false
	}

	return true
}
