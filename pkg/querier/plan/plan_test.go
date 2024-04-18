package plan

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestMarshalTo(t *testing.T) {
	plan := QueryPlan{
		AST: syntax.MustParseExpr(`sum by (foo) (bytes_over_time({app="loki"} [1m]))`),
	}

	data := make([]byte, plan.Size())
	_, err := plan.MarshalTo(data)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = syntax.EncodeJSON(plan.AST, &buf)
	require.NoError(t, err)

	require.JSONEq(t, buf.String(), string(data))
}
