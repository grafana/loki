package targets

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/prometheus/common/model"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/stretchr/testify/require"
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
		cfg     scrape.Config
		want    []line
		wantErr bool
	}{
		{
			"bad config",
			bytes.NewReader([]byte("")),
			scrape.Config{ServiceDiscoveryConfig: sd_config.ServiceDiscoveryConfig{
				StaticConfigs: []*targetgroup.Group{},
			}},
			nil,
			true,
		},
		{
			"no newlines",
			bytes.NewReader([]byte("bar")),
			scrape.Config{},
			[]line{
				{nil, "bar"},
			},
			false,
		},
		{
			"empty",
			bytes.NewReader([]byte("")),
			scrape.Config{},
			nil,
			false,
		},
		{
			"newlines",
			bytes.NewReader([]byte("\nfoo\r\nbar")),
			scrape.Config{},
			[]line{
				{nil, "foo"},
				{nil, "bar"},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &clientRecorder{}
			got, err := newReaderTarget(tt.in, recorder, tt.cfg)
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
