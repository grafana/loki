package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestEngine_IsMetricQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{
			name:  "log query",
			query: `{app="test"}`,
			want:  false,
		},
		{
			name:  "metric query",
			query: `rate({app="test"}[5m])`,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tt.query)
			require.NoError(t, err)
			require.Equal(t, tt.want, isMetricQuery(expr))
		})
	}
}
