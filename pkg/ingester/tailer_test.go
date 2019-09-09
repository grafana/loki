package ingester

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTailer_sendRaceConditionOnSendWhileClosing(t *testing.T) {
	runs := 100

	stream := logproto.Stream{
		Labels: `{type="test"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(int64(1), 0), Line: "line 1"},
			{Timestamp: time.Unix(int64(2), 0), Line: "line 2"},
		},
	}

	for run := 0; run < runs; run++ {
		tailer, err := newTailer("org-id", stream.Labels, nil)
		require.NoError(t, err)
		require.NotNil(t, tailer)

		routines := sync.WaitGroup{}
		routines.Add(2)

		go assert.NotPanics(t, func() {
			defer routines.Done()
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Microsecond)
			tailer.send(stream)
		})

		go assert.NotPanics(t, func() {
			defer routines.Done()
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Microsecond)
			tailer.close()
		})

		routines.Wait()
	}
}
