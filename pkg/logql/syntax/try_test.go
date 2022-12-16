package syntax

import (
	"testing"
)

func FuzzParseExpr(f *testing.F) {
	f.Add(`{a="b"}`)
	f.Add(`{a="b", env!="test"}`)

	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseExpr(s)
	})

}
