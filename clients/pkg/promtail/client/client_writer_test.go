package client

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/ingester/wal"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestClientWriter_LogEntriesAreReconstructedAndForwardedCorrectly(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	ch := make(chan api.Entry)
	defer close(ch)

	var receivedEntries []api.Entry

	go func() {
		for e := range ch {
			receivedEntries = append(receivedEntries, e)
		}
	}()

	var lines = []string{
		"some entry",
		"some other entry",
		"this is a song",
		"about entries",
		"I'm in a starbucks",
	}

	writeTo := newClientWriteTo(ch, logger)
	testAppLabelsRef := chunks.HeadSeriesRef(1)
	writeTo.StoreSeries([]record.RefSeries{
		{
			Ref: testAppLabelsRef,
			Labels: []labels.Label{
				{
					Name:  "app",
					Value: "test",
				},
			},
		},
	}, 1)

	for _, line := range lines {
		writeTo.AppendEntries(wal.RefEntries{
			Ref: testAppLabelsRef,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Now(),
					Line:      line,
				},
			},
		})
	}

	require.Eventually(t, func() bool {
		return len(receivedEntries) == len(lines)
	}, time.Second*10, time.Second)
	for _, receivedEntry := range receivedEntries {
		require.Contains(t, lines, receivedEntry.Line, "entry line was not expected")
		require.Equal(t, model.LabelValue("test"), receivedEntry.Labels["app"])
	}
}

func TestClientWriter_LogEntriesWithoutMatchingSeriesAreIgnored(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	ch := make(chan api.Entry)
	defer close(ch)

	var receivedEntries []api.Entry

	go func() {
		for e := range ch {
			receivedEntries = append(receivedEntries, e)
		}
	}()

	var lines = []string{
		"some entry",
		"some other entry",
		"this is a song",
		"about entries",
		"I'm in a starbucks",
	}

	writeTo := newClientWriteTo(ch, logger)
	testAppLabelsRef := chunks.HeadSeriesRef(1)
	writeTo.StoreSeries([]record.RefSeries{
		{
			Ref: testAppLabelsRef,
			Labels: []labels.Label{
				{
					Name:  "app",
					Value: "test",
				},
			},
		},
	}, 1)

	for _, line := range lines {
		writeTo.AppendEntries(wal.RefEntries{
			Ref: chunks.HeadSeriesRef(61324),
			Entries: []logproto.Entry{
				{
					Timestamp: time.Now(),
					Line:      line,
				},
			},
		})
	}

	time.Sleep(time.Second * 2)
	require.Empty(t, receivedEntries, "no entry should have arrived")
}
