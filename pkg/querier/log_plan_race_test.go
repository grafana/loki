package querier

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestQuerier_SelectLogsClonesPlanForIngesters(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}

	conf := mockQuerierConfig()
	conf.QueryIngestersWithin = 30 * time.Minute
	ingesterClient, store, q, err := setupIngesterQuerierMocks(conf, limits)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	expr := syntax.MustParseExpr(`{foo="bar"} |= "this_is_a_much_longer_filter_value" | json |= "b"`)
	request := &logproto.QueryRequest{
		Start:     now.Add(-2 * time.Hour),
		End:       now,
		Direction: logproto.FORWARD,
		Plan:      &plan.QueryPlan{AST: expr},
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	it, err := q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: request})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = it.Close() })

	ingesterCalls := ingesterClient.GetMockedCallsByMethod("Query")
	if len(ingesterCalls) != 1 {
		t.Fatalf("got %d ingester calls, want 1", len(ingesterCalls))
	}
	ingesterRequest, ok := ingesterCalls[0].Arguments.Get(1).(*logproto.QueryRequest)
	if !ok {
		t.Fatalf("ingester request has type %T", ingesterCalls[0].Arguments.Get(1))
	}
	if ingesterRequest.Plan == request.Plan {
		t.Error("ingester and caller requests share the same query plan")
	}
	if !ingesterRequest.Plan.Equal(*request.Plan) {
		t.Error("ingester query plan differs from caller plan")
	}

	beforeCaller, err := request.Plan.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	beforeIngester, err := ingesterRequest.Plan.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	storeCalls := store.GetMockedCallsByMethod("SelectLogs")
	if len(storeCalls) != 1 {
		t.Fatalf("got %d store calls, want 1", len(storeCalls))
	}
	storeParams, ok := storeCalls[0].Arguments.Get(1).(logql.SelectLogParams)
	if !ok {
		t.Fatalf("store params have type %T", storeCalls[0].Arguments.Get(1))
	}
	selector, err := storeParams.LogSelector()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selector.Pipeline(); err != nil {
		t.Fatal(err)
	}

	afterCaller, err := request.Plan.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(beforeCaller, afterCaller) {
		t.Error("building the store pipeline did not mutate the caller plan")
	}
	afterIngester, err := ingesterRequest.Plan.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeIngester, afterIngester) {
		t.Error("mutating the caller plan changed the ingester plan")
	}

	pipelineExpr, ok := request.Plan.AST.(*syntax.PipelineExpr)
	if !ok {
		t.Fatalf("query plan AST has type %T, want *syntax.PipelineExpr", request.Plan.AST)
	}
	if len(pipelineExpr.MultiStages) != 3 {
		t.Fatalf("query pipeline has %d stages, want 3", len(pipelineExpr.MultiStages))
	}
	firstFilter, ok := pipelineExpr.MultiStages[0].(*syntax.LineFilterExpr)
	if !ok {
		t.Fatalf("first pipeline stage has type %T, want *syntax.LineFilterExpr", pipelineExpr.MultiStages[0])
	}
	secondFilter, ok := pipelineExpr.MultiStages[2].(*syntax.LineFilterExpr)
	if !ok {
		t.Fatalf("third pipeline stage has type %T, want *syntax.LineFilterExpr", pipelineExpr.MultiStages[2])
	}

	// Exercise the overlap between a canceled ingester request finishing its
	// serialization and store-side pipeline construction mutating the caller's AST.
	const iterations = 10_000
	start := make(chan struct{})
	panicCh := make(chan any, 1)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				panicCh <- recovered
			}
		}()
		<-start
		for range iterations {
			if _, err := ingesterRequest.Marshal(); err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := range iterations {
			if i%2 == 0 {
				secondFilter.Left = nil
			} else {
				secondFilter.Left = firstFilter
			}
		}
	}()

	close(start)
	wg.Wait()

	select {
	case recovered := <-panicCh:
		t.Errorf("marshaling the ingester request panicked: %v", recovered)
	default:
	}
	select {
	case err := <-errCh:
		t.Errorf("marshaling the ingester request failed: %v", err)
	default:
	}
}
