package main

import (
	"encoding/json"
	"fmt"

	"github.com/go-kit/kit/log/level"
	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/pkg/loghttp"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func compareStreams(expectedRaw, actualRaw json.RawMessage, tolerance float64) error {
	var expected, actual loghttp.Streams

	err := jsoniter.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return err
	}
	err = jsoniter.Unmarshal(actualRaw, &actual)
	if err != nil {
		return err
	}

	if len(expected) != len(actual) {
		return fmt.Errorf("expected %d streams but got %d", len(expected), len(actual))
	}

	streamLabelsToIndexMap := make(map[string]int, len(expected))
	for i, actualStream := range actual {
		streamLabelsToIndexMap[actualStream.Labels.String()] = i
	}

	for _, expectedStream := range expected {
		actualStreamIndex, ok := streamLabelsToIndexMap[expectedStream.Labels.String()]
		if !ok {
			return fmt.Errorf("expected stream %s missing from actual response", expectedStream.Labels)
		}

		actualStream := actual[actualStreamIndex]
		expectedValuesLen := len(expectedStream.Entries)
		actualValuesLen := len(actualStream.Entries)

		if expectedValuesLen != actualValuesLen {
			err := fmt.Errorf("expected %d values for stream %s but got %d", expectedValuesLen,
				expectedStream.Labels, actualValuesLen)
			if expectedValuesLen > 0 && actualValuesLen > 0 {
				level.Error(util_log.Logger).Log("msg", err.Error(), "oldest-expected-ts", expectedStream.Entries[0].Timestamp.UnixNano(),
					"newest-expected-ts", expectedStream.Entries[expectedValuesLen-1].Timestamp.UnixNano(),
					"oldest-actual-ts", actualStream.Entries[0].Timestamp.UnixNano(), "newest-actual-ts", actualStream.Entries[actualValuesLen-1].Timestamp.UnixNano())
			}
			return err
		}

		for i, expectedSamplePair := range expectedStream.Entries {
			actualSamplePair := actualStream.Entries[i]
			if !expectedSamplePair.Timestamp.Equal(actualSamplePair.Timestamp) {
				return fmt.Errorf("expected timestamp %v but got %v for stream %s", expectedSamplePair.Timestamp.UnixNano(),
					actualSamplePair.Timestamp.UnixNano(), expectedStream.Labels)
			}
			if expectedSamplePair.Line != actualSamplePair.Line {
				return fmt.Errorf("expected line %s for timestamp %v but got %s for stream %s", expectedSamplePair.Line,
					expectedSamplePair.Timestamp.UnixNano(), actualSamplePair.Line, expectedStream.Labels)
			}
		}
	}

	return nil
}
