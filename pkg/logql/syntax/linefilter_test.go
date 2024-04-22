package syntax

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
)

func TestLineFilterSerialization(t *testing.T) {
	for i, orig := range []LineFilter{
		{},
		{Ty: log.LineMatchEqual, Match: "match"},
		{Ty: log.LineMatchEqual, Match: "match", Op: "OR"},
		{Ty: log.LineMatchNotEqual, Match: "not match"},
		{Ty: log.LineMatchNotEqual, Match: "not match", Op: "OR"},
		{Ty: log.LineMatchPattern, Match: "match"},
		{Ty: log.LineMatchPattern, Match: "match", Op: "OR"},
		{Ty: log.LineMatchNotPattern, Match: "not match"},
		{Ty: log.LineMatchNotPattern, Match: "not match", Op: "OR"},
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
