package objectclient

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
)

func MustParseDayTime(s string) chunk.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return chunk.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func TestFSEncoder(t *testing.T) {
	schema := chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{
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
		UserID:      "fake",
		From:        MustParseDayTime("2020-01-02").Time,
		Through:     MustParseDayTime("2020-01-03").Time,
		Checksum:    123,
		Fingerprint: 456,
		ChecksumSet: true,
	}

	// chunk that resolves to v12
	newChunk := chunk.Chunk{
		UserID:      "fake",
		From:        MustParseDayTime("2022-01-02").Time,
		Through:     MustParseDayTime("2022-01-03").Time,
		Checksum:    123,
		Fingerprint: 456,
		ChecksumSet: true,
	}

	for _, tc := range []struct {
		desc string
		from string
		exp  string
	}{
		{
			desc: "before v12 encodes entire chunk",
			from: schema.ExternalKey(oldChunk),
			exp:  "ZmFrZS8xYzg6MTZmNjM4ZDQ0MDA6MTZmNjhiM2EwMDA6N2I=",
		},
		{
			desc: "v12+ encodes encodes the non-directory trail",
			from: schema.ExternalKey(newChunk),
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
