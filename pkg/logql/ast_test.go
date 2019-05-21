package logql

import (
	"strings"
	"testing"
)

func Test_logSelectorExpr_String(t *testing.T) {
	tests := []string{
		`{foo!~"bar"}`,
		`{foo="bar", bar!="baz"}`,
		`{foo="bar", bar!="baz"} != "bip" !~ ".+bop"`,
		`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap"`,
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt, func(t *testing.T) {
			expr, err := ParseLogSelector(tt)
			if err != nil {
				t.Fatalf("failed to parse log selector: %s", err)
			}
			if expr.String() != strings.Replace(tt, " ", "", -1) {
				t.Fatalf("error expected: %s got: %s", tt, expr.String())
			}
		})
	}
}
