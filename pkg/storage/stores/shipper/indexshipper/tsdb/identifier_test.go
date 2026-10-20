package tsdb

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestParseSingleTenantTSDBPath(t *testing.T) {
	for _, tc := range []struct {
		desc         string
		input        string
		id           SingleTenantTSDBIdentifier
		ok           bool
		expectedName string
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
			desc:  "sharded_works",
			input: "1-compactor-1-10-ff-3_of_8.tsdb",
			id: SingleTenantTSDBIdentifier{
				ExportTSInSecs: true,
				TS:             time.Unix(1, 0),
				From:           1,
				Through:        10,
				Checksum:       255,
				Shard:          index.NewShard(3, 8),
			},
			ok: true,
		},
		{
			desc:  "sharded_works_with_nanosecond",
			input: "1712534400000000000-compactor-1-10-ff-0_of_2.tsdb",
			id: SingleTenantTSDBIdentifier{
				ExportTSInSecs: false,
				TS:             time.Unix(0, 1712534400000000000),
				From:           1,
				Through:        10,
				Checksum:       255,
				Shard:          index.NewShard(0, 2),
			},
			ok: true,
		},
		{
			desc:  "shard 0_of_1 is the unsharded identifier",
			input: "1-compactor-1-10-ff-0_of_1.tsdb",
			id: SingleTenantTSDBIdentifier{
				ExportTSInSecs: true,
				TS:             time.Unix(1, 0),
				From:           1,
				Through:        10,
				Checksum:       255,
			},
			ok:           true,
			expectedName: "1-compactor-1-10-ff.tsdb",
		},
		{
			desc:  "shard factor must be a power of two",
			input: "1-compactor-1-10-ff-1_of_3.tsdb",
			ok:    false,
		},
		{
			desc:  "shard must be below the factor",
			input: "1-compactor-1-10-ff-8_of_8.tsdb",
			ok:    false,
		},
		{
			desc:  "shard suffix must be well formed",
			input: "1-compactor-1-10-ff-3of8.tsdb",
			ok:    false,
		},
		{
			desc:  "shard suffix must not have trailing characters",
			input: "1-compactor-1-10-ff-3_of_8x.tsdb",
			ok:    false,
		},
		{
			desc:  "too many elements",
			input: "1-compactor-1-10-ff-3_of_8-extra.tsdb",
			ok:    false,
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
				expectedName := tc.input
				if tc.expectedName != "" {
					expectedName = tc.expectedName
				}
				require.Equal(t, expectedName, id.Name())
			}
		})
	}
}

func TestSingleTenantTSDBIdentifier_Shard(t *testing.T) {
	for _, tc := range []struct {
		desc            string
		shard           index.ShardAnnotation
		sharded         bool
		key             string
		from, through   model.Fingerprint
		matchesEveryone bool
	}{
		{desc: "zero value", sharded: false, key: "0_of_1", from: 0, through: math.MaxUint64},
		{desc: "factor 1", shard: index.NewShard(0, 1), sharded: false, key: "0_of_1", from: 0, through: math.MaxUint64},
		{desc: "first of 4", shard: index.NewShard(0, 4), sharded: true, key: "0_of_4", from: 0, through: 1 << 62},
		{desc: "last of 4", shard: index.NewShard(3, 4), sharded: true, key: "3_of_4", from: 3 << 62, through: math.MaxUint64},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			id := SingleTenantTSDBIdentifier{TS: time.Unix(1, 0), From: 1, Through: 10, Checksum: 255, Shard: tc.shard}
			require.Equal(t, tc.sharded, id.IsSharded())
			require.Equal(t, tc.key, id.ShardKey())
			from, through := id.FingerprintBounds()
			require.Equal(t, tc.from, from)
			require.Equal(t, tc.through, through)

			// the name round-trips through the parser, including the shard
			parsed, ok := ParseSingleTenantTSDBPath(id.Name())
			require.True(t, ok)
			require.Equal(t, id.Name(), parsed.Name())
			require.Equal(t, tc.sharded, parsed.IsSharded())
			require.Equal(t, tc.key, parsed.ShardKey())
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
