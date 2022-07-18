package logfmt

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogfmtExpressionParser(t *testing.T) {
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
			"field with spaces",
			`"field with spaces"`,
			[]interface{}{"field with spaces"},
			nil,
		},
		{
			"field with UTF9",
			`"field with ÜFT8👌"`,
			[]interface{}{"field with ÜFT8👌"},
			nil,
		},
		{
			"ip address",
			`"124.133.52.161"`,
			[]interface{}{"124.133.52.161"},
			nil,
		},
		{
			"invalid field with spaces",
			`field with spaces`,
			nil,
			fmt.Errorf("syntax error: unexpected FIELD"),
		},
		{ //remove this test case
			"missing closing double quote",
			`"missing closure`,
			[]interface{}{"missing closure"},
			nil,
		},
		{
			"identifier with number",
			`utf8`,
			[]interface{}{"utf8"},
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
