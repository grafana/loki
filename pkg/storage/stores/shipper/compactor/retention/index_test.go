package retention

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
)

func Test_schemaPeriodForTable(t *testing.T) {
	indexFromTime := func(t time.Time) string {
		return fmt.Sprintf("%d", t.Unix()/int64(24*time.Hour/time.Second))
	}
	tests := []struct {
		name          string
		config        storage.SchemaConfig
		tableName     string
		expected      chunk.PeriodConfig
		expectedFound bool
	}{
		{"out of scope", schemaCfg, "index_" + indexFromTime(start.Time().Add(-24*time.Hour)), chunk.PeriodConfig{}, false},
		{"first table", schemaCfg, "index_" + indexFromTime(dayFromTime(start).Time.Time()), schemaCfg.Configs[0], true},
		{"4 hour after first table", schemaCfg, "index_" + indexFromTime(dayFromTime(start).Time.Time().Add(4*time.Hour)), schemaCfg.Configs[0], true},
		{"second schema", schemaCfg, "index_" + indexFromTime(dayFromTime(start.Add(28*time.Hour)).Time.Time()), schemaCfg.Configs[1], true},
		{"third schema", schemaCfg, "index_" + indexFromTime(dayFromTime(start.Add(75*time.Hour)).Time.Time()), schemaCfg.Configs[2], true},
		{"now", schemaCfg, "index_" + indexFromTime(time.Now()), schemaCfg.Configs[3], true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, actualFound := schemaPeriodForTable(tt.config, tt.tableName)
			require.Equal(t, tt.expected, actual)
			require.Equal(t, tt.expectedFound, actualFound)
		})
	}
}

func TestParseChunkID(t *testing.T) {
	type resp struct {
		userID        string
		from, through int64
		valid         bool
	}
	for _, tc := range []struct {
		name         string
		chunkID      string
		expectedResp resp
	}{
		{
			name:    "older than v12 chunk format",
			chunkID: "fake/57f628c7f6d57aad:162c699f000:162c69a07eb:eb242d99",
			expectedResp: resp{
				userID:  "fake",
				from:    1523750400000,
				through: 1523750406123,
				valid:   true,
			},
		},
		{
			name:    "v12+ chunk format",
			chunkID: "fake/57f628c7f6d57aad/162c699f000:162c69a07eb:eb242d99",
			expectedResp: resp{
				userID:  "fake",
				from:    1523750400000,
				through: 1523750406123,
				valid:   true,
			},
		},
		{
			name:    "invalid format",
			chunkID: "fake:57f628c7f6d57aad:162c699f000:162c69a07eb:eb242d99",
			expectedResp: resp{
				valid: false,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID, hexFrom, hexThrough, valid := parseChunkID([]byte(tc.chunkID))
			if !tc.expectedResp.valid {
				require.False(t, valid)
			} else {
				require.Equal(t, tc.expectedResp.userID, string(userID))
				from, err := strconv.ParseInt(unsafeGetString(hexFrom), 16, 64)
				require.NoError(t, err)
				require.Equal(t, tc.expectedResp.from, from)

				through, err := strconv.ParseInt(unsafeGetString(hexThrough), 16, 64)
				require.NoError(t, err)
				require.Equal(t, tc.expectedResp.through, through)
			}
		})
	}
}
