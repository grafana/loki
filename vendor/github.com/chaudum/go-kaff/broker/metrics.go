package broker

// Metrics is the observability interface for go-kaff.  Implement it to forward
// operational data to Prometheus, OpenTelemetry, StatsD, or any other system.
//
// A no-op implementation (NoopMetrics) is used when Config.Metrics is nil.
//
// All methods must be safe for concurrent use and must not block.
type Metrics interface {
	// RecordProduce is called after records are successfully written to a
	// partition.  bytes is the approximate total byte size of the records.
	RecordProduce(topic string, partition int32, records int, bytes int64)

	// RecordFetch is called after records are read from a partition during a
	// Fetch request.  bytes is the approximate total byte size of the records.
	RecordFetch(topic string, partition int32, records int, bytes int64)

	// RecordGroupRebalance is called when a consumer group completes a join
	// barrier (i.e. transitions from PreparingRebalance to CompletingRebalance).
	RecordGroupRebalance(groupID string, generation int32)

	// SetPartitionHighWaterMark is called after each successful Produce to
	// report the updated high-water mark for a partition.
	SetPartitionHighWaterMark(topic string, partition int32, hwm int64)

	// SetActiveConnections is called whenever the number of open TCP
	// connections changes.
	SetActiveConnections(n int)
}

// NoopMetrics is a Metrics implementation that silently discards all
// observations.  It is used when Config.Metrics is nil.
type NoopMetrics struct{}

func (NoopMetrics) RecordProduce(_ string, _ int32, _ int, _ int64)          {}
func (NoopMetrics) RecordFetch(_ string, _ int32, _ int, _ int64)            {}
func (NoopMetrics) RecordGroupRebalance(_ string, _ int32)                   {}
func (NoopMetrics) SetPartitionHighWaterMark(_ string, _ int32, _ int64)     {}
func (NoopMetrics) SetActiveConnections(_ int)                               {}
