package tsdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseSingleTenantTSDBPath(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		input  string
		id     SingleTenantTSDBIdentifier
		parent string
		ok     bool
	}{
		{
			desc:  "simple_works",
			input: "1-compactor-1-10-ff.tsdb",
			id: SingleTenantTSDBIdentifier{
				TS:       time.Unix(1, 0),
				From:     1,
				Through:  10,
				Checksum: 255,
			},
			parent: "parent",
			ok:     true,
		},
		{
			desc:  "wrong uploader name",
			input: "1-notcompactor-1-10-ff.tsdb",
			ok:    false,
		},
		{
			desc:  "wrong argument len",
			input: "1-compactor-10-ff.tsdb",
			ok:    false,
		},
		{
			desc:  "wrong argument encoding",
			input: "1-compactor-ff-10-ff.tsdb",
			ok:    false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			id, ok := parseSingleTenantTSDBPath(tc.input)
			require.Equal(t, tc.id, id)
			require.Equal(t, tc.ok, ok)
		})
	}
}
