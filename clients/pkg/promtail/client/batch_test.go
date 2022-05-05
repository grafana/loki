package client

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
)

func TestBatch_MaxStreams(t *testing.T) {
	maxStream := 2

	var inputEntries = []api.Entry{
		{Labels: model.LabelSet{"app": "app-1"}, Entry: logproto.Entry{Timestamp: time.Unix(4, 0).UTC(), Line: "line4"}},
		{Labels: model.LabelSet{"app": "app-2"}, Entry: logproto.Entry{Timestamp: time.Unix(5, 0).UTC(), Line: "line5"}},
		{Labels: model.LabelSet{"app": "app-3"}, Entry: logproto.Entry{Timestamp: time.Unix(6, 0).UTC(), Line: "line6"}},
		{Labels: model.LabelSet{"app": "app-4"}, Entry: logproto.Entry{Timestamp: time.Unix(6, 0).UTC(), Line: "line6"}},
	}

	b := newBatch(NoopWAL, maxStream)

	errCount := 0
	for _, entry := range inputEntries {
		err := b.add(entry)
		if err != nil {
			errCount++
			assert.EqualError(t, err, fmt.Errorf(errMaxStreamsLimitExceeded, len(b.streams), b.maxStreams, entry.Labels).Error())
		}
	}
	assert.Equal(t, errCount, 2)
}

func TestBatch_add(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		inputEntries      []api.Entry
		expectedSizeBytes int
	}{
		"empty batch": {
			inputEntries:      []api.Entry{},
			expectedSizeBytes: 0,
		},
		"single stream with single log entry": {
			inputEntries: []api.Entry{
				{Labels: model.LabelSet{}, Entry: logEntries[0].Entry},
			},
			expectedSizeBytes: len(logEntries[0].Entry.Line),
		},
		"single stream with multiple log entries": {
			inputEntries: []api.Entry{
				{Labels: model.LabelSet{}, Entry: logEntries[0].Entry},
				{Labels: model.LabelSet{}, Entry: logEntries[1].Entry},
			},
			expectedSizeBytes: len(logEntries[0].Entry.Line) + len(logEntries[1].Entry.Line),
		},
		"multiple streams with multiple log entries": {
			inputEntries: []api.Entry{
				{Labels: model.LabelSet{"type": "a"}, Entry: logEntries[0].Entry},
				{Labels: model.LabelSet{"type": "a"}, Entry: logEntries[1].Entry},
				{Labels: model.LabelSet{"type": "b"}, Entry: logEntries[2].Entry},
			},
			expectedSizeBytes: len(logEntries[0].Entry.Line) + len(logEntries[1].Entry.Line) + len(logEntries[2].Entry.Line),
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			b := newBatch(NoopWAL, 0)

			for _, entry := range testData.inputEntries {
				err := b.add(entry)
				assert.NoError(t, err)
			}

			assert.Equal(t, testData.expectedSizeBytes, b.sizeBytes())
		})
	}
}

func TestBatch_encode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		inputBatch           func() *batch
		expectedEntriesCount int
	}{
		"empty batch": {
			inputBatch:           func() *batch { return newBatch(NoopWAL, 0) },
			expectedEntriesCount: 0,
		},
		"single stream with single log entry": {
			inputBatch: func() *batch {
				b := newBatch(
					NoopWAL, 0,
				)
				b.add(api.Entry{Labels: model.LabelSet{}, Entry: logEntries[0].Entry})
				return b
			},
			expectedEntriesCount: 1,
		},
		"single stream with multiple log entries": {
			inputBatch: func() *batch {
				b := newBatch(
					NoopWAL, 0,
				)
				b.add(api.Entry{Labels: model.LabelSet{}, Entry: logEntries[0].Entry})
				b.add(api.Entry{Labels: model.LabelSet{}, Entry: logEntries[1].Entry})
				return b
			},
			expectedEntriesCount: 2,
		},
		"multiple streams with multiple log entries": {
			inputBatch: func() *batch {
				b := newBatch(
					NoopWAL, 0,
				)
				b.add(api.Entry{Labels: model.LabelSet{"type": "a"}, Entry: logEntries[0].Entry})
				b.add(api.Entry{Labels: model.LabelSet{"type": "a"}, Entry: logEntries[1].Entry})
				b.add(api.Entry{Labels: model.LabelSet{"type": "b"}, Entry: logEntries[2].Entry})
				return b
			},
			expectedEntriesCount: 3,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			_, entriesCount, err := testData.inputBatch().encode()
			require.NoError(t, err)
			assert.Equal(t, testData.expectedEntriesCount, entriesCount)
		})
	}
}

func TestHashCollisions(t *testing.T) {
	b := newBatch(NoopWAL, 0)

	ls1 := model.LabelSet{"app": "l", "uniq0": "0", "uniq1": "1"}
	ls2 := model.LabelSet{"app": "m", "uniq0": "1", "uniq1": "1"}

	require.False(t, ls1.Equal(ls2))
	require.Equal(t, ls1.FastFingerprint(), ls2.FastFingerprint())

	const entriesPerLabel = 10

	for i := 0; i < entriesPerLabel; i++ {
		_ = b.add(api.Entry{Labels: ls1, Entry: logproto.Entry{Timestamp: time.Now(), Line: fmt.Sprintf("line %d", i)}})

		_ = b.add(api.Entry{Labels: ls2, Entry: logproto.Entry{Timestamp: time.Now(), Line: fmt.Sprintf("line %d", i)}})
	}

	// make sure that colliding labels are stored properly as independent streams
	req, entries := b.createPushRequest()
	assert.Len(t, req.Streams, 2)
	assert.Equal(t, 2*entriesPerLabel, entries)

	if req.Streams[0].Labels == ls1.String() {
		assert.Equal(t, ls1.String(), req.Streams[0].Labels)
		assert.Equal(t, ls2.String(), req.Streams[1].Labels)
	} else {
		assert.Equal(t, ls2.String(), req.Streams[0].Labels)
		assert.Equal(t, ls1.String(), req.Streams[1].Labels)
	}
}
