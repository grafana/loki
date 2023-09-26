package logfmt

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogfmtExpressionParser(t *testing.T) {
	// `app=foo level=error spaces="value with ÜFT8👌" ts=2021-02-12T19:18:10.037940878Z`

	tests := []struct {
		name       string
		expression string
		want       []interface{}
		error      error
	}{
		{
			"single field",
			"app",
			[]interface{}{"app"},
			nil,
		},
		{
			"empty",
			``,
			nil,
			nil,
		},
		{
			"field with UTF8",
			"fieldwithÜFT8👌",
			nil,
			fmt.Errorf("unexpected char Ü"),
		},
		{
			"invalid field with spaces",
			`field with spaces`,
			nil,
			fmt.Errorf("syntax error: unexpected KEY"),
		},
		{
			"identifier with number",
			`id8`,
			[]interface{}{"id8"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := Parse(tt.expression, false)

			require.Equal(t, tt.want, parsed)

			if tt.error == nil {
				return
			}

			require.NotNil(t, err)
			require.Equal(t, tt.error.Error(), err.Error())
		})
	}

}
