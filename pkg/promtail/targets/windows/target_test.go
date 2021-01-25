//+build windows
package windows

import (
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/grafana/loki/pkg/promtail/client/fake"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/server"
	"golang.org/x/sys/windows/svc/eventlog"

	"testing"
)

func init() {
	fs = afero.NewMemMapFs()
	// Enable debug logging
	cfg := &server.Config{}
	_ = cfg.LogLevel.Set("debug")
	util.InitLogger(cfg)
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
	ta, err := New(util.Logger, client, nil, &scrapeconfig.WindowsEventsTargetConfig{
		BoorkmarkPath: "c:\\foo.xml",
		PollInterval:  time.Microsecond,
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
			t.Log(entry)
			return entry.Labels["job"] == "windows-events" && e.Message == now
		}
		return false
	}, 5*time.Second, 500*time.Millisecond)
	require.NoError(t, ta.Stop())

	now = time.Now().String()
	l.Error(1, now)

	client = fake.New(func() {})
	defer client.Stop()
	ta, err = New(util.Logger, client, nil, &scrapeconfig.WindowsEventsTargetConfig{
		BoorkmarkPath: "c:\\foo.xml",
		PollInterval:  time.Microsecond,
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
			t.Log(entry)
			return entry.Labels["job"] == "windows-events" && e.Message == now
		}
		return false
	}, 5*time.Second, 500*time.Millisecond)
	require.NoError(t, ta.Stop())

}
