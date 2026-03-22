package kotel

import (
	"context"
	"log"
	"math"
	"net"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.18.0"
)

var ( // interface checks to ensure we implement the hooks properly
	_ kgo.HookBrokerConnect       = new(Meter)
	_ kgo.HookBrokerDisconnect    = new(Meter)
	_ kgo.HookBrokerWrite         = new(Meter)
	_ kgo.HookBrokerRead          = new(Meter)
	_ kgo.HookProduceBatchWritten = new(Meter)
	_ kgo.HookFetchBatchRead      = new(Meter)
)

const (
	dimensionless = "1"
	bytes         = "by"
)

type Meter struct {
	provider    metric.MeterProvider
	meter       metric.Meter
	instruments instruments

	mergeConnectsMeter bool
}

// MeterOpt interface used for setting optional config properties.
type MeterOpt interface {
	apply(*Meter)
}

type meterOptFunc func(*Meter)

// MeterProvider takes a metric.MeterProvider and applies it to the Meter
// If none is specified, the global provider is used.
func MeterProvider(provider metric.MeterProvider) MeterOpt {
	return meterOptFunc(func(m *Meter) {
		if provider != nil {
			m.provider = provider
		}
	})
}

// WithMergedConnectsMeter merges the `messaging.kafka.connect_errors.count`
// counter into the `messaging.kafka.connects.count` counter, adding an
// attribute "outcome" with the values "success" or "failure". This option
// shall be used when a single metric with different dimensions is preferred
// over two separate metrics that produce data at alternating intervals.
// For example, it becomes possible to alert on the metric no longer
// producing data.
func WithMergedConnectsMeter() MeterOpt {
	return meterOptFunc(func(m *Meter) {
		m.mergeConnectsMeter = true
	})

}

func (o meterOptFunc) apply(m *Meter) {
	o(m)
}

// NewMeter returns a Meter, used as option for kotel to instrument franz-go
// with instruments.
func NewMeter(opts ...MeterOpt) *Meter {
	m := &Meter{}
	for _, opt := range opts {
		opt.apply(m)
	}
	if m.provider == nil {
		m.provider = otel.GetMeterProvider()
	}
	m.meter = m.provider.Meter(
		instrumentationName,
		metric.WithInstrumentationVersion(semVersion()),
		metric.WithSchemaURL(semconv.SchemaURL),
	)
	m.instruments = m.newInstruments()
	return m
}

// instruments ---------------------------------------------------------------

type instruments struct {
	connects    metric.Int64Counter
	connectErrs metric.Int64Counter
	disconnects metric.Int64Counter

	writeErrs  metric.Int64Counter
	writeBytes metric.Int64Counter

	readErrs  metric.Int64Counter
	readBytes metric.Int64Counter

	produceBytes   metric.Int64Counter
	produceRecords metric.Int64Counter
	fetchBytes     metric.Int64Counter
	fetchRecords   metric.Int64Counter
}

func (m *Meter) newInstruments() instruments {
	// connects and disconnects
	connects, err := m.meter.Int64Counter(
		"messaging.kafka.connects.count",
		metric.WithUnit(dimensionless),
		metric.WithDescription("Total number of connections opened, by broker"),
	)
	if err != nil {
		log.Printf("failed to create connects instrument, %v", err)
	}

	var connectErrs metric.Int64Counter
	if !m.mergeConnectsMeter {
		var err error
		connectErrs, err = m.meter.Int64Counter(
			"messaging.kafka.connect_errors.count",
			metric.WithUnit(dimensionless),
			metric.WithDescription("Total number of connection errors, by broker"),
		)
		if err != nil {
			log.Printf("failed to create connectErrs instrument, %v", err)
		}
	}

	disconnects, err := m.meter.Int64Counter(
		"messaging.kafka.disconnects.count",
		metric.WithUnit(dimensionless),
		metric.WithDescription("Total number of connections closed, by broker"),
	)
	if err != nil {
		log.Printf("failed to create disconnects instrument, %v", err)
	}

	// write

	writeErrs, err := m.meter.Int64Counter(
		"messaging.kafka.write_errors.count",
		metric.WithUnit(dimensionless),
		metric.WithDescription("Total number of write errors, by broker"),
	)
	if err != nil {
		log.Printf("failed to create writeErrs instrument, %v", err)
	}

	writeBytes, err := m.meter.Int64Counter(
		"messaging.kafka.write_bytes",
		metric.WithUnit(bytes),
		metric.WithDescription("Total number of bytes written, by broker"),
	)
	if err != nil {
		log.Printf("failed to create writeBytes instrument, %v", err)
	}

	// read

	readErrs, err := m.meter.Int64Counter(
		"messaging.kafka.read_errors.count",
		metric.WithUnit(dimensionless),
		metric.WithDescription("Total number of read errors, by broker"),
	)
	if err != nil {
		log.Printf("failed to create readErrs instrument, %v", err)
	}

	readBytes, err := m.meter.Int64Counter(
		"messaging.kafka.read_bytes.count",
		metric.WithUnit(bytes),
		metric.WithDescription("Total number of bytes read, by broker"),
	)
	if err != nil {
		log.Printf("failed to create readBytes instrument, %v", err)
	}

	// produce & consume

	produceBytes, err := m.meter.Int64Counter(
		"messaging.kafka.produce_bytes.count",
		metric.WithUnit(bytes),
		metric.WithDescription("Total number of uncompressed bytes produced, by broker and topic"),
	)
	if err != nil {
		log.Printf("failed to create produceBytes instrument, %v", err)
	}

	produceRecords, err := m.meter.Int64Counter(
		"messaging.kafka.produce_records.count",
		metric.WithUnit(dimensionless),
		metric.WithDescription("Total number of produced records, by broker and topic"),
	)
	if err != nil {
		log.Printf("failed to create produceRecords instrument, %v", err)
	}

	fetchBytes, err := m.meter.Int64Counter(
		"messaging.kafka.fetch_bytes.count",
		metric.WithUnit(bytes),
		metric.WithDescription("Total number of uncompressed bytes fetched, by broker and topic"),
	)
	if err != nil {
		log.Printf("failed to create fetchBytes instrument, %v", err)
	}

	fetchRecords, err := m.meter.Int64Counter(
		"messaging.kafka.fetch_records.count",
		metric.WithUnit(dimensionless),
		metric.WithDescription("Total number of fetched records, by broker and topic"),
	)
	if err != nil {
		log.Printf("failed to create fetchRecords instrument, %v", err)
	}

	return instruments{
		connects:    connects,
		connectErrs: connectErrs,
		disconnects: disconnects,

		writeErrs:  writeErrs,
		writeBytes: writeBytes,

		readErrs:  readErrs,
		readBytes: readBytes,

		produceBytes:   produceBytes,
		produceRecords: produceRecords,
		fetchBytes:     fetchBytes,
		fetchRecords:   fetchRecords,
	}
}

// Helpers -------------------------------------------------------------------

func strnode(node int32) string {
	if node < 0 {
		return "seed_" + strconv.Itoa(int(node)-math.MinInt32)
	}
	return strconv.Itoa(int(node))
}

// Hooks ---------------------------------------------------------------------

func (m *Meter) OnBrokerConnect(meta kgo.BrokerMetadata, _ time.Duration, _ net.Conn, err error) {
	node := strnode(meta.NodeID)

	if m.mergeConnectsMeter {
		if err != nil {
			m.instruments.connects.Add(
				context.Background(),
				1,
				metric.WithAttributeSet(attribute.NewSet(
					attribute.String("node_id", node),
					attribute.String("outcome", "failure"),
				)),
			)
			return
		}
		m.instruments.connects.Add(
			context.Background(),
			1,
			metric.WithAttributeSet(attribute.NewSet(
				attribute.String("node_id", node),
				attribute.String("outcome", "success"),
			)),
		)
		return
	}

	attributes := attribute.NewSet(attribute.String("node_id", node))
	if err != nil {
		m.instruments.connectErrs.Add(
			context.Background(),
			1,
			metric.WithAttributeSet(attributes),
		)
		return
	}
	m.instruments.connects.Add(
		context.Background(),
		1,
		metric.WithAttributeSet(attributes),
	)
}

func (m *Meter) OnBrokerDisconnect(meta kgo.BrokerMetadata, _ net.Conn) {
	node := strnode(meta.NodeID)
	attributes := attribute.NewSet(attribute.String("node_id", node))
	m.instruments.disconnects.Add(
		context.Background(),
		1,
		metric.WithAttributeSet(attributes),
	)
}

func (m *Meter) OnBrokerWrite(meta kgo.BrokerMetadata, _ int16, bytesWritten int, _, _ time.Duration, err error) {
	node := strnode(meta.NodeID)
	attributes := attribute.NewSet(attribute.String("node_id", node))
	if err != nil {
		m.instruments.writeErrs.Add(
			context.Background(),
			1,
			metric.WithAttributeSet(attributes),
		)
		return
	}
	m.instruments.writeBytes.Add(
		context.Background(),
		int64(bytesWritten),
		metric.WithAttributeSet(attributes),
	)
}

func (m *Meter) OnBrokerRead(meta kgo.BrokerMetadata, _ int16, bytesRead int, _, _ time.Duration, err error) {
	node := strnode(meta.NodeID)
	attributes := attribute.NewSet(attribute.String("node_id", node))
	if err != nil {
		m.instruments.readErrs.Add(
			context.Background(),
			1,
			metric.WithAttributeSet(attributes),
		)
		return
	}
	m.instruments.readBytes.Add(
		context.Background(),
		int64(bytesRead),
		metric.WithAttributeSet(attributes),
	)
}

func (m *Meter) OnProduceBatchWritten(meta kgo.BrokerMetadata, topic string, _ int32, pbm kgo.ProduceBatchMetrics) {
	node := strnode(meta.NodeID)
	attributes := attribute.NewSet(
		attribute.String("node_id", node),
		attribute.String("topic", topic),
	)
	m.instruments.produceBytes.Add(
		context.Background(),
		int64(pbm.UncompressedBytes),
		metric.WithAttributeSet(attributes),
	)
	m.instruments.produceRecords.Add(
		context.Background(),
		int64(pbm.NumRecords),
		metric.WithAttributeSet(attributes),
	)
}

func (m *Meter) OnFetchBatchRead(meta kgo.BrokerMetadata, topic string, _ int32, fbm kgo.FetchBatchMetrics) {
	node := strnode(meta.NodeID)
	attributes := attribute.NewSet(
		attribute.String("node_id", node),
		attribute.String("topic", topic),
	)
	m.instruments.fetchBytes.Add(
		context.Background(),
		int64(fbm.UncompressedBytes),
		metric.WithAttributeSet(attributes),
	)
	m.instruments.fetchRecords.Add(
		context.Background(),
		int64(fbm.NumRecords),
		metric.WithAttributeSet(attributes),
	)
}
