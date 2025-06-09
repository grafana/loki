package tsdb

import (
	"encoding/json"
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
				ExportTSInSecs: true,
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
				ExportTSInSecs: false,
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
				ExportTSInSecs: true,
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

func TestSingleTenantTSDBIdentifierSerialization(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		input SingleTenantTSDBIdentifier
	}{
		{
			desc:  "simple_works",
			input: SingleTenantTSDBIdentifier{ExportTSInSecs: true, TS: time.Unix(1, 0).UTC(), From: 1, Through: 10, Checksum: 255},
		},
		{
			desc:  "simple_works_with_nanosecond",
			input: SingleTenantTSDBIdentifier{ExportTSInSecs: false, TS: time.Unix(0, 1712534400000000000).UTC(), From: 1, Through: 10, Checksum: 255},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			b, err := json.Marshal(tc.input)
			require.NoError(t, err)

			var id SingleTenantTSDBIdentifier
			require.NoError(t, json.Unmarshal(b, &id))
			require.Equal(t, tc.input.Name(), id.Name())
			require.Equal(t, tc.input, id)
		})
	}
}
