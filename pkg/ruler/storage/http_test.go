package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/test"
	"github.com/go-kit/kit/log"
	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

func TestAgent_ListInstancesHandler(t *testing.T) {
	fact := newFakeInstanceFactory()
	a, err := newAgent(prometheus.NewRegistry(), Config{
		WALDir: "/tmp/agent",
	}, log.NewNopLogger(), fact.factory)
	require.NoError(t, err)
	defer a.Stop()

	r := httptest.NewRequest("GET", "/agent/api/v1/instances", nil)

	t.Run("no instances", func(t *testing.T) {
		rr := httptest.NewRecorder()
		a.ListInstancesHandler(rr, r)
		expect := `{"status":"success","data":[]}`
		require.Equal(t, expect, rr.Body.String())
	})

	t.Run("non-empty", func(t *testing.T) {
		require.NoError(t, a.mm.ApplyConfig(makeInstanceConfig("foo")))
		require.NoError(t, a.mm.ApplyConfig(makeInstanceConfig("bar")))

		expect := `{"status":"success","data":["bar","foo"]}`
		test.Poll(t, time.Second, true, func() interface{} {
			rr := httptest.NewRecorder()
			a.ListInstancesHandler(rr, r)
			return expect == rr.Body.String()
		})
	})
}

func TestAgent_ListTargetsHandler(t *testing.T) {
	fact := newFakeInstanceFactory()
	a, err := newAgent(prometheus.NewRegistry(), Config{
		WALDir: "/tmp/agent",
	}, log.NewNopLogger(), fact.factory)
	require.NoError(t, err)

	mockManager := &instance.MockManager{
		ListInstancesFunc: func() map[string]instance.ManagedInstance { return nil },
		ListConfigsFunc:   func() map[string]instance.Config { return nil },
		ApplyConfigFunc:   func(_ instance.Config) error { return nil },
		DeleteConfigFunc:  func(name string) error { return nil },
		StopFunc:          func() {},
	}
	a.mm, err = instance.NewModalManager(prometheus.NewRegistry(), a.logger, mockManager, instance.ModeDistinct)
	require.NoError(t, err)

	r := httptest.NewRequest("GET", "/agent/api/v1/targets", nil)

	t.Run("scrape manager not ready", func(t *testing.T) {
		mockManager.ListInstancesFunc = func() map[string]instance.ManagedInstance {
			return map[string]instance.ManagedInstance{
				"test_instance": &mockInstanceScrape{},
			}
		}

		rr := httptest.NewRecorder()
		a.ListTargetsHandler(rr, r)
		expect := `{"status": "success", "data": []}`
		require.JSONEq(t, expect, rr.Body.String())
		require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	})

	t.Run("scrape manager targets", func(t *testing.T) {
		tgt := scrape.NewTarget(labels.FromMap(map[string]string{
			model.JobLabel:         "job",
			model.InstanceLabel:    "instance",
			"foo":                  "bar",
			model.SchemeLabel:      "http",
			model.AddressLabel:     "localhost:12345",
			model.MetricsPathLabel: "/metrics",
		}), labels.FromMap(map[string]string{
			"__discovered__": "yes",
		}), nil)

		startTime := time.Date(1994, time.January, 12, 0, 0, 0, 0, time.UTC)
		tgt.Report(startTime, time.Minute, fmt.Errorf("something went wrong"))

		mockManager.ListInstancesFunc = func() map[string]instance.ManagedInstance {
			return map[string]instance.ManagedInstance{
				"test_instance": &mockInstanceScrape{
					tgts: map[string][]*scrape.Target{
						"group_a": {tgt},
					},
				},
			}
		}

		rr := httptest.NewRecorder()
		a.ListTargetsHandler(rr, r)
		expect := `{
			"status": "success",
			"data": [{
				"instance": "test_instance",
				"target_group": "group_a",
				"endpoint": "http://localhost:12345/metrics",
				"state": "down",
				"labels": {
					"foo": "bar",
					"instance": "instance",
					"job": "job"
				},
				"discovered_labels": {
					"__discovered__": "yes"
				},
				"last_scrape": "1994-01-12T00:00:00Z",
				"scrape_duration_ms": 60000,
				"scrape_error":"something went wrong"
			}]
		}`
		require.JSONEq(t, expect, rr.Body.String())
		require.Equal(t, http.StatusOK, rr.Result().StatusCode)
	})
}

type mockInstanceScrape struct {
	tgts map[string][]*scrape.Target
}

func (i *mockInstanceScrape) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (i *mockInstanceScrape) Update(_ instance.Config) error {
	return nil
}

func (i *mockInstanceScrape) TargetsActive() map[string][]*scrape.Target {
	return i.tgts
}

func (i *mockInstanceScrape) StorageDirectory() string {
	return ""
}

func (i *mockInstanceScrape) Appender(ctx context.Context) storage.Appender {
	return nil
}
