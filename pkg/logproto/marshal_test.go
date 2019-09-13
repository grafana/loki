package logproto

import (
	"encoding/json"
	fmt "fmt"
	reflect "reflect"
	"testing"
	time "time"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

var (
	entries = []Entry{
		{
			Timestamp: time.Now(),
			Line:      "testline",
		},
		{
			Timestamp: time.Date(2019, 9, 10, 1, 1, 1, 1, time.UTC),
			Line:      "{}\"'!@$%&*^(_)(",
		},
	}
	streams = []Stream{
		{
			Labels:  "{}",
			Entries: []Entry{},
		},
		{
			Labels:  "{name=\"value\",name1=\"value1\"}",
			Entries: []Entry{},
		},
	}
)

func Test_EntryMarshalJSON(t *testing.T) {
	var array []interface{}

	for _, entry := range entries {

		bytes, err := entry.MarshalJSON()
		require.NoError(t, err)

		err = json.Unmarshal(bytes, &array)
		require.NoError(t, err)

		timestamp, ok := array[0].(string)
		require.True(t, ok)

		line, ok := array[1].(string)
		require.True(t, ok)

		require.Equal(t, fmt.Sprint(entry.Timestamp.UnixNano()), timestamp, "Timestamps not equal ", array[0])
		require.Equal(t, entry.Line, line, "Lines are not equal ", array[1])
	}
}

func Test_StreamMarshalJSON(t *testing.T) {
	actual := struct {
		Labels  map[string]string `json:"stream"`
		Entries []Entry           `json:"values"`
	}{}

	for _, expected := range streams {

		bytes, err := expected.MarshalJSON()
		require.NoError(t, err)

		err = json.Unmarshal(bytes, &actual)
		require.NoError(t, err)

		// check labels
		expectedLabels, err := promql.ParseMetric(expected.Labels)
		require.NoError(t, err)

		require.Equal(t, len(actual.Labels), len(expectedLabels))
		for _, l := range expectedLabels {
			require.Equal(t, l.Value, actual.Labels[l.Name])
		}

		// check entries
		require.True(t, reflect.DeepEqual(actual.Entries, expected.Entries))
	}
}
