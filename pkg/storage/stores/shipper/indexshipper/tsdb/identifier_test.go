package tsdb

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseSingleTenantTSDBPath(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		input string
		id    SingleTenantTSDBIdentifier
		ok    bool
	}{
		{
			desc:  "simple_works",
			input: "1-compactor-1-10-ff.tsdb",
			id: SingleTenantTSDBIdentifier{
				exportTSInSecs: true,
				TS:             time.Unix(1, 0),
				From:           1,
				Through:        10,
				Checksum:       255,
			},
			ok: true,
		},
		{
			desc:  "simple_works_with_nanosecond",
			input: "1712534400000000000-compactor-1-10-ff.tsdb",
			id: SingleTenantTSDBIdentifier{
				exportTSInSecs: false,
				TS:             time.Unix(0, 1712534400000000000),
				From:           1,
				Through:        10,
				Checksum:       255,
			},
			ok: true,
		},
		{
			desc:  "uint32_max_checksum_works",
			input: fmt.Sprintf("1-compactor-1-10-%x.tsdb", math.MaxUint32),
			id: SingleTenantTSDBIdentifier{
				exportTSInSecs: true,
				TS:             time.Unix(1, 0),
				From:           1,
				Through:        10,
				Checksum:       math.MaxUint32,
			},
			ok: true,
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
			id, ok := ParseSingleTenantTSDBPath(tc.input)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.id, id)
			if ok {
				require.Equal(t, tc.input, id.Name())
			}
		})
	}
}
