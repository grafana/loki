package legacy

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/stretchr/testify/require"
)

var expectedStreamsValue = logql.Streams{
	&logproto.Stream{
		Entries: []logproto.Entry{
			logproto.Entry{
				Timestamp: time.Now(),
				Line:      "super line",
			},
		},
		Labels: "{test=\"test\"}",
	},
}

var expectedLabelsValue = logproto.LabelResponse{
	Values: []string{
		"label1",
		"test",
		"value",
	},
}

func init() {

}

func Test_WriteQueryResponseJSON(t *testing.T) {
	var b bytes.Buffer
	err := WriteQueryResponseJSON(expectedStreamsValue, &b)
	require.NoError(t, err)

	//unmarshal to a simple map and compare actual vs. expected
	var actualValue map[string]interface{}
	err = json.Unmarshal(b.Bytes(), &actualValue)
	require.NoError(t, err)

	streams, ok := actualValue["streams"].([]interface{})
	require.Truef(t, ok, "Failed to convert streams object")
	require.Equalf(t, len(expectedStreamsValue), len(streams), "Stream count difference")

	for i, stream := range streams {
		actualStream, ok := stream.(map[string]interface{})
		require.Truef(t, ok, "Failed to convert stream object")

		expectedStream := expectedStreamsValue[i]
		require.Equalf(t, expectedStream.Labels, actualStream["labels"], "Labels different on stream %d", i)

		entries, ok := actualStream["entries"].([]interface{})
		require.Truef(t, ok, "Failed to convert entries object on stream %d", i)
		require.Equalf(t, len(expectedStream.Entries), len(entries), "Entries count different on stream %d", i)

		for j, entry := range entries {
			actualEntry, ok := entry.(map[string]interface{})
			require.Truef(t, ok, "Failed to convert entry object on stream %d entry %d", i, j)

			expectedEntry := expectedStream.Entries[j]
			require.Equalf(t, expectedEntry.Line, actualEntry["line"], "Lines not equal on stream %d entry %d", i, j)
			require.Equalf(t, expectedEntry.Timestamp.Format(time.RFC3339Nano), actualEntry["ts"], "Timestamps not equal on stream %d entry %d", i, j)
		}
	}
}

func Test_WriteLabelResponseJSON(t *testing.T) {
	var b bytes.Buffer
	err := WriteLabelResponseJSON(expectedLabelsValue, &b)
	require.NoError(t, err)

	//unmarshal to a simple map and compare actual vs. expected
	var actualValue map[string]interface{}
	err = json.Unmarshal(b.Bytes(), &actualValue)
	require.NoError(t, err)

	values, ok := actualValue["values"].([]interface{})
	require.Truef(t, ok, "Failed to convert values object")
	require.Equalf(t, len(expectedLabelsValue.Values), len(values), "Value count difference")

	for i, value := range values {
		require.Equal(t, expectedLabelsValue.Values[i], value)
	}
}
