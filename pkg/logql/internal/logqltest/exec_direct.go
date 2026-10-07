package logqltest

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// directExecutionStack runs queries straight through the v1 engine, with no query-frontend in
// front of it.
type directExecutionStack struct {
	t          *testing.T
	stackName  string
	limits     logql.Limits
	newQuerier newScriptQuerierFunc
	querier    logql.Querier

	// dataObjStart is the time from which the querier reads stream-first queries from data
	// objects. It is zero when the querier reads no data objects.
	dataObjStart time.Time
}

// newDirectTimestampFirstStack returns the direct stack in timestamp-first order.
func newDirectTimestampFirstStack(t *testing.T) *directExecutionStack {
	return &directExecutionStack{t: t, stackName: directTimestampFirstStackName, limits: execLimits{}, newQuerier: newChunkQuerier}
}

// newDirectStreamFirstStack returns the direct stack with stream-first execution enabled.
func newDirectStreamFirstStack(t *testing.T) *directExecutionStack {
	return &directExecutionStack{t: t, stackName: directStreamFirstStackName, limits: execLimits{streamFirstExecutionEnabled: true}, newQuerier: newChunkQuerier}
}

// newDirectDataObjStack returns the direct stack with stream-first execution enabled. The stack
// reads all the data of stream-first queries from data objects.
func newDirectDataObjStack(t *testing.T) *directExecutionStack {
	return &directExecutionStack{
		t:            t,
		stackName:    directDataObjStackName,
		limits:       execLimits{streamFirstExecutionEnabled: true},
		newQuerier:   newDataObjQuerierFunc(epoch),
		dataObjStart: epoch,
	}
}

func (s *directExecutionStack) name() string {
	return s.stackName
}

func (*directExecutionStack) isQueryShardingSupported() bool {
	return false
}

func (*directExecutionStack) isEvalSupported(evalCmd, expectations) bool {
	return true
}

func (s *directExecutionStack) setStores(stores *scriptStores) {
	s.querier = s.newQuerier(s.t, stores)
}

func (s *directExecutionStack) eval(cmd evalCmd) (logqlmodel.Result, error) {
	var opts logql.EngineOpts
	flagext.DefaultValues(&opts)
	engine := logql.NewEngine(opts, s.querier, s.limits, log.NewNopLogger())

	start, end, step := cmd.getTimeRange()
	params, err := logql.NewLiteralParams(
		cmd.query,
		epoch.Add(start), epoch.Add(end), step, 0,
		cmd.direction, 1000, nil, nil,
	)
	if err != nil {
		return logqlmodel.Result{}, err
	}

	ctx := user.InjectOrgID(context.Background(), tenant)
	// Add flag to categorize labels. This is mimicking standard behavior of our most important client: Grafana
	ctx = httpreq.AddEncodingFlagsToContext(ctx, httpreq.NewEncodingFlags(httpreq.FlagCategorizeLabels))
	res, err := engine.Query(params).Exec(ctx)
	if err != nil {
		return res, err
	}

	return res, checkDataObjReads(cmd.query, res, s.dataObjStart)
}
