package plan

import (
	"bytes"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type QueryPlan struct {
	AST syntax.Expr
}

func (t QueryPlan) Marshal() ([]byte, error) {
	return t.MarshalJSON()
}

func (t *QueryPlan) MarshalTo(data []byte) (n int, err error) {
	// TODO: we probably want to write to data directly
	src, err := t.Marshal()
	if err != nil {
		return 0, err
	}

	return copy(src, data), nil
}

func (t *QueryPlan) Unmarshal(data []byte) error {
	return t.UnmarshalJSON(data)
}

func (t *QueryPlan) Size() int {

	// TODO: we probably want to calculate the size directly.
	src, _ := t.Marshal()

	return len(src)
}

func (t QueryPlan) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	err := syntax.EncodeJSON(t.AST, &buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (t *QueryPlan) UnmarshalJSON(data []byte) error {
	expr, err := syntax.DecodeJSON(string(data))
	if err != nil {
		return err
	}

	t.AST = expr
	return nil
}

func (t QueryPlan) Equal(other QueryPlan) bool {
	return syntax.Equal(t.AST, other.AST)
}
