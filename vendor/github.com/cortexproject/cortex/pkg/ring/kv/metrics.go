package kv

import (
	"context"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/instrument"
)

var requestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "cortex",
	Name:      "kv_request_duration_seconds",
	Help:      "Time spent on consul requests.",
	Buckets:   prometheus.DefBuckets,
}, []string{"operation", "status_code"}))

func init() {
	requestDuration.Register()
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
	c Client
}

func (m metrics) Get(ctx context.Context, key string) (interface{}, error) {
	var result interface{}
	err := instrument.CollectedRequest(ctx, "GET", requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		result, err = m.c.Get(ctx, key)
		return err
	})
	return result, err
}

func (m metrics) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	return instrument.CollectedRequest(ctx, "CAS", requestDuration, errorCode, func(ctx context.Context) error {
		return m.c.CAS(ctx, key, f)
	})
}

func (m metrics) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	instrument.CollectedRequest(ctx, "WatchKey", requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		m.c.WatchKey(ctx, key, f)
		return nil
	})
}

func (m metrics) WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool) {
	instrument.CollectedRequest(ctx, "WatchPrefix", requestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		m.c.WatchPrefix(ctx, prefix, f)
		return nil
	})
}

func (m metrics) Stop() {
	m.c.Stop()
}
