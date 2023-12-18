package syntax

import (
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestLineFilterSerialization(t *testing.T) {
	for i, orig := range []LineFilter{
		{},
		{Ty: labels.MatchEqual, Match: "match"},
		{Ty: labels.MatchEqual, Match: "match", Op: "OR"},
		{Ty: labels.MatchNotEqual, Match: "not match"},
		{Ty: labels.MatchNotEqual, Match: "not match", Op: "OR"},
		{Ty: labels.MatchRegexp, Op: "OR"},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			b := make([]byte, orig.Size())
			_, err := orig.MarshalTo(b)
			require.NoError(t, err)
			t.Log(b)
			res := &LineFilter{}
			err = res.Unmarshal(b)
			require.NoError(t, err)
			t.Log(res)
			require.Equal(t, orig, *res)
		})
	}
}
