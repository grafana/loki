//+build windows

package windows

import (
	"testing"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/server"
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/windows/win_eventlog"

	"github.com/grafana/loki/pkg/logproto"
)

func init() {
	fs = afero.NewMemMapFs()
	// Enable debug logging
	cfg := &server.Config{}
	_ = cfg.LogLevel.Set("debug")
	util_log.InitLogger(cfg)
}

// Test that you can use to generate event logs locally.
func Test_WriteLog(t *testing.T) {
	l, err := eventlog.Open("myapp")
	if err != nil {
		t.Fatalf("Open failed: %s", err)
	}
	l.Error(500, "hello 5 world")
}

func Test_GetCreateBookrmark(t *testing.T) {
	const name = "mylog"
	const supports = eventlog.Error | eventlog.Warning | eventlog.Info
	err := eventlog.InstallAsEventCreate(name, supports)
	if err != nil {
		t.Logf("Install failed: %s", err)
	}
	defer func() {
		err = eventlog.Remove(name)
		if err != nil {
			t.Fatalf("Remove failed: %s", err)
		}
	}()
	l, err := eventlog.Open(name)
	if err != nil {
		t.Fatalf("Open failed: %s", err)
	}
	client := fake.New(func() {})
	defer client.Stop()
	ta, err := New(util_log.Logger, client, nil, &scrapeconfig.WindowsEventsTargetConfig{
		BookmarkPath: "c:foo.xml",
		PollInterval: time.Microsecond,
		Query: `<QueryList>
			<Query Id="0" Path="Application">
			  <Select Path="Application">*[System[Provider[@Name='mylog']]]</Select>
			</Query>
		  </QueryList>`,
		Labels: model.LabelSet{"job": "windows-events"},
	})
	require.NoError(t, err)

	now := time.Now().String()
	l.Error(1, now)

	require.Eventually(t, func() bool {
		if len(client.Received()) > 0 {
			entry := client.Received()[0]
			var e Event
			if err := jsoniter.Unmarshal([]byte(entry.Line), &e); err != nil {
				t.Log(err)
				return false
			}
			return entry.Labels["job"] == "windows-events" && e.Message == now
		}
		return false
	}, 5*time.Second, 500*time.Millisecond)
	require.NoError(t, ta.Stop())

	now = time.Now().String()
	l.Error(1, now)

	client = fake.New(func() {})
	defer client.Stop()
	ta, err = New(util_log.Logger, client, nil, &scrapeconfig.WindowsEventsTargetConfig{
		BookmarkPath: "c:foo.xml",
		PollInterval: time.Microsecond,
		Query: `<QueryList>
			<Query Id="0" Path="Application">
			  <Select Path="Application">*[System[Provider[@Name='mylog']]]</Select>
			</Query>
		  </QueryList>`,
		Labels: model.LabelSet{"job": "windows-events"},
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		if len(client.Received()) > 0 {
			entry := client.Received()[0]
			var e Event
			if err := jsoniter.Unmarshal([]byte(entry.Line), &e); err != nil {
				t.Log(err)
				return false
			}
			return entry.Labels["job"] == "windows-events" && e.Message == now
		}
		return false
	}, 5*time.Second, 500*time.Millisecond)
	require.NoError(t, ta.Stop())
}

func Test_renderEntries(t *testing.T) {
	client := fake.New(func() {})
	defer client.Stop()
	ta, err := New(util_log.Logger, client, nil, &scrapeconfig.WindowsEventsTargetConfig{
		Labels:               model.LabelSet{"job": "windows-events"},
		EventlogName:         "Application",
		Query:                "*",
		UseIncomingTimestamp: true,
	})
	require.NoError(t, err)
	defer ta.Stop()
	entries := ta.renderEntries([]win_eventlog.Event{
		{
			Source:        win_eventlog.Provider{Name: "Application"},
			EventID:       10,
			Version:       10,
			Level:         10,
			Task:          10,
			Opcode:        10,
			Keywords:      "keywords",
			TimeCreated:   win_eventlog.TimeCreated{SystemTime: time.Unix(0, 1).UTC().Format(time.RFC3339Nano)},
			EventRecordID: 11,
			Correlation:   win_eventlog.Correlation{ActivityID: "some activity", RelatedActivityID: "some related activity"},
			Execution:     win_eventlog.Execution{ThreadID: 5, ProcessID: 1},
			Channel:       "channel",
			Computer:      "local",
			Security:      win_eventlog.Security{UserID: "1"},
			UserData:      win_eventlog.UserData{InnerXML: []byte(`userdata`)},
			EventData:     win_eventlog.EventData{InnerXML: []byte(`eventdata`)},
			Message:       "message",
		},
	})
	require.Equal(t, []api.Entry{
		{
			Labels: model.LabelSet{"channel": "channel", "computer": "local", "job": "windows-events"},
			Entry: logproto.Entry{
				Timestamp: time.Unix(0, 1).UTC(),
				Line:      `{"source":"Application","channel":"channel","computer":"local","event_id":10,"version":10,"level":10,"task":10,"opCode":10,"keywords":"keywords","timeCreated":"1970-01-01T00:00:00.000000001Z","eventRecordID":11,"correlation":{"activityID":"some activity","relatedActivityID":"some related activity"},"execution":{"processId":1,"threadId":5},"security":{"userId":"1"},"user_data":"eventdata","event_data":"eventdata","message":"message"}`,
			},
		},
	}, entries)
}
