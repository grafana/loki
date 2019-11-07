package client

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatch_add(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		inputEntries      []entry
		expectedSizeBytes int
	}{
		"empty batch": {
			inputEntries:      []entry{},
			expectedSizeBytes: 0,
		},
		"single stream with single log entry": {
			inputEntries: []entry{
				{"tenant", model.LabelSet{}, logEntries[0].Entry},
			},
			expectedSizeBytes: len(logEntries[0].Entry.Line),
		},
		"single stream with multiple log entries": {
			inputEntries: []entry{
				{"tenant", model.LabelSet{}, logEntries[0].Entry},
				{"tenant", model.LabelSet{}, logEntries[1].Entry},
			},
			expectedSizeBytes: len(logEntries[0].Entry.Line) + len(logEntries[1].Entry.Line),
		},
		"multiple streams with multiple log entries": {
			inputEntries: []entry{
				{"tenant", model.LabelSet{"type": "a"}, logEntries[0].Entry},
				{"tenant", model.LabelSet{"type": "a"}, logEntries[1].Entry},
				{"tenant", model.LabelSet{"type": "b"}, logEntries[2].Entry},
			},
			expectedSizeBytes: len(logEntries[0].Entry.Line) + len(logEntries[1].Entry.Line) + len(logEntries[2].Entry.Line),
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			b := newBatch()

			for _, entry := range testData.inputEntries {
				b.add(entry)
			}

			assert.Equal(t, testData.expectedSizeBytes, b.sizeBytes())
		})
	}
}

func TestBatch_encode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		inputBatch           *batch
		expectedEntriesCount int
	}{
		"empty batch": {
			inputBatch:           newBatch(),
			expectedEntriesCount: 0,
		},
		"single stream with single log entry": {
			inputBatch: newBatch(
				entry{"tenant", model.LabelSet{}, logEntries[0].Entry},
			),
			expectedEntriesCount: 1,
		},
		"single stream with multiple log entries": {
			inputBatch: newBatch(
				entry{"tenant", model.LabelSet{}, logEntries[0].Entry},
				entry{"tenant", model.LabelSet{}, logEntries[1].Entry},
			),
			expectedEntriesCount: 2,
		},
		"multiple streams with multiple log entries": {
			inputBatch: newBatch(
				entry{"tenant", model.LabelSet{"type": "a"}, logEntries[0].Entry},
				entry{"tenant", model.LabelSet{"type": "a"}, logEntries[1].Entry},
				entry{"tenant", model.LabelSet{"type": "b"}, logEntries[2].Entry},
			),
			expectedEntriesCount: 3,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			_, entriesCount, err := testData.inputBatch.encode()
			require.NoError(t, err)
			assert.Equal(t, testData.expectedEntriesCount, entriesCount)
		})
	}
}
