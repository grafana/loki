package stdin

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/client/fake"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
)

func Test_newReaderTarget(t *testing.T) {
	tests := []struct {
		name    string
		in      io.Reader
		cfg     scrapeconfig.Config
		want    []api.Entry
		wantErr bool
	}{
		{
			"no newlines",
			bytes.NewReader([]byte("bar")),
			scrapeconfig.Config{},
			[]api.Entry{
				{Labels: model.LabelSet{}, Entry: logproto.Entry{Line: "bar"}},
			},
			false,
		},
		{
			"empty",
			bytes.NewReader([]byte("")),
			scrapeconfig.Config{},
			nil,
			false,
		},
		{
			"newlines",
			bytes.NewReader([]byte("\nfoo\r\nbar")),
			scrapeconfig.Config{},
			[]api.Entry{
				{Labels: model.LabelSet{}, Entry: logproto.Entry{Line: "foo"}},
				{Labels: model.LabelSet{}, Entry: logproto.Entry{Line: "bar"}},
			},
			false,
		},
		{
			"pipeline",
			bytes.NewReader([]byte("\nfoo\r\nbar")),
			scrapeconfig.Config{
				PipelineStages: loadConfig(stagesConfig),
			},
			[]api.Entry{
				{Labels: model.LabelSet{"new_key": "hello world!"}, Entry: logproto.Entry{Line: "foo"}},
				{Labels: model.LabelSet{"new_key": "hello world!"}, Entry: logproto.Entry{Line: "bar"}},
			},
			false,
		},
		{
			"default config",
			bytes.NewReader([]byte("\nfoo\r\nbar")),
			defaultStdInCfg,
			[]api.Entry{
				{Labels: model.LabelSet{"job": "stdin", "hostname": model.LabelValue(hostName)}, Entry: logproto.Entry{Line: "foo"}},
				{Labels: model.LabelSet{"job": "stdin", "hostname": model.LabelValue(hostName)}, Entry: logproto.Entry{Line: "bar"}},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.New(func() {})
			got, err := newReaderTarget(util.Logger, tt.in, c, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("newReaderTarget() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			<-got.ctx.Done()
			c.Stop()
			compareEntries(t, tt.want, c.Received())
		})
	}
}

type mockShutdownable struct {
	called chan bool
}

func (m *mockShutdownable) Shutdown() {
	m.called <- true
}

type fakeStdin struct {
	io.Reader
	os.FileInfo
}

func newFakeStdin(data string) *fakeStdin {
	return &fakeStdin{
		Reader: strings.NewReader(data),
	}
}

func (f fakeStdin) Stat() (os.FileInfo, error) { return f.FileInfo, nil }

func Test_Shutdown(t *testing.T) {
	stdIn = newFakeStdin("line")
	appMock := &mockShutdownable{called: make(chan bool, 1)}
	recorder := fake.New(func() {})
	manager, err := NewStdinTargetManager(util.Logger, appMock, recorder, []scrapeconfig.Config{{}})
	require.NoError(t, err)
	require.NotNil(t, manager)
	require.Equal(t, true, <-appMock.called)
	recorder.Stop()
	compareEntries(t, []api.Entry{{Labels: model.LabelSet{}, Entry: logproto.Entry{Line: "line"}}}, recorder.Received())
}

func compareEntries(t *testing.T, expected, actual []api.Entry) {
	t.Helper()
	require.Equal(t, len(expected), len(actual))
	for i := range expected {
		require.Equal(t, expected[i].Entry.Line, actual[i].Entry.Line)
		require.Equal(t, expected[i].Labels, actual[i].Labels)
	}
}

func Test_StdinConfigs(t *testing.T) {

	// should take the first config
	require.Equal(t, scrapeconfig.DefaultScrapeConfig, getStdinConfig(util.Logger, []scrapeconfig.Config{
		scrapeconfig.DefaultScrapeConfig,
		{},
	}))
	// or use the default if none if provided
	require.Equal(t, defaultStdInCfg, getStdinConfig(util.Logger, []scrapeconfig.Config{}))
}

var stagesConfig = `
pipeline_stages:
- template:
    source: new_key
    template: 'hello world!'
- labels:
    new_key:
`

func loadConfig(yml string) stages.PipelineStages {
	var config map[string]interface{}
	err := yaml.Unmarshal([]byte(yml), &config)
	if err != nil {
		panic(err)
	}
	return config["pipeline_stages"].([]interface{})
}
