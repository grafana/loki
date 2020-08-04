package stdin

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
)

type line struct {
	labels model.LabelSet
	entry  string
}

type clientRecorder struct {
	recorded []line
}

func (c *clientRecorder) Handle(labels model.LabelSet, time time.Time, entry string) error {
	c.recorded = append(c.recorded, line{labels: labels, entry: entry})
	return nil
}

func Test_newReaderTarget(t *testing.T) {
	tests := []struct {
		name    string
		in      io.Reader
		cfg     scrapeconfig.Config
		want    []line
		wantErr bool
	}{
		{
			"no newlines",
			bytes.NewReader([]byte("bar")),
			scrapeconfig.Config{},
			[]line{
				{model.LabelSet{}, "bar"},
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
			[]line{
				{model.LabelSet{}, "foo"},
				{model.LabelSet{}, "bar"},
			},
			false,
		},
		{
			"pipeline",
			bytes.NewReader([]byte("\nfoo\r\nbar")),
			scrapeconfig.Config{
				PipelineStages: loadConfig(stagesConfig),
			},
			[]line{
				{model.LabelSet{"new_key": "hello world!"}, "foo"},
				{model.LabelSet{"new_key": "hello world!"}, "bar"},
			},
			false,
		},
		{
			"default config",
			bytes.NewReader([]byte("\nfoo\r\nbar")),
			defaultStdInCfg,
			[]line{
				{model.LabelSet{"job": "stdin", "hostname": model.LabelValue(hostName)}, "foo"},
				{model.LabelSet{"job": "stdin", "hostname": model.LabelValue(hostName)}, "bar"},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &clientRecorder{}
			got, err := newReaderTarget(util.Logger, tt.in, recorder, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("newReaderTarget() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			<-got.ctx.Done()
			require.Equal(t, tt.want, recorder.recorded)
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

func newFakeStin(data string) *fakeStdin {
	return &fakeStdin{
		Reader: strings.NewReader(data),
	}
}

func (f fakeStdin) Stat() (os.FileInfo, error) { return f.FileInfo, nil }

func Test_Shutdown(t *testing.T) {
	stdIn = newFakeStin("line")
	appMock := &mockShutdownable{called: make(chan bool, 1)}
	recorder := &clientRecorder{}
	manager, err := NewStdinTargetManager(util.Logger, appMock, recorder, []scrapeconfig.Config{{}})
	require.NoError(t, err)
	require.NotNil(t, manager)
	called := <-appMock.called
	require.Equal(t, true, called)
	require.Equal(t, []line{{labels: model.LabelSet{}, entry: "line"}}, recorder.recorded)
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
