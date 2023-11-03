package logql

import (
	"bytes"
	"testing"

	jsoniter "github.com/json-iterator/go"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
)

func TestJSONSerializationRoundTrip(t *testing.T) {
	query := `{env="prod", app=~"loki.*"}`

	root, err := syntax.ParseExpr(query)
	require.NoError(t, err)

	var buf bytes.Buffer
	visitor := NewJSONSerializer(jsoniter.NewStream(jsoniter.ConfigDefault, &buf, 1024))

	err = syntax.Dispatch(root, visitor)
	require.NoError(t, err)

	require.JSONEq(t, `{}`, buf.String())
}
