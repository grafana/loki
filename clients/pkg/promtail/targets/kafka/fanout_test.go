package kafka

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
)

func Test_FanOut(t *testing.T) {
	var stops int
	var fakes []*fake.Client
	// replace the client factory for the purpose of test.
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

	for i := 0; i < 20; i++ {
		h.Chan() <- api.Entry{
			Labels: model.LabelSet{
				"e": model.LabelValue(fmt.Sprintf("%d", i)),
			},
			Entry: logproto.Entry{
				Timestamp: time.Unix(0, int64(i)),
				Line:      fmt.Sprintf("%d", i),
			},
		}
	}
	h.Stop()
	require.Equal(t, 10, stops)

	for i, f := range fakes {
		require.Len(t, f.Received(), 2) // each client should have received 2 entries.
		require.Equal(t, api.Entry{
			Labels: model.LabelSet{
				"e":         model.LabelValue(fmt.Sprintf("%d", i)),
				"added":     "another",
				WorkerLabel: model.LabelValue(fmt.Sprintf("%d", i%10)),
			},
			Entry: logproto.Entry{
				Timestamp: time.Unix(0, int64(i)),
				Line:      fmt.Sprintf("%d", i),
			},
		}, f.Received()[0])
		require.Equal(t, api.Entry{
			Labels: model.LabelSet{
				"e":         model.LabelValue(fmt.Sprintf("%d", i+10)),
				"added":     "another",
				WorkerLabel: model.LabelValue(fmt.Sprintf("%d", (i+10)%10)),
			},
			Entry: logproto.Entry{
				Timestamp: time.Unix(0, int64(i+10)),
				Line:      fmt.Sprintf("%d", i+10),
			},
		}, f.Received()[1])
	}
}

func Test_FanOutSingle(t *testing.T) {
	var stops int
	var fakes []*fake.Client
	// replace the client factory for the purpose of test.
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
	h, err := NewFanOutHandler(1, log.NewNopLogger(), prometheus.DefaultRegisterer, func() (api.EntryMiddleware, error) {
		return api.AddLabelsMiddleware(model.LabelSet{"added": "another"}), nil
	}, cfg)
	require.NoError(t, err)
	require.Len(t, fakes, 1)

	for i := 0; i < 20; i++ {
		h.Chan() <- api.Entry{
			Labels: model.LabelSet{
				"e": model.LabelValue(fmt.Sprintf("%d", i)),
			},
			Entry: logproto.Entry{
				Timestamp: time.Unix(0, int64(i)),
				Line:      fmt.Sprintf("%d", i),
			},
		}
	}
	h.Stop()
	require.Equal(t, 1, stops)

	f := fakes[0]
	require.Len(t, f.Received(), 20)
	for i := 0; i < 20; i++ {
		require.Equal(t, api.Entry{
			Labels: model.LabelSet{
				"e":     model.LabelValue(fmt.Sprintf("%d", i)),
				"added": "another",
			},
			Entry: logproto.Entry{
				Timestamp: time.Unix(0, int64(i)),
				Line:      fmt.Sprintf("%d", i),
			},
		}, f.Received()[i])
	}
}
