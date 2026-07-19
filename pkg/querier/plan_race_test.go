package querier

import (
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

func TestQuerier_SelectSamplesClonesPlanForIngesters(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}

	conf := mockQuerierConfig()
	conf.QueryIngestersWithin = 30 * time.Minute
	ingesterClient, _, q, err := setupIngesterQuerierMocks(conf, limits)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	expr := syntax.MustParseExpr(`sum by (this_is_a_much_longer_grouping_label, a) (rate({foo="bar"}[5m]))`)
	request := &logproto.SampleQueryRequest{
		Start: now.Add(-2 * time.Hour),
		End:   now,
		Plan:  &plan.QueryPlan{AST: expr},
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	it, err := q.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: request})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = it.Close() })

	calls := ingesterClient.GetMockedCallsByMethod("QuerySample")
	if len(calls) != 1 {
		t.Fatalf("got %d ingester calls, want 1", len(calls))
	}
	ingesterRequest, ok := calls[0].Arguments.Get(1).(*logproto.SampleQueryRequest)
	if !ok {
		t.Fatalf("ingester request has type %T", calls[0].Arguments.Get(1))
	}
	if ingesterRequest.Plan == request.Plan {
		t.Error("ingester and caller requests share the same query plan")
	}
	if !ingesterRequest.Plan.Equal(*request.Plan) {
		t.Error("ingester query plan differs from caller plan")
	}

	before, err := ingesterRequest.Plan.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	sampleExpr, ok := request.Plan.AST.(syntax.SampleExpr)
	if !ok {
		t.Fatalf("query plan AST has type %T, want syntax.SampleExpr", request.Plan.AST)
	}
	if _, err := sampleExpr.Extractors(); err != nil {
		t.Fatal(err)
	}
	after, err := ingesterRequest.Plan.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("mutating the caller plan changed the ingester plan")
	}

	vectorExpr, ok := request.Plan.AST.(*syntax.VectorAggregationExpr)
	if !ok {
		t.Fatalf("query plan AST has type %T, want *syntax.VectorAggregationExpr", request.Plan.AST)
	}

	// Exercise the overlap between a canceled ingester request finishing its
	// serialization and store-side evaluation mutating the caller's AST.
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
		for range iterations {
			vectorExpr.Grouping.Groups[0], vectorExpr.Grouping.Groups[1] = vectorExpr.Grouping.Groups[1], vectorExpr.Grouping.Groups[0]
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
