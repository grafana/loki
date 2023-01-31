package client

import (
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"

	"github.com/grafana/loki/pkg/logproto"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	nilMetrics = NewMetrics(nil)
	metrics    = NewMetrics(prometheus.DefaultRegisterer)
)

func TestNewMulti(t *testing.T) {
	_, err := NewMulti(nilMetrics, util_log.Logger, 0, 0, false, []Config{}...)
	if err == nil {
		t.Fatal("expected err but got nil")
	}
	host1, _ := url.Parse("http://localhost:3100")
	host2, _ := url.Parse("https://grafana.com")
	cc1 := Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}
	cc2 := Config{
		BatchSize:      10,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host2},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"hi": "there"}},
	}

	clients, err := NewMulti(metrics, util_log.Logger, 0, 0, false, cc1, cc2)
	if err != nil {
		t.Fatalf("expected err: nil got:%v", err)
	}
	multi := clients.(*MultiClient)
	if len(multi.clients) != 2 {
		t.Fatalf("expected client: 2 got:%d", len(multi.clients))
	}
	actualCfg1 := clients.(*MultiClient).clients[0].(*client).cfg
	// Yaml should overridden the command line so 'order: yaml' should be expected
	expectedCfg1 := Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}

	if !reflect.DeepEqual(actualCfg1, expectedCfg1) {
		t.Fatalf("expected cfg: %v got:%v", expectedCfg1, actualCfg1)
	}
}

func TestNewMulti_BlockDuplicates(t *testing.T) {
	_, err := NewMulti(nilMetrics, util_log.Logger, 0, 0, false, []Config{}...)
	if err == nil {
		t.Fatal("expected err but got nil")
	}
	host1, _ := url.Parse("http://localhost:3100")
	cc1 := Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}
	cc1Copy := cc1

	_, err = NewMulti(metrics, util_log.Logger, 0, 0, false, cc1, cc1Copy)
	require.Error(t, err, "expected NewMulti to reject duplicate client configs")

	cc1Copy.Name = "copy"
	clients, err := NewMulti(metrics, util_log.Logger, 0, 0, false, cc1, cc1Copy)
	require.NoError(t, err, "expected NewMulti to reject duplicate client configs")

	multi := clients.(*MultiClient)
	if len(multi.clients) != 2 {
		t.Fatalf("expected client: 2 got:%d", len(multi.clients))
	}
	actualCfg1 := clients.(*MultiClient).clients[0].(*client).cfg
	// Yaml should overridden the command line so 'order: yaml' should be expected
	expectedCfg1 := Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}

	if !reflect.DeepEqual(actualCfg1, expectedCfg1) {
		t.Fatalf("expected cfg: %v got:%v", expectedCfg1, actualCfg1)
	}
}

func TestMultiClient_Stop(t *testing.T) {
	var stopped int

	stopping := func() {
		stopped++
	}
	fc := fake.New(stopping)
	clients := []Client{fc, fc, fc, fc}
	m := &MultiClient{
		clients: clients,
		entries: make(chan api.Entry),
	}
	m.start()
	m.Stop()

	if stopped != len(clients) {
		t.Fatal("missing stop call")
	}
}

func TestMultiClient_Handle(t *testing.T) {
	f := fake.New(func() {})
	clients := []Client{f, f, f, f, f, f}
	m := &MultiClient{
		clients: clients,
		entries: make(chan api.Entry),
	}
	m.start()

	m.Chan() <- api.Entry{Labels: model.LabelSet{"foo": "bar"}, Entry: logproto.Entry{Line: "foo"}}

	m.Stop()

	if len(f.Received()) != len(clients) {
		t.Fatal("missing handle call")
	}
}

func TestMultiClient_Handle_Race(t *testing.T) {
	u := flagext.URLValue{}
	require.NoError(t, u.Set("http://localhost"))
	c1, err := New(nilMetrics, Config{URL: u, BackoffConfig: backoff.Config{MaxRetries: 1}, Timeout: time.Microsecond}, 0, 0, false, log.NewNopLogger())
	require.NoError(t, err)
	c2, err := New(nilMetrics, Config{URL: u, BackoffConfig: backoff.Config{MaxRetries: 1}, Timeout: time.Microsecond}, 0, 0, false, log.NewNopLogger())
	require.NoError(t, err)
	clients := []Client{c1, c2}
	m := &MultiClient{
		clients: clients,
		entries: make(chan api.Entry),
	}
	m.start()

	m.Chan() <- api.Entry{Labels: model.LabelSet{"foo": "bar", ReservedLabelTenantID: "1"}, Entry: logproto.Entry{Line: "foo"}}

	m.Stop()
}
