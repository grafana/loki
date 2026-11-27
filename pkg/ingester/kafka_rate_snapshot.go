package ingester

import (
	"sync"

	"github.com/google/uuid"

	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// kafkaRateState keeps published rates and completed offsets consistent.
// When both locks are needed, acquire stream stripes before mu. Rate-map updates
// stay outside mu so workers on different stripes can record concurrently.
type kafkaRateState struct {
	mu                                                sync.Mutex
	partitionID                                       int32
	instanceID, sessionID                             string
	startOffset, completed, highWatermark             int64
	initialized, watermarkKnown, running, cleanSample bool
	sampleInvalid                                     bool
}

type rateObservationKey struct{}

// A Kafka push contains one stream and records at most one contribution per attempt.
type rateObservation struct {
	tenant                        string
	streamHash, streamHashNoShard uint64
	bytes                         int
	recorded                      bool // Zero-byte observations still count as a push.
}

type kafkaRateBatch struct {
	records   []partition.Record
	completed []bool
	next      int
}

func newKafkaStreamRateCalculator(partitionID int32, instanceID string) *StreamRateCalculator {
	c := newStreamRateCalculator(newKafkaRateState(partitionID, instanceID))
	go c.updateLoop()
	return c
}

func newKafkaRateState(partitionID int32, instanceID string) *kafkaRateState {
	return &kafkaRateState{
		partitionID: partitionID, instanceID: instanceID, sessionID: uuid.NewString(),
		startOffset: -1, completed: -1, highWatermark: -1,
	}
}

func (c *StreamRateCalculator) setKafkaHighWatermark(nextOffset int64) {
	state := c.kafka
	state.mu.Lock()
	defer state.mu.Unlock()
	state.highWatermark = nextOffset - 1
	state.watermarkKnown = true
}

func (c *StreamRateCalculator) markKafkaConsumerRunning() {
	state := c.kafka
	state.mu.Lock()
	defer state.mu.Unlock()
	state.running = true
}

// trackKafkaBatch begins tracking the completion of a non-empty batch.
func (c *StreamRateCalculator) trackKafkaBatch(records []partition.Record) *kafkaRateBatch {
	state := c.kafka
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.initialized {
		state.completed = records[0].Offset - 1
		state.startOffset = state.completed
		state.initialized = true
	}
	// Kafka offsets may contain gaps. Completion follows delivered record order.
	return &kafkaRateBatch{records: records, completed: make([]bool, len(records))}
}

// recordKafkaAttempt runs after every push attempt, including retries. It counts and
// clears that attempt's observation so the next attempt cannot recount it.
// Hold the stream's stripe through completion so sampling cannot split the updates.
func (c *StreamRateCalculator) recordKafkaAttempt(batch *kafkaRateBatch, index int, observation *rateObservation, completed bool) {
	var stripe *stripeLock
	if observation.recorded {
		stripe = &c.locks[observation.streamHash&uint64(c.size-1)]
		stripe.Lock()
		c.record(observation.tenant, observation.streamHash, observation.streamHashNoShard, observation.bytes)
	}
	state := c.kafka
	state.mu.Lock()
	if completed {
		state.complete(batch, index)
	} else {
		state.sampleInvalid = true
	}
	state.mu.Unlock()
	if stripe != nil {
		stripe.Unlock()
	}
	*observation = rateObservation{}
}

// recordKafkaDecodeFailure completes tracking of a record and invalidates the sample because its contribution is unknown.
func (c *StreamRateCalculator) recordKafkaDecodeFailure(batch *kafkaRateBatch, index int) {
	state := c.kafka
	state.mu.Lock()
	defer state.mu.Unlock()
	state.sampleInvalid = true
	state.complete(batch, index)
}

// completeKafkaRecordTracking ends tracking of a record when retries stop, without acknowledging the record.
func (c *StreamRateCalculator) completeKafkaRecordTracking(batch *kafkaRateBatch, index int) {
	state := c.kafka
	state.mu.Lock()
	defer state.mu.Unlock()
	// The failed attempt may belong to an earlier sample.
	state.sampleInvalid = true
	state.complete(batch, index)
}

// complete marks a record finished and advances the offset in batch order without
// passing an unfinished record. Caller must hold state.mu.
func (state *kafkaRateState) complete(batch *kafkaRateBatch, index int) {
	batch.completed[index] = true
	for batch.next < len(batch.records) && batch.completed[batch.next] {
		state.completed = batch.records[batch.next].Offset
		batch.next++
	}
}

// finishSample returns the current sample's metadata and prepares tracking for the next sample. Caller must hold state.mu.
func (state *kafkaRateState) finishSample() *logproto.KafkaRateSnapshot {
	snapshot := &logproto.KafkaRateSnapshot{
		PartitionID: state.partitionID, InstanceID: state.instanceID, SessionID: state.sessionID,
		StartOffset: state.startOffset, EndOffset: state.completed, HighWatermark: state.highWatermark,
		Ready: state.running && state.cleanSample, ProgressValid: state.initialized && state.watermarkKnown && !state.sampleInvalid,
	}
	state.startOffset = state.completed
	state.cleanSample = state.running
	state.sampleInvalid = false
	return snapshot
}

func (c *StreamRateCalculator) buildRatesResponse() *logproto.StreamRatesResponse {
	c.rateLock.RLock()
	allRates, snapshot := c.allRates, c.kafkaSnapshot
	c.rateLock.RUnlock()

	// Published data is immutable; build the response without blocking snapshot publication.
	rates := make([]*logproto.StreamRate, len(allRates))
	for i := range allRates {
		rates[i] = &allRates[i]
	}
	return &logproto.StreamRatesResponse{StreamRates: rates, Kafka: snapshot}
}
