package v1

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
	&logproto.Stream{
		Entries: []logproto.Entry{
			logproto.Entry{
				Timestamp: time.Now(),
				Line:      "super line",
			},
			logproto.Entry{
				Timestamp: time.Now().Add(300 * time.Second),
				Line:      "other line",
			},
		},
		Labels: "{test=\"test\",asdf=\"asdf\"}",
	},
	&logproto.Stream{
		Entries: []logproto.Entry{},
		Labels:  "{}",
	},
}

var expectedLabelsValue = logproto.LabelResponse{
	Values: []string{
		"label1",
		"test",
		"value",
	},
}

var expectedTailResponse = logproto.TailResponse{
	Stream: &logproto.Stream{
		Entries: []logproto.Entry{
			logproto.Entry{
				Timestamp: time.Now(),
				Line:      "super line",
			},
		},
		Labels: "{test=\"test\"}",
	},
	DroppedStreams: []*logproto.DroppedStream{
		&logproto.DroppedStream{
			From:   time.Now(),
			To:     time.Now().Add(20 * time.Millisecond),
			Labels: "{test=\"test\"}",
		},
		&logproto.DroppedStream{
			From:   time.Now(),
			To:     time.Now().Add(20 * time.Nanosecond),
			Labels: "{test=\"test\"}",
		},
	},
}

func init() {

}

func Test_WriteQueryResponseJSON(t *testing.T) {

}

//
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

func Test_MarshalTailResponse(t *testing.T) {
	// convert logproto to model objects
	model, err := NewTailResponse(expectedTailResponse)
	require.NoError(t, err)

	// marshal model object
	bytes, err := json.Marshal(model)
	require.NoError(t, err)

	var actualValue map[string]interface{}
	err = json.Unmarshal(bytes, &actualValue)
	require.NoError(t, err)

	stream, ok := actualValue["stream"].(map[string]interface{})
	require.Truef(t, ok, "Failed to convert stream object")
	testStream(t, expectedTailResponse.Stream, stream)

	droppedStreams, ok := actualValue["droppedStreams"].([]interface{})
	require.Truef(t, ok, "Failed to convert droppedStreams object")
	require.Equalf(t, len(expectedTailResponse.DroppedStreams), len(droppedStreams), "Dropped stream count difference")

	for i, droppedStream := range droppedStreams {
		actualDropped, ok := droppedStream.(map[string]interface{})
		require.Truef(t, ok, "Failed to convert droppedStream object")

		require.Equalf(t, expectedTailResponse.DroppedStreams[i].Labels, actualDropped["labels"], "Labels not equal on dropped stream %d", i)
		require.Equalf(t, expectedTailResponse.DroppedStreams[i].To.Format(time.RFC3339Nano), actualDropped["to"], "To not equal on dropped stream %d", i)
		require.Equalf(t, expectedTailResponse.DroppedStreams[i].From.Format(time.RFC3339Nano), actualDropped["from"], "From not equal on dropped stream %d", i)
	}
}

func testStream(t *testing.T, expectedValue *logproto.Stream, actualValue map[string]interface{}) {
	expectedStream := expectedValue
	require.Equalf(t, expectedStream.Labels, actualValue["labels"], "Labels different on stream")

	entries, ok := actualValue["entries"].([]interface{})
	require.Truef(t, ok, "Failed to convert entries object on stream")
	require.Equalf(t, len(expectedStream.Entries), len(entries), "Entries count different on stream")

	for j, entry := range entries {
		actualEntry, ok := entry.(map[string]interface{})
		require.Truef(t, ok, "Failed to convert entry object on entry %d", j)

		expectedEntry := expectedStream.Entries[j]
		require.Equalf(t, expectedEntry.Line, actualEntry["line"], "Lines not equal on stream %d", j)
		require.Equalf(t, expectedEntry.Timestamp.Format(time.RFC3339Nano), actualEntry["ts"], "Timestamps not equal on stream %d", j)
	}
}
