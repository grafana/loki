package querytee

import (
	"errors"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

const (
	selectionStrategyDataObj = "dataobj"

	// Values of the decision label that the dataobj strategy sets itself. reasonUnsupportedQuery
	// has the value of the frontend outcome for a query that is not a stream-first sum. The
	// strategy also uses it for a query that does not parse. reasonUnsupportedRequest has no
	// frontend equivalent.
	reasonUnsupportedQuery   = string(queryrange.DataObjOutcomeUnsupportedQuery)
	reasonUnsupportedRequest = "unsupported_request"
)

var errUnsupportedRequest = errors.New("the request is not a range or instant query")

// dataObjStrategy selects a range or instant query when the query-frontend of the secondary backend
// would read it from data objects. It runs the frontend eligibility check with the limits of the
// secondary backend's Loki cluster. It selects no other request. It is safe for concurrent use.
type dataObjStrategy struct {
	cfg        DataObjConfig
	lokiLimits *LokiLimits
	now        func() time.Time
}

func newDataObjStrategy(cfg DataObjConfig, lokiLimits *LokiLimits) (*dataObjStrategy, error) {
	if lokiLimits == nil {
		return nil, errors.New("the dataobj selection strategy needs the Loki config of the secondary backend: set -config.file and, for per-tenant limits, -runtime-config.file")
	}
	if lokiLimits.Overrides == nil {
		return nil, errors.New("the dataobj selection strategy needs the tenant limits")
	}
	return &dataObjStrategy{cfg: cfg, lokiLimits: lokiLimits, now: time.Now}, nil
}

// Select implements SelectionStrategy.
func (s *dataObjStrategy) Select(tenants []string, req queryrangebase.Request) Selection {
	expr, err := requestExpr(req)
	if errors.Is(err, errUnsupportedRequest) {
		return Selection{Selected: false, Reason: reasonUnsupportedRequest}
	}
	if err != nil {
		return Selection{Selected: false, Reason: reasonUnsupportedQuery}
	}

	res := queryrange.ResolveDataObj(s.cfg.Enabled, s.cfg.StorageLag, s.lokiLimits.Overrides, tenants, expr, req.GetStart(), req.GetEnd(), s.now)
	if !res.Eligible() {
		return Selection{Selected: false, Reason: string(res.Outcome)}
	}
	return Selection{Selected: true}
}

// requestExpr returns the parsed query of a range or instant request. It returns
// errUnsupportedRequest for any other request. The codec sets the plan of every range and instant
// query it decodes. The parse is a fallback for a request built without a plan.
func requestExpr(req queryrangebase.Request) (syntax.Expr, error) {
	var queryPlan *plan.QueryPlan
	switch r := req.(type) {
	case *queryrange.LokiRequest:
		queryPlan = r.Plan
	case *queryrange.LokiInstantRequest:
		queryPlan = r.Plan
	default:
		return nil, errUnsupportedRequest
	}
	if queryPlan != nil && queryPlan.AST != nil {
		return queryPlan.AST, nil
	}
	return syntax.ParseExpr(req.GetQuery())
}
