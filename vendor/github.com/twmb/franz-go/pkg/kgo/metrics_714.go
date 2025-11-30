package kgo

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func (cl *Client) pushMetrics() {
	defer cl.metrics.ctxCancel()

	if cl.cfg.disableClientMetrics {
		return
	}

	m := &cl.metrics

	select {
	case <-cl.ctx.Done():
		return
	case <-m.firstObserve:
	}

	if !cl.supportsClientMetrics() {
		m.mu.Lock()
		m.unsupported.Store(true)
		m.pReqLatency = nil
		m.cReqLatency = nil
		m.mu.Unlock()
		return
	}

	var clientInstanceID [16]byte
	var terminating bool
	for !terminating {
		greq := kmsg.NewGetTelemetrySubscriptionsRequest()
		greq.ClientInstanceID = clientInstanceID

		gresp, err := greq.RequestWith(m.ctx, cl)
		if err == nil {
			err = kerr.ErrorForCode(gresp.ErrorCode)
		}
		if err != nil {
			cl.cfg.logger.Log(LogLevelInfo, "unable to get telemetry subscriptions, retrying in 30s", "err", err)
			after := time.NewTimer(30 * time.Second)
			select {
			case <-cl.ctx.Done():
				after.Stop()
				return
			case <-after.C:
			}
			continue
		}

		clientInstanceID = gresp.ClientInstanceID

		// If there are no requested metrics, we wait the push interval
		// and re-get.
		if len(gresp.RequestedMetrics) == 0 {
			wait := time.Duration(gresp.PushIntervalMillis) * time.Millisecond
			cl.cfg.logger.Log(LogLevelInfo, "no metrics requested, sleeping and asking again later", "sleep", wait)
			after := time.NewTimer(wait)
			select {
			case <-cl.ctx.Done():
				terminating = true
			case <-after.C:
			}
			continue
		}
		cl.cfg.logger.Log(LogLevelInfo, "received client metrics subscription, beginning periodic send loop",
			"client_instance_id", fmt.Sprintf("%x", gresp.ClientInstanceID),
			"subscription_id", gresp.SubscriptionID,
			"accepteded_compression_types", gresp.AcceptedCompressionTypes,
			"telemetry_max_bytes", gresp.TelemetryMaxBytes,
			"push_interval", time.Duration(gresp.PushIntervalMillis)*time.Millisecond,
			"requested_metrics", gresp.RequestedMetrics,
		)

		var codecs []CompressionCodec
		for _, accepted := range gresp.AcceptedCompressionTypes {
			switch CompressionCodecType(accepted) {
			case CodecNone:
			case CodecGzip:
				codecs = append(codecs, GzipCompression())
			case CodecSnappy:
				codecs = append(codecs, SnappyCompression())
			case CodecLz4:
				codecs = append(codecs, Lz4Compression())
			case CodecZstd:
				codecs = append(codecs, ZstdCompression())
			}
		}
		compressor, _ := DefaultCompressor(codecs...)
		allowedNames := buildNameFilter(gresp.RequestedMetrics)

		// We want to pin pushing metrics to a single broker until that
		// broker goes away. To do this, we first issue RequestSharded,
		// figure out the broker that responded, and then use that
		// broker until we receive errUnknownBroker.
		var br *Broker

	push:
		for i := 0; !terminating; i++ {
			wait := max(time.Duration(gresp.PushIntervalMillis)*time.Millisecond, time.Second)
			if i == 0 { // for the first request, jitter 0.5 <= wait <= 1.5
				cl.rng(func(r *rand.Rand) {
					wait = time.Duration(float64(wait) * (0.5 + r.Float64()))
				})
			}

			// Wait until our push interval; if the client is quitting,
			// we immediately send a push with Terminating=true.
			after := time.NewTimer(wait)
			var terminating bool
			select {
			case <-cl.ctx.Done():
				terminating = true
			case <-after.C:
			}

			// Create our request.
			preq := kmsg.NewPushTelemetryRequest()
			preq.ClientInstanceID = clientInstanceID
			preq.SubscriptionID = gresp.SubscriptionID
			preq.Terminating = terminating
			serialized, compression, nmetrics := m.appendTo(nil, gresp.DeltaTemporality, gresp.TelemetryMaxBytes, allowedNames, compressor)
			if len(serialized) > int(gresp.TelemetryMaxBytes) {
				cl.cfg.logger.Log(LogLevelWarn, "serialized metrics are larger than the broker provided max limit, sending anyway and hoping for the best",
					"max_bytes", gresp.TelemetryMaxBytes,
					"serialized_bytes", len(serialized),
				)
			}
			preq.CompressionType = compression
			preq.Metrics = serialized

			cl.cfg.logger.Log(LogLevelDebug, "sending client metrics to broker", "num_metrics", nmetrics)

			// Send our request, pinning to the broker we get a response
			// from or using the pinned broker if possible.
			var resp kmsg.Response
			var err error
		doreq:
			if br == nil {
				shard := cl.RequestSharded(m.ctx, &preq)[0]
				resp, err = shard.Resp, shard.Err
				if err == nil {
					br = cl.Broker(int(shard.Meta.NodeID))
				}
			} else {
				resp, err = br.RetriableRequest(m.ctx, &preq)
				if errors.Is(err, errUnknownBroker) {
					br = nil
					goto doreq
				}
			}
			if err != nil {
				cl.cfg.logger.Log(LogLevelWarn, "unable to send client metrics, resetting subscription", "err", err)
				break
			}

			// Not much to do on the response besides a bunch of
			// error handling.
			presp := resp.(*kmsg.PushTelemetryResponse)
			switch err := kerr.ErrorForCode(presp.ErrorCode); err {
			case nil:
			case kerr.InvalidRequest:
				cl.cfg.logger.Log(LogLevelError, "client metrics was sent at the wrong time (after sending terminating request already?), exiting metrics loop", "err", err)
				return
			case kerr.InvalidRecord:
				cl.cfg.logger.Log(LogLevelError, "broker could not understand our client metrics serialization, this is perhaps a franz-go bug", "err", err)
				return
			case kerr.TelemetryTooLarge:
				cl.cfg.logger.Log(LogLevelWarn, "client metrics payload was too large (check your broker max telemetry bytes)", "err", err)
				// Do nothing; continue aggregating
			case kerr.UnknownSubscriptionID:
				cl.cfg.logger.Log(LogLevelInfo, "client metrics has an outdated subscription, re-getting our subscription information", "err", err)
				break push
			case kerr.UnsupportedCompressionType:
				cl.cfg.logger.Log(LogLevelInfo, "client metrics compression is not supported by the broker even though we only used previously supported compressors, re-getting our subscription information", "err", err)
			default:
				if !kerr.IsRetriable(err) {
					cl.cfg.logger.Log(LogLevelError, "client metrics received an unknown error we do not know how to handle, exiting metrics loop", "err", err)
					return
				}
				cl.cfg.logger.Log(LogLevelWarn, "client metrics received an unknown error that is retryably, continuing to next push cycle", "err", err)
			}
		}
	}
}

func buildNameFilter(requested []string) func(string) bool {
	if len(requested) == 0 {
		return func(string) bool { return false }
	}
	if len(requested) == 1 && requested[0] == "" {
		return func(string) bool { return true }
	}
	return func(name string) bool {
		for _, pfx := range requested {
			if strings.HasPrefix(name, pfx) {
				return true
			}
		}
		return false
	}
}

const (
	// MetricTypeSum is a sum type metric. Every time the metric is
	// collected, the number you are returning is cumulative from the time
	// the client / your application was initialized.
	MetricTypeSum = 1 + iota

	// MetricTypeGauge is a gauge type metric. It is a recording of a value
	// at a point in time.
	MetricTypeGauge
)

type (
	// MetricType is the type of metric you are providing: Type is the type
	// of metric this is: either a gauge or a sum type.
	MetricType uint8

	// Metric is a user-defined client side metric so that you can send
	// user-defined client metrics to the broker and give your cluster
	// operator insight into your client.
	//
	// This type exists to support KIP-1076, which is an extension to
	// KIP-714. Read either of those for more detail.
	Metric struct {
		// Name is a user provided metric name.
		//
		// KIP-714 prescribes how to name metrics: lowercase with dots,
		// no dashes, and interoperable with the OpenTelemetry
		// ecosystem. It is recommended follow the KIP-714 guidance and
		// to namespace your metrics ("my.specific.metric.1;
		// my.specific.metric.2). This client does not attempt to
		// further santize your user provided name.
		Name string

		// Type is the type of metric this is: either a gauge or a sum
		// type.
		//
		// Note that for sum types, the client internally caches all
		// sum types by name for one extra collection period so that
		// the client can calculate "delta" metrics if the broker
		// requests them. You must provide the sum type metric on every
		// collection; skipping a cycle means the client will be unable
		// to calculate the delta once you provide it again in the
		// future.
		Type MetricType

		// ValueInt is the value to record. Only one of ValueInt or
		// ValueFloat should be non-zero; if both are non-zero or if
		// both are zero, the metric is ignored.
		//
		// Sum metrics only support ValueInt, and the number should
		// never go down. If the value goes down or ValueFloat is
		// used, the metric is skipped.
		ValueInt int64

		// ValueFloat is the value to record. Only one of ValueInt or
		// ValueFloat should be non-zero; if both are non-zero or if
		// both are zero, the metric is ignored.
		//
		// Sum metrics only support ValueInt, and the number should
		// never go down. If the value goes down or ValueFloat is
		// used, the metric is skipped.
		ValueFloat float64

		// Attrs are optional attributes to add to this metric, such as
		// a node ID. The supported `any` types are strings, booleans,
		// numbers, and byte slices. All other attributes are silently
		// skipped. Attributes should be lowercase with underscores
		// (see KIP-714).
		Attrs map[string]any
	}

	// metricRate is a count per second and a total.
	metricRate struct {
		count   atomic.Int64 // Sum of events this period; rate == float64(count/time) at rollup
		tot     atomic.Int64 // Total events over all time.
		lastTot int64        // Updated when encoding; the last value for tot in case broker requests DELTA.
	}

	// metricTime reports average latency, max latency, and total latency.
	// The unit is in milliseconds.
	metricTime struct {
		sum atomic.Int64 // With separate aggDur field, avg = sum/aggDur at rollup.
		max atomic.Int64 // Max latency seen during this window.
	}

	// We skip:
	// * producer.record.queue.time.{avg,max}     : medium signal; requires more wiring in the producer
	// * consumer.poll.idle.ratio.avg             : less relevant in this client
	// * consumer.coordinator.rebalance.latency   : underspecified: should this just track leader rebalance time? or from when we notice rebalance in progress to the next ok heartbeat? or..?
	// * consumer.fetch.manager.fetch.latency     : seems to duplicate consumer.node.request.latency ??
	// * consumer.coordinator.assigned.partitions : honestly just tedious to work in given incremental changes from cooperative rebalancing; open to being convinced to add this
	metrics struct {
		cl *Client

		closedFirstObserve atomic.Bool

		// mu is grabbed when accessing the map fields.
		mu          sync.Mutex
		unsupported atomic.Bool // set to true if the broker does not support client metrics; guards nil-ing the maps

		pConnCreation metricRate
		pReqLatency   map[int32]*metricTime
		pThrottle     metricTime

		cConnCreation  metricRate
		cReqLatency    map[int32]*metricTime
		cCommitLatency metricTime

		initNano     int64
		lastPushNano int64

		userSumLast map[string]int64

		firstObserve chan struct{}

		ctx       context.Context
		ctxCancel func()
	}
)

func (m *metrics) init(cl *Client) {
	m.cl = cl
	m.initNano = time.Now().UnixNano()
	m.firstObserve = make(chan struct{})
	m.ctx, m.ctxCancel = context.WithCancel(context.Background()) // for graceful shutdown
}

func safeDiv[T ~int64 | ~float64](num, denom T) T {
	if denom == 0 {
		return 0
	}
	return num / denom
}

// Internally we use the metrics.observeRate function so that
// triggerFirstObserve is always called.
func (t *metricRate) observe() {
	t.count.Add(1)
	t.tot.Add(1)
}

func (t *metricRate) rollNums() (rate float64, tot, lastTot int64) {
	count := t.count.Swap(0)
	lastTot = t.lastTot
	tot = t.tot.Load()
	t.lastTot = tot

	rate = safeDiv(float64(count), float64(tot-lastTot))
	return rate, tot, lastTot
}

// Internally we use the metrics.observeTime function so that
// triggerFirstObserve is always called.
func (t *metricTime) observe(millis int64) {
	t.sum.Add(millis)
	for {
		max := t.max.Load()
		if millis < max {
			return
		}
		if t.max.CompareAndSwap(max, millis) {
			return
		}
	}
}

func (t *metricTime) rollNums(aggDur time.Duration) (avg float64, max int64) {
	sum := t.sum.Swap(0)
	max = t.max.Swap(0)
	avg = safeDiv(float64(sum), float64(aggDur.Milliseconds()))
	return avg, max
}

func (m *metrics) triggerFirstObserve() {
	if !m.closedFirstObserve.Swap(true) {
		close(m.firstObserve)
	}
}

func (m *metrics) observeRate(field *metricRate) {
	m.triggerFirstObserve()
	field.observe()
}

func (m *metrics) observeTime(field *metricTime, millis int64) {
	m.triggerFirstObserve()
	field.observe(millis)
}

func (m *metrics) observeNodeTime(node int32, field *map[int32]*metricTime, millis int64) {
	m.triggerFirstObserve()
	if m.unsupported.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unsupported.Load() { // one more check inside the lock to avoid a race where the maps are set nil after storing true
		return
	}
	if *field == nil {
		*field = make(map[int32]*metricTime)
	}
	t := (*field)[node]
	if t == nil {
		t = new(metricTime)
		(*field)[node] = t
	}
	t.observe(millis)
}

func (m *metrics) appendTo(b []byte, useDeltaSums bool, maxBytes int32, allowedNames func(string) bool, compressor Compressor) ([]byte, int8, int) {
	////////////
	// VARIABLE INITIALIZATION
	////////////
	var (
		metricsData    otelMetricsData
		resourceMetric = &metricsData.resourceMetric
		resource       = &resourceMetric.resource
		scopeMetric    = &resourceMetric.scopeMetric
		scope          = &scopeMetric.scope
		metrics        = &scopeMetric.metrics
		labels         = make(map[string]any)
	)

	if m.cl.cfg.rack != "" {
		labels["client_rack"] = m.cl.cfg.rack
	}
	if m.cl.cfg.group != "" {
		labels["group_id"] = m.cl.cfg.group
	}
	if m.cl.cfg.instanceID != nil {
		labels["group_instance_id"] = *m.cl.cfg.instanceID
	}
	if memberID, _ := m.cl.GroupMetadata(); memberID != "" {
		labels["group_member_id"] = memberID
	}
	if m.cl.cfg.txnID != nil {
		labels["transactional_id"] = *m.cl.cfg.txnID
	}
	if len(labels) > 0 {
		resource.attributes = labels
	}
	scope.name = "kgo"
	scope.version = softwareVersion()

	now := time.Now()
	nowNano := now.UnixNano()
	lastPush := m.lastPushNano
	aggDur := now.Sub(time.Unix(0, lastPush))
	if lastPush == 0 {
		aggDur = now.Sub(time.Unix(0, m.initNano))
	}
	m.lastPushNano = nowNano

	////////////
	// FUNCTIONS THAT APPEND TO `metrics`.
	////////////

	appendGauge := func(name string, vi64 int64, vf64 float64, attrs map[string]any) {
		if vi64 == 0 && vf64 == 0 || vi64 != 0 && vf64 != 0 {
			return
		}
		if !allowedNames(name) {
			return
		}
		*metrics = append(*metrics, otelMetric{
			name: name,
			gauge: otelGauge{
				otelNumDataPoint{
					attributes: attrs,
					vInt:       vi64,
					vDouble:    vf64,
					startNano:  lastPush,
					timeNano:   nowNano,
				},
			},
		})
	}
	appendSum := func(name string, tot, lastTot int64, attrs map[string]any) {
		if tot-lastTot == 0 {
			return
		}
		if !allowedNames(name) {
			return
		}
		if useDeltaSums {
			*metrics = append(*metrics, otelMetric{
				name: name,
				sum: otelSum{
					dataPoint: otelNumDataPoint{
						attributes: attrs,
						vInt:       tot - lastTot,
						startNano:  lastPush,
						timeNano:   nowNano,
					},
					aggregationTemporality: otelTempDelta,
				},
			})
		} else {
			*metrics = append(*metrics, otelMetric{
				name: name,
				sum: otelSum{
					dataPoint: otelNumDataPoint{
						attributes: attrs,
						vInt:       tot,
						startNano:  m.initNano,
						timeNano:   nowNano,
					},
					aggregationTemporality: otelTempCumulative,
				},
			})
		}
	}

	////////////
	// COLLECTING ALL METRICS TO SEND
	////////////

	m.mu.Lock()
	for _, s := range []struct {
		name string
		v    any
	}{
		{"org.apache.kafka.producer.connection.creation", &m.pConnCreation},
		{"org.apache.kafka.producer.node.request.latency", &m.pReqLatency},
		{"org.apache.kafka.producer.produce.throttle.time", &m.pThrottle},
		{"org.apache.kafka.consumer.connection.creation", &m.cConnCreation},
		{"org.apache.kafka.consumer.node.request.latency", &m.cReqLatency},
		{"org.apache.kafka.consumer.coordinator.commit.latency", &m.cCommitLatency},
	} {
		switch t := s.v.(type) {
		case *metricRate:
			rate, tot, lastTot := t.rollNums()
			appendGauge(s.name+".rate", 0, rate, nil)
			appendSum(s.name+".total", tot, lastTot, nil)

		case *metricTime:
			avg, max := t.rollNums(aggDur)
			appendGauge(s.name+".avg", 0, avg, nil)
			appendGauge(s.name+".max", max, 0, nil)

		case *map[int32]*metricTime:
			for broker, m := range *t {
				avg, max := m.rollNums(aggDur)
				attrs := map[string]any{"node_id": broker}
				appendGauge(s.name+".avg", 0, avg, attrs)
				appendGauge(s.name+".max", max, 0, attrs)
				// By-node metrics do not write the forever-total-latency.
				// We only have per-push increments, so we can safely delete
				// the node every round (i.e., clean up if a broker goes away).
				if avg == 0 && max == 0 {
					delete(*t, broker)
				}
			}

		default:
			m.mu.Unlock()
			panic("unsupported type")
		}
	}
	m.mu.Unlock()

	userAt := len(*metrics)
	if m.cl.cfg.userMetrics != nil {
		var userSkipped []string
		last := m.userSumLast
		if last == nil {
			last = make(map[string]int64)
		}
		m.userSumLast = make(map[string]int64)
		for um := range m.cl.cfg.userMetrics() {
			if um.ValueInt != 0 && um.ValueFloat != 0 || um.ValueInt == 0 && um.ValueFloat == 0 {
				userSkipped = append(userSkipped, um.Name)
				continue
			}

			switch um.Type {
			case MetricTypeSum:
				lastTot := last[um.Name]
				if um.ValueFloat != 0 || um.ValueInt < 0 || lastTot > um.ValueInt {
					userSkipped = append(userSkipped, um.Name)
					continue
				}
				appendSum(um.Name, um.ValueInt, lastTot, um.Attrs)
				m.userSumLast[um.Name] = um.ValueInt

			case MetricTypeGauge:
				appendGauge(um.Name, um.ValueInt, um.ValueFloat, um.Attrs)
			}
		}
		if len(userSkipped) > 0 {
			m.cl.cfg.logger.Log(LogLevelWarn, "skipped serialization of some user provided metrics", "skipped", userSkipped)
		}
	}

	////////////
	// SERIALIZING - finally append metricsData to b
	////////////

	serialized, codec := metricsData.appendTo(b[:0]), CodecNone

	if compressor != nil {
		serialized, codec = compressor.Compress(new(bytes.Buffer), serialized)
	}

	if len(serialized) > int(maxBytes) && userAt < len(*metrics) {
		m.cl.cfg.logger.Log(LogLevelWarn, "adding user metrics results in a too-large payload; dropping user metrics and serializing only standard client metrics",
			"max_bytes", maxBytes,
			"serialized_bytes_with_user", len(serialized),
		)
		*metrics = (*metrics)[:userAt]
		serialized, codec = metricsData.appendTo(b[:0]), CodecNone
		if compressor != nil {
			serialized, codec = compressor.Compress(new(bytes.Buffer), serialized)
		}
	}
	return serialized, int8(codec), len(*metrics)
}

// The types below are encoded in protobuf format; canonical
// definitions can be found at:
// https://github.com/open-telemetry/opentelemetry-proto/blob/f24da8deeb50118271c9435972791ef05ec003b1/opentelemetry/proto/metrics/v1/metrics.proto
type (
	otelMetricsData struct {
		// resourceMetrics is repeated, but we always use one element.
		resourceMetric otelResourceMetric // = 1
	}

	otelResourceMetric struct {
		// We do not encode resource if resource.attributes is empty;
		// eliding resource just means it is not known.
		resource otelResource // = 1

		// scopeMetric is repeated, but we always use one element.
		scopeMetric otelScopeMetric // = 2
	}

	// From a separate file:
	// https://github.com/open-telemetry/opentelemetry-proto/blob/f24da8deeb50118271c9435972791ef05ec003b1/opentelemetry/proto/resource/v1/resource.proto
	// We only use attributes, and only if any exist.
	otelResource struct {
		attributes map[string]any // = 1
	}

	otelScopeMetric struct {
		scope   otelInstrumentationScope // = 1
		metrics []otelMetric             // = 2
	}

	// From a separate file:
	// https://github.com/open-telemetry/opentelemetry-proto/blob/f24da8deeb50118271c9435972791ef05ec003b1/opentelemetry/proto/common/v1/common.proto
	// We use "kgo" and our released version (via the `softwareVersion()` func).
	otelInstrumentationScope struct {
		name    string // = 1
		version string // = 2
	}

	otelMetric struct {
		name string // = 1

		// The metric data can be oneOf the following two:
		gauge otelGauge // = 5
		sum   otelSum   // = 7
		// We use the non-empty struct; one must be non-empty.
	}

	otelGauge struct {
		// The .proto defines this as `repeated`, but we always use
		// only one data point.
		dataPoint otelNumDataPoint // = 1
	}

	otelSum struct {
		// Same as otelGauge, the .proto defines this as `repeated`
		// but we only use one data point.
		dataPoint otelNumDataPoint // = 1

		// aggregationTemporality is an enum;
		// 0 is unspecified
		// 1 is delta
		// 2 is cumulative
		aggregationTemporality uint8 // = 2

		// We always set isMonotonic to true.
		isMonotonic bool // = 3
	}

	otelNumDataPoint struct {
		// Encoded as key/value attributes; we currently only use int32
		// broker IDs. This will need to change if other types are
		// needed.
		attributes map[string]any // = 7

		startNano int64 // = 2
		timeNano  int64 // = 3

		// Only one of vDouble or vInt is non-zero, only
		// one is used (this is oneof).
		vDouble float64 // = 4
		vInt    int64   // = 6
	}
)

const (
	otelTempDelta      = 1
	otelTempCumulative = 2
)

////////////
// SERIALIZATION
////////////

const (
	protoTypeVarint = 0
	protoType64bit  = 1
	protoTypeLength = 2
	protoType32bit  = 5
)

// appendProtoTag adds a Protocol Buffer tag (field number + proto type)
func appendProtoTag(b []byte, fieldNumber, protoType int) []byte {
	return binary.AppendUvarint(b, uint64((fieldNumber<<3)|protoType))
}

// appendProtoString appends a string as length-prefixed bytes
func appendProtoString(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

func (d *otelMetricsData) appendTo(b []byte) []byte {
	// Field 1: resourceMetric (message)
	b = appendProtoTag(b, 1, protoTypeLength)
	resourceBytes := d.resourceMetric.appendTo(nil)
	b = binary.AppendUvarint(b, uint64(len(resourceBytes)))
	b = append(b, resourceBytes...)

	return b
}

func (m *otelResourceMetric) appendTo(b []byte) []byte {
	// Field 1: resource (message) - only if attributes are present
	if len(m.resource.attributes) > 0 {
		b = appendProtoTag(b, 1, protoTypeLength)
		resourceBytes := m.resource.appendTo(nil)
		b = binary.AppendUvarint(b, uint64(len(resourceBytes)))
		b = append(b, resourceBytes...)
	}

	// Field 2: scopeMetric (message)
	b = appendProtoTag(b, 2, protoTypeLength)
	scopeBytes := m.scopeMetric.appendTo(nil)
	b = binary.AppendUvarint(b, uint64(len(scopeBytes)))
	b = append(b, scopeBytes...)

	return b
}

func (r *otelResource) appendTo(b []byte) []byte {
	// Field 1: attributes (repeated KeyValue)
	return appendOtelAttributesTo(b, 1, r.attributes)
}

func (s *otelScopeMetric) appendTo(b []byte) []byte {
	// Field 1: scope (message)
	b = appendProtoTag(b, 1, protoTypeLength)
	scopeBytes := s.scope.appendTo(nil)
	b = binary.AppendUvarint(b, uint64(len(scopeBytes)))
	b = append(b, scopeBytes...)

	// Field 2: metrics (repeated message)
	for _, m := range s.metrics {
		b = appendProtoTag(b, 2, protoTypeLength)
		metricBytes := m.appendTo(nil)
		b = binary.AppendUvarint(b, uint64(len(metricBytes)))
		b = append(b, metricBytes...)
	}

	return b
}

func (s *otelInstrumentationScope) appendTo(b []byte) []byte {
	// Field 1: name (string)
	if s.name != "" {
		b = appendProtoTag(b, 1, protoTypeLength)
		b = appendProtoString(b, s.name)
	}
	// Field 2: version (string)
	if s.version != "" {
		b = appendProtoTag(b, 2, protoTypeLength)
		b = appendProtoString(b, s.version)
	}
	return b
}

func (m *otelMetric) appendTo(b []byte) []byte {
	// Field 1: name (string)
	if m.name != "" {
		b = appendProtoTag(b, 1, protoTypeLength)
		b = appendProtoString(b, m.name)
	}

	// Field 5: gauge (message) - if used
	if m.gauge.dataPoint.timeNano != 0 {
		b = appendProtoTag(b, 5, protoTypeLength)
		gaugeBytes := m.gauge.appendTo(nil)
		b = binary.AppendUvarint(b, uint64(len(gaugeBytes)))
		b = append(b, gaugeBytes...)
	}

	// Field 7: sum (message) - if used
	if m.sum.dataPoint.timeNano != 0 {
		b = appendProtoTag(b, 7, protoTypeLength)
		sumBytes := m.sum.appendTo(nil)
		b = binary.AppendUvarint(b, uint64(len(sumBytes)))
		b = append(b, sumBytes...)
	}
	return b
}

func (g *otelGauge) appendTo(b []byte) []byte {
	// Field 1: dataPoints (repeated message)
	b = appendProtoTag(b, 1, protoTypeLength)
	dataPointBytes := g.dataPoint.appendTo(nil)
	b = binary.AppendUvarint(b, uint64(len(dataPointBytes)))
	b = append(b, dataPointBytes...)

	return b
}

func (s *otelSum) appendTo(b []byte) []byte {
	// Field 1: dataPoints (repeated message)
	b = appendProtoTag(b, 1, protoTypeLength)
	dataPointBytes := s.dataPoint.appendTo(nil)
	b = binary.AppendUvarint(b, uint64(len(dataPointBytes)))
	b = append(b, dataPointBytes...)

	// Field 2: aggregationTemporality (enum)
	if s.aggregationTemporality != 0 {
		b = appendProtoTag(b, 2, protoTypeVarint)
		b = binary.AppendUvarint(b, uint64(s.aggregationTemporality))
	}

	// Field 3: isMonotonic (bool)
	if s.isMonotonic {
		b = appendProtoTag(b, 3, protoTypeVarint)
		b = binary.AppendUvarint(b, 1) // true
	}

	return b
}

func (d *otelNumDataPoint) appendTo(b []byte) []byte {
	// Field 2: startTimeUnixNano (fixed64)
	if d.startNano != 0 {
		b = appendProtoTag(b, 2, protoType64bit)
		b = binary.LittleEndian.AppendUint64(b, uint64(d.startNano))
	}

	// Field 3: timeUnixNano (fixed64)
	b = appendProtoTag(b, 3, protoType64bit)
	b = binary.LittleEndian.AppendUint64(b, uint64(d.timeNano))

	// Field 4: asDouble (double)
	if d.vDouble != 0 {
		b = appendProtoTag(b, 4, protoType64bit)
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(d.vDouble))
	}

	// Field 6: asInt (sfixed64)
	if d.vInt != 0 {
		b = appendProtoTag(b, 6, protoType64bit)
		b = binary.LittleEndian.AppendUint64(b, uint64(d.vInt))
	}

	// Field 7: attributes
	b = appendOtelAttributesTo(b, 7, d.attributes)

	return b
}

func appendOtelAttributesTo(b []byte, fieldNumber int, attrs map[string]any) []byte {
outer:
	for key, value := range attrs {
		b = appendProtoTag(b, fieldNumber, protoTypeLength)

		kvBytes := []byte{}
		// Field 1: key (string)
		kvBytes = appendProtoTag(kvBytes, 1, protoTypeLength)
		kvBytes = appendProtoString(kvBytes, key)

		// Field 2: value (AnyValue)
		kvBytes = appendProtoTag(kvBytes, 2, protoTypeLength)

		var anyValueBytes []byte
		switch t := value.(type) {
		case *string, string:
			var v string
			switch t := t.(type) {
			case string:
				v = t
			case *string:
				if t != nil {
					v = *t
				}
			}
			anyValueBytes = appendProtoTag(anyValueBytes, 1, protoTypeLength) // string_value = 1
			anyValueBytes = appendProtoString(anyValueBytes, v)
		case bool:
			anyValueBytes = appendProtoTag(anyValueBytes, 2, protoTypeVarint) // bool_value = 2
			if t {
				anyValueBytes = binary.AppendUvarint(anyValueBytes, 1)
			} else {
				anyValueBytes = binary.AppendUvarint(anyValueBytes, 0)
			}
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr:
			anyValueBytes = appendProtoTag(anyValueBytes, 3, protoTypeVarint) // int_value = 3
			var v uint64
			switch t := t.(type) {
			case int:
				v = uint64(t)
			case int8:
				v = uint64(t)
			case int16:
				v = uint64(t)
			case int32:
				v = uint64(t)
			case int64:
				v = uint64(t)
			case uint:
				v = uint64(t)
			case uint8:
				v = uint64(t)
			case uint16:
				v = uint64(t)
			case uint32:
				v = uint64(t)
			case uint64:
				v = t
			case uintptr:
				v = uint64(t)
			}
			anyValueBytes = binary.AppendUvarint(anyValueBytes, v)
		case float32, float64:
			var v float64
			switch t := t.(type) {
			case float32:
				v = float64(t)
			case float64:
				v = t
			}
			anyValueBytes = appendProtoTag(anyValueBytes, 4, protoType64bit) // double_value = 4
			anyValueBytes = binary.LittleEndian.AppendUint64(anyValueBytes, math.Float64bits(v))
		case []byte:
			anyValueBytes = appendProtoTag(anyValueBytes, 7, protoTypeLength) // bytes_value = 7
			anyValueBytes = binary.AppendUvarint(anyValueBytes, uint64(len(t)))
			anyValueBytes = append(anyValueBytes, t...)
		default:
			continue outer
		}

		kvBytes = binary.AppendUvarint(kvBytes, uint64(len(anyValueBytes)))
		kvBytes = append(kvBytes, anyValueBytes...)

		b = binary.AppendUvarint(b, uint64(len(kvBytes)))
		b = append(b, kvBytes...)
	}
	return b
}
