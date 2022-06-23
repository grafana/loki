package tsdb

import (
	"testing"

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
			input: "parent/fake/index-1-10-ff.tsdb",
			id: SingleTenantTSDBIdentifier{
				Tenant:   "fake",
				From:     1,
				Through:  10,
				Checksum: 255,
			},
			parent: "parent",
			ok:     true,
		},
		{
			desc:  "no tenant dir",
			input: "index-1-10-ff.tsdb",
			ok:    false,
		},
		{
			desc:  "wrong index name",
			input: "fake/notindex-1-10-ff.tsdb",
			ok:    false,
		},
		{
			desc:  "wrong argument len",
			input: "fake/index-10-ff.tsdb",
			ok:    false,
		},
		{
			desc:  "wrong argument encoding",
			input: "fake/index-ff-10-ff.tsdb",
			ok:    false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			id, parent, ok := parseSingleTenantTSDBPath(tc.input)
			require.Equal(t, tc.id, id)
			require.Equal(t, tc.parent, parent)
			require.Equal(t, tc.ok, ok)
		})
	}
}
