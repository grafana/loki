package receiver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/pkg/logproto"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/opentracing/opentracing-go"
	prom_client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/logging"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/configunmarshaler"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	//"go.opentelemetry.io/otel/trace"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

var (
	metricPushDuration = promauto.NewHistogram(prom_client.HistogramOpts{
		Namespace: "loki",
		Name:      "distributor_push_duration_seconds",
		Help:      "Records the amount of time to push a batch to the ingester.",
		Buckets:   prom_client.DefBuckets,
	})
)

type BatchPusher interface {
	Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error)
}

type receiversShim struct {
	services.Service

	receivers []component.Receiver
	pusher    BatchPusher
	logger    log.Logger
}

func (r *receiversShim) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

// New
// handler "/v1/logs" go.opentelemetry.io/collector/receiver/otlpreceiver/otlp.go:229
func New(receiverCfg map[string]interface{}, pusher BatchPusher, middleware Middleware, logLevel logging.Level) (services.Service, error) {
	shim := &receiversShim{
		pusher: pusher,
		logger: util_log.Logger,
	}

	receiverFactories, err := component.MakeReceiverFactoryMap(
		otlpreceiver.NewFactory(),
	)
	if err != nil {
		return nil, err
	}

	p := config.NewMapFromStringMap(map[string]interface{}{
		"receivers": receiverCfg,
	})
	cfgs, err := configunmarshaler.NewDefault().Unmarshal(p, component.Factories{
		Receivers: receiverFactories,
	})
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	params := component.ReceiverCreateSettings{TelemetrySettings: component.TelemetrySettings{
		Logger:         zap.NewNop(),
		TracerProvider: trace.NewNoopTracerProvider(),
		MeterProvider:  metric.NewNoopMeterProvider(),
	}}

	for componentID, cfg := range cfgs.Receivers {
		factoryBase := receiverFactories[componentID.Type()]
		if factoryBase == nil {
			return nil, fmt.Errorf("receiver factory not found for type: %s", componentID.Type())
		}

		receiver, err := factoryBase.CreateLogsReceiver(ctx, params, cfg, middleware.Wrap(shim))
		if err != nil {
			return nil, err
		}

		shim.receivers = append(shim.receivers, receiver)
	}

	shim.Service = services.NewIdleService(shim.starting, shim.stopping)

	return shim, nil
}
func (r *receiversShim) starting(ctx context.Context) error {
	for _, receiver := range r.receivers {
		r.logger.Log("msg", "receiver start")
		err := receiver.Start(ctx, r)
		if err != nil {
			return fmt.Errorf("error starting receiver %w", err)
		}
	}

	return nil
}

// Called after distributor is asked to stop via StopAsync.
func (r *receiversShim) stopping(_ error) error {
	// when shutdown is called on the receiver it immediately shuts down its connection
	// which drops requests on the floor. at this point in the shutdown process
	// the readiness handler is already down . so we are not receiving any more requests.
	// sleep for 30 seconds to here to all pending requests to finish.
	time.Sleep(30 * time.Second)

	ctx, cancelFn := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelFn()

	errs := make([]error, 0)

	for _, receiver := range r.receivers {
		err := receiver.Shutdown(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return multierr.Combine(errs...)
	}

	return nil
}

// ConsumeLogs implements consumer.Logs
func (r *receiversShim) ConsumeLogs(ctx context.Context, ld pdata.Logs) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "distributor.ConsumeLogs")
	defer span.Finish()
	req, err := parseLog(ld)
	if err != nil {
		r.logger.Log("msg", "pusher failed to parse log data", "err", err)
	}

	start := time.Now()
	_, err = r.pusher.Push(ctx, req)
	metricPushDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		r.logger.Log("msg", "pusher failed to consume log data", "err", err)
	}

	return err
}

func parseLog(ld pdata.Logs) (*logproto.PushRequest, error) {
	streams := make(map[string]*logproto.Stream)
	rss := ld.ResourceLogs()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ill := rs.InstrumentationLibraryLogs()
		for i := 0; i < ill.Len(); i++ {
			logs := ill.At(i).Logs()
			for k := 0; k < logs.Len(); k++ {
				pLog := logs.At(k)
				labels := parseLabel(rs.Resource().Attributes(), pLog.Attributes())
				entry, err := parseEntry(pLog)
				if err != nil {
					return nil, err
				}
				if stream, ok := streams[labels]; ok {
					stream.Entries = append(stream.Entries, *entry)
					continue
				}
				// Add the entry as a new stream
				streams[labels] = &logproto.Stream{
					Labels:  labels,
					Entries: []logproto.Entry{*entry},
				}
			}
		}
	}
	req := &logproto.PushRequest{
		Streams: make([]logproto.Stream, 0, len(streams)),
	}

	for _, stream := range streams {
		req.Streams = append(req.Streams, *stream)
	}

	return req, nil
}

func parseEntry(pLog pdata.LogRecord) (*logproto.Entry, error) {
	record := LokiLogRecord{
		SeverityNumber: int32(pLog.SeverityNumber()),
		SeverityText:   pLog.SeverityText(),
		Name:           pLog.Name(),
		Body:           pLog.Body().AsString(),
		Flags:          pLog.Flags(),
		TraceID:        pLog.TraceID().HexString(),
		SpanID:         pLog.SpanID().HexString(),
	}

	data, err := json.Marshal(&record)
	if err != nil {
		return nil, err
	}

	return &logproto.Entry{
		Timestamp: time.Unix(0, int64(pLog.Timestamp())),
		Line:      string(data),
	}, nil
}

func parseLabel(resource pdata.AttributeMap, attr pdata.AttributeMap) string {
	labelMap := make([]string, 0)
	resource.Range(func(key string, attr pdata.AttributeValue) bool {
		labelMap = append(labelMap, fmt.Sprintf("%s=%q", key, attr.AsString()))
		return true
	})
	attr.Range(func(key string, attr pdata.AttributeValue) bool {
		labelMap = append(labelMap, fmt.Sprintf("%s=%q", key, attr.AsString()))
		return true
	})
	sort.Strings(labelMap)
	labels := fmt.Sprintf("{%s}", strings.Join(labelMap, ", "))
	return labels
}

// implements component.Host
func (r *receiversShim) ReportFatalError(err error) {
	level.Error(r.logger).Log("msg", "fatal error reported", "err", err)
}

// implements component.Host
func (r *receiversShim) GetFactory(kind component.Kind, componentType config.Type) component.Factory {
	return nil
}

// implements component.Host
func (r *receiversShim) GetExtensions() map[config.ComponentID]component.Extension {
	return nil
}

// implements component.Host
func (r *receiversShim) GetExporters() map[config.DataType]map[config.ComponentID]component.Exporter {
	return nil
}

type LokiLogRecord struct {
	SeverityNumber int32  `json:"severity_number,omitempty"`
	SeverityText   string `json:"severity_text,omitempty"`
	Name           string `json:"name,omitempty"`
	Body           string `json:"body"`
	Flags          uint32 `json:"flags,omitempty"`
	TraceID        string `json:"trace_id"`
	SpanID         string `json:"span_id"`
}
