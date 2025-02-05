package tail

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/deletion"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	// How long the Tailer should wait - once there are no entries to read from ingesters -
	// before checking if a new entry is available (to avoid spinning the CPU in a continuous
	// check loop)
	tailerWaitEntryThrottle = time.Second / 2
)

type Ingester interface {
	TailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error)
	TailersCount(ctx context.Context) ([]uint32, error)
	Tail(ctx context.Context, req *logproto.TailRequest) (map[string]logproto.Querier_TailClient, error)
}

type Store interface {
	SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error)
}

type Querier struct {
	ingester     Ingester
	store        Store
	limits       querier_limits.Limits
	deleteGetter deletion.DeleteGetter

	tailMaxDuration time.Duration
	metrics         *Metrics
	logger          log.Logger
}

func NewQuerier(ingester Ingester, store Store, deleteGetter deletion.DeleteGetter, limits querier_limits.Limits, tailMaxDuration time.Duration, metrics *Metrics, logger log.Logger) *Querier {
	return &Querier{
		ingester:        ingester,
		store:           store,
		deleteGetter:    deleteGetter,
		limits:          limits,
		tailMaxDuration: tailMaxDuration,
		metrics:         metrics,
		logger:          logger,
	}
}

// Tail keeps getting matching logs from all ingesters for given query
func (q *Querier) Tail(ctx context.Context, req *logproto.TailRequest, categorizedLabels bool) (*Tailer, error) {
	err := q.checkTailRequestLimit(ctx)
	if err != nil {
		return nil, err
	}

	if req.Plan == nil {
		parsed, err := syntax.ParseExpr(req.Query)
		if err != nil {
			return nil, err
		}
		req.Plan = &plan.QueryPlan{
			AST: parsed,
		}
	}

	deletes, err := deletion.DeletesForUserQuery(ctx, req.Start, time.Now(), q.deleteGetter)
	if err != nil {
		level.Error(spanlogger.FromContext(ctx)).Log("msg", "failed loading deletes for user", "err", err)
	}

	histReq := logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  req.Query,
			Start:     req.Start,
			End:       time.Now(),
			Limit:     req.Limit,
			Direction: logproto.BACKWARD,
			Deletes:   deletes,
			Plan:      req.Plan,
		},
	}

	histReq.Start, histReq.End, err = querier_limits.ValidateQueryRequest(ctx, histReq, q.limits)
	if err != nil {
		return nil, err
	}

	// Enforce the query timeout except when tailing, otherwise the tailing
	// will be terminated once the query timeout is reached
	tailCtx := ctx
	tenantID, err := tenant.TenantID(tailCtx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load tenant")
	}
	queryTimeout := q.limits.QueryTimeout(tailCtx, tenantID)
	queryCtx, cancelQuery := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancelQuery()

	tailClients, err := q.ingester.Tail(tailCtx, req)
	if err != nil {
		return nil, err
	}

	histIterators, err := q.store.SelectLogs(queryCtx, histReq)
	if err != nil {
		return nil, err
	}

	reversedIterator, err := iter.NewReversedIter(histIterators, req.Limit, true)
	if err != nil {
		return nil, err
	}

	return newTailer(
		time.Duration(req.DelayFor)*time.Second,
		tailClients,
		reversedIterator,
		func(connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
			return q.ingester.TailDisconnectedIngesters(tailCtx, req, connectedIngestersAddr)
		},
		q.tailMaxDuration,
		tailerWaitEntryThrottle,
		categorizedLabels,
		q.metrics,
		q.logger,
	), nil
}

func (q *Querier) checkTailRequestLimit(ctx context.Context) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	responses, err := q.ingester.TailersCount(ctx)
	// We are only checking active ingesters, and any error returned stops checking other ingesters
	// so return that error here as well.
	if err != nil {
		return err
	}

	var maxCnt uint32
	maxCnt = 0
	for _, resp := range responses {
		if resp > maxCnt {
			maxCnt = resp
		}
	}
	l := uint32(q.limits.MaxConcurrentTailRequests(ctx, userID))
	if maxCnt >= l {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max concurrent tail requests limit exceeded, count > limit (%d > %d)", maxCnt+1, l)
	}

	return nil
}
