package plan

import (
	"bytes"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
)

type QueryPlan struct {
	AST syntax.Expr
}

func (t QueryPlan) Marshal() ([]byte, error) {
	return t.MarshalJSON()
}

func (t *QueryPlan) MarshalTo(data []byte) (int, error) {
	appender := &appendWriter{
		slice: data[:0],
	}
	err := syntax.EncodeJSON(t.AST, appender)
	if err != nil {
		return 0, err
	}

	return len(appender.slice), nil
}

func (t *QueryPlan) Unmarshal(data []byte) error {
	return t.UnmarshalJSON(data)
}

func (t *QueryPlan) Size() int {
	counter := &countWriter{}
	err := syntax.EncodeJSON(t.AST, counter)
	if err != nil {
		return 0
	}

	return counter.bytes
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
	// An empty query plan is ingored to be backwards compatible.
	if len(data) == 0 {
		return nil
	}

	expr, err := syntax.DecodeJSON(string(data))
	if err != nil {
		return err
	}

	t.AST = expr
	return nil
}

func (t QueryPlan) Equal(other QueryPlan) bool {
	left, err := t.Marshal()
	if err != nil {
		return false
	}

	right, err := other.Marshal()
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

func (t QueryPlan) String() string {
	if t.AST == nil {
		return ""
	}
	return t.AST.String()
}

func (t *QueryPlan) Hash() uint32 {
	if t.AST == nil {
		return 0
	}
	return util.HashedQuery(t.AST.String())
}

// countWriter is not writing any bytes. It just counts the bytes that would be
// written.
type countWriter struct {
	bytes int
}

// Write implements io.Writer.
func (w *countWriter) Write(p []byte) (int, error) {
	w.bytes += len(p)
	return len(p), nil
}

// appendWriter appends to a slice.
type appendWriter struct {
	slice []byte
}

func (w *appendWriter) Write(p []byte) (int, error) {
	w.slice = append(w.slice, p...)
	return len(p), nil
}
