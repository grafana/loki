package logqltest

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// directExecutionStack runs queries straight through the v1 engine, with no query-frontend in
// front of it.
type directExecutionStack struct {
	t          *testing.T
	stackName  string
	limits     logql.Limits
	engineOpts logql.EngineOpts
	newQuerier newScriptQuerierFunc
	querier    logql.Querier

	// dataObjQueryStart is the time from which the querier reads stream-first queries from data
	// objects. It is zero when the querier reads no data objects.
	dataObjQueryStart time.Time
}

// newDirectTimestampFirstStack returns the direct stack in timestamp-first order, changed by opts.
func newDirectTimestampFirstStack(t *testing.T, stackName string, opts ...stackOption) *directExecutionStack {
	o := newStackOptions(opts...)
	return &directExecutionStack{t: t, stackName: stackName, limits: execLimits{maxQuerySeries: o.maxQuerySeries}, engineOpts: newEngineOpts(o), newQuerier: newChunkQuerier}
}

// newDirectStreamFirstStack returns the direct stack with stream-first execution enabled, changed
// by opts.
func newDirectStreamFirstStack(t *testing.T, stackName string, opts ...stackOption) *directExecutionStack {
	o := newStackOptions(opts...)
	return &directExecutionStack{t: t, stackName: stackName, limits: execLimits{streamFirstExecutionEnabled: true, maxQuerySeries: o.maxQuerySeries}, engineOpts: newEngineOpts(o), newQuerier: newChunkQuerier}
}

// newDirectDataObjStack returns the direct stack with stream-first execution enabled, changed by
// opts. The stack reads all the data of stream-first queries from data objects.
func newDirectDataObjStack(t *testing.T, stackName string, opts ...stackOption) *directExecutionStack {
	o := newStackOptions(opts...)
	return &directExecutionStack{
		t:                 t,
		stackName:         stackName,
		limits:            execLimits{streamFirstExecutionEnabled: true, maxQuerySeries: o.maxQuerySeries},
		engineOpts:        newEngineOpts(o),
		newQuerier:        newDataObjQuerierFunc(epoch),
		dataObjQueryStart: epoch,
	}
}

func (s *directExecutionStack) name() string {
	return s.stackName
}

func (*directExecutionStack) isQueryShardingSupported() bool {
	return false
}

func (s *directExecutionStack) isStreamFirstEnabled() bool {
	return s.limits.StreamFirstExecutionEnabled(tenant)
}

func (*directExecutionStack) isEvalSupported(evalCmd, expectations) bool {
	return true
}

func (s *directExecutionStack) setStores(stores *scriptStores) {
	s.querier = s.newQuerier(s.t, stores)
}

func (s *directExecutionStack) eval(cmd evalCmd) (logqlmodel.Result, error) {
	engine := logql.NewEngine(s.engineOpts, s.querier, s.limits, log.NewNopLogger())

	start, end, step := cmd.getTimeRange()
	params, err := logql.NewLiteralParams(
		cmd.query,
		epoch.Add(start), epoch.Add(end), step, 0,
		cmd.direction, 1000, nil, nil,
	)
	if err != nil {
		return logqlmodel.Result{}, err
	}

	// The stack has no query-frontend, which decides whether a querier may read data objects.
	// The stack marks the query eligible itself.
	if !s.dataObjQueryStart.IsZero() {
		params = params.WithDataObjOptions(&logproto.DataObjOptions{Eligible: true})
	}

	ctx := user.InjectOrgID(context.Background(), tenant)
	// Add flag to categorize labels. This is mimicking standard behavior of our most important client: Grafana
	ctx = httpreq.AddEncodingFlagsToContext(ctx, httpreq.NewEncodingFlags(httpreq.FlagCategorizeLabels))
	res, err := engine.Query(params).Exec(ctx)
	if err != nil {
		return res, err
	}

	return res, checkDataObjReads(cmd.query, res, s.dataObjQueryStart)
}
