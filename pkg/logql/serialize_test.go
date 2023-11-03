package logql

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
)

func TestJSONSerializationRoundTrip(t *testing.T) {
	query := `{env="prod", app=~"loki.*"}`

	root, err := syntax.ParseExpr(query)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = EncodeJSON(root, &buf)
	require.NoError(t, err)

	actual, err := DecodeJSON(buf.String())
	require.NoError(t, err)

	require.Equal(t, query, actual.String())
}
