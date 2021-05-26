package kafka

import (
	"net/url"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
)

func Test_FanOut(t *testing.T) {
	var stops int
	var fakes []*fake.Client
	DefaultClientFactory = func(reg prometheus.Registerer, logger log.Logger, cfgs ...client.Config) (client.Client, error) {
		f := fake.New(func() {
			stops++
		})
		fakes = append(fakes, f)
		return f, nil
	}
	host1, _ := url.Parse("http://localhost:3100")
	cfg := client.Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"foo": "bar"}},
	}
	h, err := NewFanOutHandler(10, log.NewNopLogger(), prometheus.DefaultRegisterer, func() (api.EntryMiddleware, error) {
		return api.AddLabelsMiddleware(model.LabelSet{"added": "another"}), nil
	}, cfg)
	require.NoError(t, err)
	require.Len(t, fakes, 10)
	h.Stop()

	require.Equal(t, 10, stops)
}
