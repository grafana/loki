package client

import (
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/promtail/api"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/grafana/loki/pkg/promtail/client/fake"
	"github.com/prometheus/common/model"
)

func TestNewMulti(t *testing.T) {
	_, err := NewMulti(util.Logger, []Config{}...)
	if err == nil {
		t.Fatal("expected err but got nil")
	}
	host1, _ := url.Parse("http://localhost:3100")
	host2, _ := url.Parse("https://grafana.com")
	expectedCfg1 := Config{BatchSize: 20, URL: flagext.URLValue{URL: host1}}
	expectedCfg2 := Config{BatchSize: 10, URL: flagext.URLValue{URL: host2}}

	clients, err := NewMulti(util.Logger, expectedCfg1, expectedCfg2)
	if err != nil {
		t.Fatalf("expected err: nil got:%v", err)
	}
	multi := clients.(MultiClient)
	if len(multi) != 2 {
		t.Fatalf("expected client: 2 got:%d", len(multi))
	}
	cfg1 := clients.(MultiClient)[0].(*client).cfg

	if !reflect.DeepEqual(cfg1, expectedCfg1) {
		t.Fatalf("expected cfg: %v got:%v", expectedCfg1, cfg1)
	}

	cfg2 := clients.(MultiClient)[1].(*client).cfg

	if !reflect.DeepEqual(cfg2, expectedCfg2) {
		t.Fatalf("expected cfg: %v got:%v", expectedCfg2, cfg2)
	}
}

func TestMultiClient_Stop(t *testing.T) {
	var stopped int

	stopping := func() {
		stopped++
	}
	fc := &fake.Client{OnStop: stopping}
	clients := []Client{fc, fc, fc, fc}
	m := MultiClient(clients)

	m.Stop()

	if stopped != len(clients) {
		t.Fatal("missing stop call")
	}
}

func TestMultiClient_Handle(t *testing.T) {

	var called int

	errorFn := api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error { called++; return errors.New("") })
	okFn := api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error { called++; return nil })

	errfc := &fake.Client{OnHandleEntry: errorFn}
	okfc := &fake.Client{OnHandleEntry: okFn}
	t.Run("some error", func(t *testing.T) {
		clients := []Client{okfc, errfc, okfc, errfc, errfc, okfc}
		m := MultiClient(clients)

		if err := m.Handle(nil, time.Now(), ""); err == nil {
			t.Fatal("expected err got nil")
		}

		if called != len(clients) {
			t.Fatal("missing handle call")
		}

	})
	t.Run("no error", func(t *testing.T) {
		called = 0
		clients := []Client{okfc, okfc, okfc, okfc, okfc, okfc}
		m := MultiClient(clients)

		if err := m.Handle(nil, time.Now(), ""); err != nil {
			t.Fatal("expected err to be nil")
		}

		if called != len(clients) {
			t.Fatal("missing handle call")
		}

	})

}
