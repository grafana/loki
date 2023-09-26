package compactor

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

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
