package logql

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/pkg/logproto"

	"github.com/go-kit/log"
)

func TestBugWorking(t *testing.T) {
	NoLimits = &fakeLimits{maxSeries: math.MaxInt32}

	qs := `sum(count_over_time({app="foo"}[1m])) > 3`

	data := [][]logproto.Series{
		{newSeries(4, constant(70), `{app="foo",pool="foo"}`)},
	}

	params := []SelectSampleParams{
		{&logproto.SampleQueryRequest{Start: time.Unix(10, 0), End: time.Unix(70, 0), Selector: `sum(count_over_time({app="foo"}[1m]))`}},
	}

	querier := newQuerierRecorder(t, data, params)

	engine := NewEngine(EngineOpts{}, querier, NoLimits, log.NewNopLogger())
	start := time.Unix(70, 0)
	end := time.Unix(70, 0)
	direction := logproto.FORWARD
	limit := uint32(0)

	q := engine.Query(LiteralParams{qs: qs, start: start, end: end, direction: direction, limit: limit})
	res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("Return Data type:", res.Data.Type())
	fmt.Printf("%+v\n", res.Data)
}

func TestBugNotWorking(t *testing.T) {
	NoLimits = &fakeLimits{maxSeries: math.MaxInt32}

	qs := `3 < sum(count_over_time({app="foo"}[1m]))`

	data := [][]logproto.Series{
		{newSeries(4, constant(70), `{app="foo",pool="foo"}`)},
	}

	params := []SelectSampleParams{
		{&logproto.SampleQueryRequest{Start: time.Unix(10, 0), End: time.Unix(70, 0), Selector: `sum(count_over_time({app="foo"}[1m]))`}},
	}

	querier := newQuerierRecorder(t, data, params)

	engine := NewEngine(EngineOpts{}, querier, NoLimits, log.NewNopLogger())
	start := time.Unix(70, 0)
	end := time.Unix(70, 0)
	direction := logproto.FORWARD
	limit := uint32(0)

	q := engine.Query(LiteralParams{qs: qs, start: start, end: end, direction: direction, limit: limit})
	res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("Return Data type:", res.Data.Type())
	fmt.Printf("%+v\n", res.Data)
}

func TestUnderstanding(t *testing.T) {
	data := [][]logproto.Series{
		{newSeries(2, identity, `{app="foo"}`)},
	}

	fmt.Printf("%+v\n", data)
}
