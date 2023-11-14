package querier

import (
	"bytes"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type QueryPlan struct {
	AST syntax.Expr
}

func (t QueryPlan) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	err := syntax.EncodeJSON(t.AST, &buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (t *QueryPlan) MarshalTo(data []byte) (n int, err error) {
}

func (t *QueryPlan) Unmarshal(data []byte) error {}
func (t *QueryPlan) Size() int {}

func (t QueryPlan) MarshalJSON() ([]byte, error) {}
func (t *QueryPlan) UnmarshalJSON(data []byte) error {}
