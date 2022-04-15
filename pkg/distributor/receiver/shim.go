package receiver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/go-logfmt/logfmt"
	"github.com/grafana/dskit/services"
	zaplogfmt "github.com/jsternberg/zap-logfmt"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	prom_client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/user"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/configunmarshaler"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"

	"go.uber.org/multierr"
	"go.uber.org/zap"

	"github.com/grafana/loki/pkg/logproto"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	LogFormatJSON   = "json"
	LogFormatLogfmt = "logfmt"
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

	receivers    []component.Receiver
	pusher       BatchPusher
	logger       log.Logger
	format       string
	drainTimeout time.Duration
}

func (r *receiversShim) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

// New
// handler "/v1/logs" go.opentelemetry.io/collector/receiver/otlpreceiver/otlp.go:229
func New(receiverCfg map[string]interface{}, pusher BatchPusher, receiverFormat string, receiverDrainTimeout time.Duration, middleware Middleware, logLevel logging.Level) (services.Service, error) {
	shim := &receiversShim{
		pusher:       pusher,
		format:       receiverFormat,
		logger:       util_log.Logger,
		drainTimeout: receiverDrainTimeout,
	}

	zapLogger := newLogger(logLevel)
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
		Logger:         zapLogger,
		TracerProvider: trace.NewNoopTracerProvider(),
		//MeterProvider:  metric.NewMeterConfig(),
	}}

	for componentID, cfg := range cfgs.Receivers {
		factoryBase := receiverFactories[componentID.Type()]
		if factoryBase == nil {
			return nil, fmt.Errorf("receiver factory not found for type: %s", componentID.Type())
		}

		receiver, err := factoryBase.CreateTracesReceiver(ctx, params, cfg, middleware.Wrap(shim))
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
		level.Debug(r.logger).Log("msg", "receiver start")
		err := receiver.Start(ctx, r)
		if err != nil {
			return fmt.Errorf("error starting receiver %w", err)
		}
	}

	return nil
}

// observability shims
func newLogger(level logging.Level) *zap.Logger {
	zapLevel := zapcore.InfoLevel

	switch level.Logrus {
	case logrus.PanicLevel:
		zapLevel = zapcore.PanicLevel
	case logrus.FatalLevel:
		zapLevel = zapcore.FatalLevel
	case logrus.ErrorLevel:
		zapLevel = zapcore.ErrorLevel
	case logrus.WarnLevel:
		zapLevel = zapcore.WarnLevel
	case logrus.InfoLevel:
		zapLevel = zapcore.InfoLevel
	case logrus.DebugLevel:
	case logrus.TraceLevel:
		zapLevel = zapcore.DebugLevel
	}

	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = func(ts time.Time, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(ts.UTC().Format(time.RFC3339))
	}
	logger := zap.New(zapcore.NewCore(
		zaplogfmt.NewEncoder(config),
		os.Stdout,
		zapLevel,
	))
	logger = logger.With(zap.String("component", "tempo"))
	logger.Info("OTel Shim Logger Initialized")

	return logger
}

func sanitizeLabelKey(key string) string {
	if len(key) == 0 {
		return key
	}
	key = strings.TrimSpace(key)
	if len(key) == 0 {
		return key
	}
	if key[0] >= '0' && key[0] <= '9' {
		key = "_" + key
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, key)
}

// Called after distributor is asked to stop via StopAsync.
func (r *receiversShim) stopping(_ error) error {
	// when shutdown is called on the receiver it immediately shuts down its connection
	// which drops requests on the floor. at this point in the shutdown process
	// the readiness handler is already down . so we are not receiving any more requests.
	// sleep for 30 seconds to here to all pending requests to finish.
	time.Sleep(r.drainTimeout)

	ctx, cancelFn := context.WithTimeout(context.Background(), 3*time.Second)
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
func (r *receiversShim) ConsumeTraces(ctx context.Context, td pdata.Traces) error {
	span, ctx := opentracing.StartSpanFromContext(ctx, "distributor.ConsumeTraces")
	defer span.Finish()
	req, err := parseTrace(td, r.format)
	if err != nil {
		level.Error(r.logger).Log("msg", "pusher failed to parse trace data", "err", err)
	}

	start := time.Now()

	tenantId := ""
	tenantIdVal := ctx.Value(user.OrgIDHeaderName)
	if tenantIdVal == nil {
		tenantIdVal = ctx.Value("X-Scope-Orgid")
		if tenantIdVal != nil {
			tenantId = (tenantIdVal.([]string))[0]
			ctx = user.InjectOrgID(ctx, tenantId)
			ctx = user.InjectUserID(ctx, tenantId)
		}
	}

	_, err = r.pusher.Push(ctx, req)
	metricPushDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		r.logger.Log("msg", "pusher failed to consume trace data", "err", err)
		return errors.New("pusher failed to consume trace data,err:" + err.Error() + ",tenantId:" + tenantId)
	}

	return nil
}

func parseTrace(ld pdata.Traces, format string) (*logproto.PushRequest, error) {
	streams := make(map[string]*logproto.Stream)
	rss := ld.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		ill := rs.InstrumentationLibrarySpans()
		for i := 0; i < ill.Len(); i++ {
			logs := ill.At(i).Spans()
			for k := 0; k < logs.Len(); k++ {
				pLog := logs.At(k)
				labels := parseLabel(rs.Resource().Attributes(), pLog.Attributes())
				entry, err := parseEntry(pLog, format)
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

func parseEntry(pLog pdata.Span, format string) (*logproto.Entry, error) {
	var line string
	if format == LogFormatLogfmt {
		records := make(map[string]interface{})

		records["trace_id"] = pLog.TraceID().HexString()
		records["span_id"] = pLog.SpanID().HexString()
		records["name"] = pLog.Name()
		records["parent_span_id"] = pLog.ParentSpanID().HexString()
		records["status_code"] = pLog.Status().Code().String()
		records["status_msg"] = pLog.Status().Message()
		records["trace_state"] = pLog.TraceState()
		records["kind"] = pLog.Kind()
		records["start_time"] = pLog.StartTimestamp().String()
		records["end_time"] = pLog.EndTimestamp().String()
		records["duration"] = pLog.EndTimestamp() - pLog.StartTimestamp()

		buf := &bytes.Buffer{}
		enc := logfmt.NewEncoder(buf)
		keys := make([]string, 0, len(records))
		for k := range records {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			err := enc.EncodeKeyval(k, records[k])
			if err == logfmt.ErrUnsupportedValueType {
				err := enc.EncodeKeyval(k, fmt.Sprintf("%+v", records[k]))
				if err != nil {
					return nil, err
				}
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		line = buf.String()
	} else if format == LogFormatJSON {

		record := LokiTraceRecord{
			TraceID:      pLog.TraceID().HexString(),
			SpanID:       pLog.SpanID().HexString(),
			Name:         pLog.Name(),
			ParentSpanId: pLog.ParentSpanID().HexString(),
			StatusCode:   pLog.Status().Code().String(),
			StatusMsg:    pLog.Status().Message(),
			TraceState:   pLog.TraceState(),
			Kind:         pLog.Kind().String(),
			StartTime:    pLog.StartTimestamp().String(),
			EndTime:      pLog.EndTimestamp().String(),
			Duration:     pLog.EndTimestamp() - pLog.StartTimestamp(),
		}

		maxEvents := 3
		for i := 0; i < pLog.Events().Len(); i++ {
			if i >= maxEvents {
				break
			}
			event := pLog.Events().At(i)
			spanEvent := Span_Event{Name: event.Name(), Timestamp: event.Timestamp()}
			if i == 0 {
				record.Event1 = spanEvent
			} else if i == 1 {
				record.Event2 = spanEvent
			} else if i == 2 {
				record.Event3 = spanEvent
			}

		}

		data, err := json.Marshal(&record)
		if err != nil {
			return nil, err
		}
		line = string(data)
	} else {
		return nil, fmt.Errorf("unsupported format type: %s", format)
	}

	return &logproto.Entry{
		Timestamp: time.Unix(0, int64(pLog.StartTimestamp())),
		Line:      line,
	}, nil
}

func parseLabel(resource pdata.AttributeMap, attr pdata.AttributeMap) string {
	labelMap := make([]string, 0)
	resource.Range(func(key string, attr pdata.AttributeValue) bool {
		key = sanitizeLabelKey(key)
		labelMap = append(labelMap, fmt.Sprintf("%s=%q", key, attr.AsString()))
		return true
	})
	attr.Range(func(key string, attr pdata.AttributeValue) bool {
		key = sanitizeLabelKey(key)
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

type LokiTraceRecord struct {
	TraceID      string           `json:"trace_id"`
	SpanID       string           `json:"span_id"`
	Name         string           `json:"name"`
	ParentSpanId string           `json:"parent_span_id"`
	StatusCode   string           `json:"status_code"`
	StatusMsg    string           `json:"status_msg"`
	TraceState   pdata.TraceState `json:"trace_state"`
	Kind         string           `json:"kind"`
	StartTime    string           `json:"start_time"`
	EndTime      string           `json:"end_time"`
	Duration     pdata.Timestamp  `json:"duration"`
	Event1       Span_Event       `json:"event1"`
	Event2       Span_Event       `json:"event2"`
	Event3       Span_Event       `json:"event3"`
}

type Span_Event struct {
	// time_unix_nano is the time the event occurred.
	Timestamp pdata.Timestamp `protobuf:"fixed64,1,opt,name=time_unix_nano,json=timeUnixNano,proto3" json:"time_unix_nano,omitempty"`
	// name of the event.
	// This field is semantically required to be set to non-empty string.
	Name string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	// attributes is a collection of attribute key/value pairs on the event.
	Attributes pdata.AttributeMap `protobuf:"bytes,3,rep,name=attributes,proto3" json:"attributes"`
}
