package logproto

import (
	"encoding/json"
	"testing"
	time "time"

	"github.com/stretchr/testify/require"
)

var (
	entries = []Entry{
		Entry{
			Timestamp: time.Now(),
			Line:      "testline",
		},
		Entry{
			Timestamp: time.Date(2019, 9, 10, 1, 1, 1, 1, time.UTC),
			Line:      "{}\"'!@$%&*^(_)(",
		},
	}
)

func Test_EntryMarshalJSON(t *testing.T) {

	var array []interface{}

	for _, entry := range entries {

		bytes, err := entry.MarshalJSON()
		require.NoError(t, err)

		err = json.Unmarshal(bytes, &array)

		timestamp, ok := array[0].(float64)
		require.True(t, ok)

		line, ok := array[1].(string)
		require.True(t, ok)

		// only test to the microsecond level.  json's number type (float64) does not have enough precision to store nanoseconds
		require.Equal(t, entry.Timestamp.UnixNano()/int64(time.Microsecond), int64(timestamp*1e9)/int64(time.Microsecond), "Timestamps not equal ", array[0])
		require.Equal(t, entry.Line, line, "Lines are not equal ", array[1])
	}
}
