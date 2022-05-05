package client

import (
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func Test_WAL_Write(t *testing.T) {
	dir := t.TempDir()
	c, err := New(nilMetrics, Config{
		Name:      "foo",
		URL:       flagext.URLValue{URL: &url.URL{}},
		BatchWait: 100 * time.Millisecond,
		BatchSize: int(1e6),
		WAL: WALConfig{
			Enabled: true,
			Dir:     dir,
		},
	}, nil, log.NewNopLogger())
	require.NoError(t, err)

	// defer c.Stop()
	c.Chan() <- api.Entry{
		Labels: model.LabelSet{
			"foo": "bar",
		},
		Entry: logproto.Entry{
			Line: "test",
		},
	}
	c.Chan() <- api.Entry{
		Labels: model.LabelSet{
			ReservedLabelTenantID: "tenant1",
		},
		Entry: logproto.Entry{
			Line: "test",
		},
	}
	names, _ := os.ReadDir(dir)
	for _, name := range names {
		t.Log(name.Name())
	}
}
