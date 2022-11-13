package syntax

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrettify(t *testing.T) {
	maxCharsPerLine = 20

	cases := []struct {
		name string
		in   string
		exp  string
	}{
		{
			name: "basic stream selector",
			in:   `{job="loki", instance="localhost"}`,
			exp:  `{job="loki", instance="localhost"}`,
		},
		{
			name: "pipeline",
			in:   `{job="loki", instance="localhost"}|logfmt `,
			exp: `{job="loki", instance="localhost"}
 | logfmt`,
		},
		{
			name: "aggregation",
			in:   `count_over_time({job="loki", instance="localhost"}|logfmt[1m])`,
			exp: `count_over_time(
 {job="loki", instance="localhost"}
  | logfmt
 [1m]
)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			fmt.Printf("%#v\n", expr)
			require.NoError(t, err)
			got := Prettify(expr)
			assert.Equal(t, c.exp, got)
		})
	}
}
