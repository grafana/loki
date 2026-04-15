package client

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func MustParseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func TestFSEncoder(t *testing.T) {
	schema := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:   MustParseDayTime("2020-01-01"),
				Schema: "v11",
			},
			{
				From:   MustParseDayTime("2022-01-01"),
				Schema: "v12",
			},
		},
	}

	// chunk that resolves to v11
	oldChunk := chunk.Chunk{
		ChunkRef: logproto.ChunkRef{
			UserID:      "fake",
			From:        MustParseDayTime("2020-01-02").Time,
			Through:     MustParseDayTime("2020-01-03").Time,
			Fingerprint: uint64(456),
			Checksum:    123,
		},
	}

	// chunk that resolves to v12
	newChunk := chunk.Chunk{
		ChunkRef: logproto.ChunkRef{
			UserID:      "fake",
			From:        MustParseDayTime("2022-01-02").Time,
			Through:     MustParseDayTime("2022-01-03").Time,
			Fingerprint: uint64(456),
			Checksum:    123,
		},
	}

	for _, tc := range []struct {
		desc string
		from string
		exp  string
	}{
		{
			desc: "before v12 encodes entire chunk",
			from: schema.ExternalKey(oldChunk.ChunkRef),
			exp:  "ZmFrZS8xYzg6MTZmNjM4ZDQ0MDA6MTZmNjhiM2EwMDA6N2I=",
		},
		{
			desc: "v12+ encodes encodes the non-directory trail",
			from: schema.ExternalKey(newChunk.ChunkRef),
			exp:  "fake/1c8/MTdlMTgxNWY4MDA6MTdlMWQzYzU0MDA6N2I=",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			chk, err := chunk.ParseExternalKey("fake", tc.from)
			require.Nil(t, err)
			require.Equal(t, tc.exp, FSEncoder(schema, chk))
		})
	}
}
