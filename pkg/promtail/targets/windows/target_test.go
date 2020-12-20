//+build windows
package windows

import (
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/client/fake"
	"github.com/grafana/loki/pkg/promtail/targets/windows/win_eventlog"
	"github.com/prometheus/common/model"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
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
	ta, err := New(util.Logger, client, nil, Config{
		BoorkmarkPath: "c:\\foo.xml",
		PollInterval:  time.Microsecond,
		WinEventLog: win_eventlog.WinEventLog{
			EventlogName: "Application",
			Query: `<QueryList>
			<Query Id="0" Path="Application">
			  <Select Path="Application">*[System[Provider[@Name='mylog']]]</Select>
			</Query>
		  </QueryList>`,
		},
		Labels: model.LabelSet{"job": "windows"},
	})
	require.NoError(t, err)

	l.Error(1, "ERROR")

	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqual(client.Received(), []api.Entry{{Labels: model.LabelSet{"job": "windows"}}})
	}, 5*time.Second, 500*time.Millisecond)
	require.NoError(t, ta.Stop())

}
