package query

import (
	"testing"

	"github.com/grafana/loki/pkg/logql"
)

func TestQuery(t *testing.T) {
	expr, err := logql.ParseExpr(`rate({cluster="dev-us-central-0", namespace="loki-dev-005", container="query-frontend"} |= "level=error"[1m])`)

	if err != nil {
		t.Fail()
	}

	se := expr.(logql.SampleExpr)

	ee, err := se.Extractor()
	if err != nil {
		t.Fail()
	}
	t.Logf("%v, %T, %v\n", ee, ee, se.Selector())
}
