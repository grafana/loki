package kv

import (
	"context"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/instrument"
)

// RegistererWithKVName wraps the provided Registerer with the KV name label. If a nil reg
// is provided, a nil registry is returned
func RegistererWithKVName(reg prometheus.Registerer, name string) prometheus.Registerer {
	if reg == nil {
		return nil
	}

	return prometheus.WrapRegistererWith(prometheus.Labels{"kv_name": name}, reg)
}

// errorCode converts an error into an HTTP status code, modified from weaveworks/common/instrument
func errorCode(err error) string {
	if err == nil {
		return "200"
	}
	if resp, ok := httpgrpc.HTTPResponseFromError(err); ok {
		return strconv.Itoa(int(resp.GetCode()))
	}
	return "500"
}

type metrics struct {
	c               Client
	requestDuration *instrument.HistogramCollector
}

func newMetricsClient(backend string, c Client, reg prometheus.Registerer) Client {
	return &metrics{
		c: c,
		requestDuration: instrument.NewHistogramCollector(
			promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "cortex",
				Name:      "kv_request_duration_seconds",
				Help:      "Time spent on kv store requests.",
				Buckets:   prometheus.DefBuckets,
				ConstLabels: prometheus.Labels{
					"type": backend,
				},
			}, []string{"operation", "status_code"}),
		),
	}
}

func (m metrics) List(ctx context.Context, prefix string) ([]string, error) {
	var result []string
	err := instrument.CollectedRequest(ctx, "List", m.requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		result, err = m.c.List(ctx, prefix)
		return err
	})
	return result, err
}

func (m metrics) Get(ctx context.Context, key string) (interface{}, error) {
	var result interface{}
	err := instrument.CollectedRequest(ctx, "GET", m.requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		result, err = m.c.Get(ctx, key)
		return err
	})
	return result, err
}

func (m metrics) Delete(ctx context.Context, key string) error {
	err := instrument.CollectedRequest(ctx, "Delete", m.requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		return m.c.Delete(ctx, key)
	})
	return err
}

func (m metrics) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	return instrument.CollectedRequest(ctx, "CAS", m.requestDuration, errorCode, func(ctx context.Context) error {
		return m.c.CAS(ctx, key, f)
	})
}

func (m metrics) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	_ = instrument.CollectedRequest(ctx, "WatchKey", m.requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		m.c.WatchKey(ctx, key, f)
		return nil
	})
}

func (m metrics) WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool) {
	_ = instrument.CollectedRequest(ctx, "WatchPrefix", m.requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		m.c.WatchPrefix(ctx, prefix, f)
		return nil
	})
}
