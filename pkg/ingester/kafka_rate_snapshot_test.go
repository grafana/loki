package ingester

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// newManualStreamRateCalculator leaves sample rotation to the test.
func newManualStreamRateCalculator() *StreamRateCalculator {
	return newStreamRateCalculator(nil)
}

func newManualKafkaStreamRateCalculator() *StreamRateCalculator {
	return newStreamRateCalculator(newKafkaRateState(0, "test-ingester"))
}

// newReadyKafkaStreamRateCalculator skips the startup sample; tests rotate later samples explicitly.
func newReadyKafkaStreamRateCalculator(nextOffset int64) *StreamRateCalculator {
	c := newManualKafkaStreamRateCalculator()
	c.setKafkaHighWatermark(nextOffset)
	c.markKafkaConsumerRunning()
	c.updateRates()
	return c
}

func TestKafkaRateSnapshotCompletion(t *testing.T) {
	c := newManualKafkaStreamRateCalculator()
	c.setKafkaHighWatermark(6)
	batch := c.trackKafkaBatch([]partition.Record{{Offset: 0}, {Offset: 2}, {Offset: 3}, {Offset: 4}, {Offset: 5}})
	observation := &rateObservation{}
	ctx := context.WithValue(context.Background(), rateObservationKey{}, observation)
	c.markKafkaConsumerRunning()

	// Reuse the observation across records, including zero-byte accounting and an early rejection.
	for _, step := range []struct {
		name      string
		index     int
		record    bool
		hash      uint64
		bytes     int
		wantEnd   int64
		wantReady bool
	}{
		{name: "out of order completion", index: 1, record: true, hash: 9, bytes: 20, wantEnd: -1},
		{name: "close offset gap", index: 0, record: true, hash: 9, bytes: 10, wantEnd: 2, wantReady: true},
		{name: "next record", index: 2, record: true, hash: 9, bytes: 30, wantEnd: 3, wantReady: true},
		{name: "zero-valued observation", index: 3, record: true, wantEnd: 4, wantReady: true},
		{name: "rejected before recording", index: 4, wantEnd: 5, wantReady: true},
	} {
		previous := c.buildRatesResponse()
		previousEnd := previous.Kafka.EndOffset
		if step.record {
			c.Record(ctx, "tenant", step.hash, step.hash, step.bytes)
		}
		c.recordKafkaAttempt(batch, step.index, observation, true)
		require.Zero(t, *observation, "%s: completed attempts must clear the reusable observation", step.name)
		c.updateRates()

		got := c.buildRatesResponse()
		require.Equal(t, step.wantEnd, got.Kafka.EndOffset, step.name)
		require.Equal(t, step.wantReady, got.Kafka.Ready, step.name)
		require.True(t, got.Kafka.ProgressValid, step.name)
		if step.record {
			require.Len(t, got.StreamRates, 1, step.name)
			require.Equal(t, step.hash, got.StreamRates[0].StreamHash, step.name)
			require.EqualValues(t, step.bytes, got.StreamRates[0].Rate, step.name)
			require.EqualValues(t, 1, got.StreamRates[0].Pushes, step.name)
		} else {
			require.Empty(t, got.StreamRates, "%s: the previous record's observation must not be reused", step.name)
		}
		require.Equal(t, previousEnd, previous.Kafka.EndOffset, "%s: old snapshots must not acquire live progress", step.name)
	}
}

func TestKafkaRateSnapshotUntrackedPush(t *testing.T) {
	c := newReadyKafkaStreamRateCalculator(1)
	batch := c.trackKafkaBatch([]partition.Record{{Offset: 0}})
	obs := &rateObservation{}
	ctx := context.WithValue(context.Background(), rateObservationKey{}, obs)
	c.Record(ctx, "tenant", 9, 7, 10)
	c.recordKafkaAttempt(batch, 0, obs, true)
	c.Record(context.Background(), "tenant", 9, 7, 20)
	c.updateRates()
	got := c.buildRatesResponse()
	require.False(t, got.Kafka.ProgressValid)
	require.EqualValues(t, 0, got.Kafka.EndOffset)
	require.Len(t, got.StreamRates, 1)
	require.EqualValues(t, 30, got.StreamRates[0].Rate, "untracked contributions remain available to fallback")
	c.updateRates()
	require.True(t, c.buildRatesResponse().Kafka.ProgressValid, "an untracked push only invalidates its own sample")
}

func TestKafkaRateSnapshotConcurrentRecording(t *testing.T) {
	const count, workers, entryBytes = 8000, 8, 100
	for _, tc := range []struct {
		name   string
		rotate bool
	}{
		{name: "publish after recording"},
		{name: "publish while recording", rotate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newManualKafkaStreamRateCalculator()
			c.setKafkaHighWatermark(count)
			c.markKafkaConsumerRunning()
			records := make([]partition.Record, count)
			for i := range records {
				records[i].Offset = int64(i)
			}
			batch := c.trackKafkaBatch(records)
			var bytes, pushes int64
			lastOffset := int64(-1)
			validProgress := true
			observe := func() {
				c.updateRates()
				r := c.buildRatesResponse()
				for _, rate := range r.StreamRates {
					bytes += rate.Rate
					pushes += int64(rate.Pushes)
				}
				if r.Kafka.EndOffset < lastOffset || r.Kafka.EndOffset >= pushes {
					validProgress = false
				}
				lastOffset = r.Kafka.EndOffset
			}
			done, stopped := make(chan struct{}), make(chan struct{})
			if tc.rotate {
				go func() {
					defer close(stopped)
					for {
						observe()
						select {
						case <-done:
							observe()
							return
						default:
						}
					}
				}()
			}
			var wg sync.WaitGroup
			for worker := range workers {
				wg.Go(func() {
					obs := &rateObservation{}
					ctx := context.WithValue(context.Background(), rateObservationKey{}, obs)
					for i := worker; i < len(records); i += workers {
						c.Record(ctx, "tenant", uint64(i%17), 7, entryBytes)
						c.recordKafkaAttempt(batch, i, obs, true)
					}
				})
			}
			wg.Wait()

			if tc.rotate {
				close(done)
				<-stopped
			} else {
				observe()
			}
			require.EqualValues(t, count*entryBytes, bytes)
			require.EqualValues(t, count, pushes)
			require.True(t, validProgress, "published progress must be monotonic and must not outrun published contributions")
			require.EqualValues(t, count-1, lastOffset)
		})
	}
}

func TestKafkaRateSnapshotRetries(t *testing.T) {
	for _, tc := range []struct {
		name                string
		retrySucceeds       bool
		skipFailedRecording bool
		rotateBeforeCancel  bool
		wantBytes           int64
		wantPushes          uint32
	}{
		{name: "retry succeeds", retrySucceeds: true, wantBytes: 30, wantPushes: 2},
		{name: "retry cancelled", wantBytes: 10, wantPushes: 1},
		{name: "retry succeeds after early rejection", skipFailedRecording: true, retrySucceeds: true, wantBytes: 20, wantPushes: 1},
		{name: "retry cancelled after early rejection", skipFailedRecording: true},
		{name: "retry cancelled after sample rotation", rotateBeforeCancel: true},
		{name: "retry cancelled after early rejection and sample rotation", skipFailedRecording: true, rotateBeforeCancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newReadyKafkaStreamRateCalculator(1)
			batch := c.trackKafkaBatch([]partition.Record{{Offset: 0}})
			observation := &rateObservation{}
			ctx := context.WithValue(context.Background(), rateObservationKey{}, observation)
			if !tc.skipFailedRecording {
				c.Record(ctx, "tenant", 9, 7, 10)
			}
			c.recordKafkaAttempt(batch, 0, observation, false)
			require.Zero(t, *observation, "failed attempts must not leave contributions for the next attempt")
			require.Zero(t, batch.next, "a retryable failure must not advance completed progress")
			if tc.rotateBeforeCancel {
				c.updateRates()
				response := c.buildRatesResponse()
				require.False(t, response.Kafka.ProgressValid)
				require.EqualValues(t, -1, response.Kafka.EndOffset)
			}
			if tc.retrySucceeds {
				c.Record(ctx, "tenant", 9, 7, 20)
				c.recordKafkaAttempt(batch, 0, observation, true)
			} else {
				c.completeKafkaRecordTracking(batch, 0)
			}

			c.updateRates()
			response := c.buildRatesResponse()
			if tc.wantPushes == 0 {
				require.Empty(t, response.StreamRates)
			} else {
				require.Len(t, response.StreamRates, 1)
				require.Equal(t, tc.wantBytes, response.StreamRates[0].Rate)
				require.Equal(t, tc.wantPushes, response.StreamRates[0].Pushes)
			}
			require.False(t, response.Kafka.ProgressValid, "failed or cancelled attempts must invalidate their sample")
			require.EqualValues(t, 0, response.Kafka.EndOffset)
			c.updateRates()
			response = c.buildRatesResponse()
			require.True(t, response.Kafka.ProgressValid)
			require.Empty(t, response.StreamRates, "rotation must not publish attempt contributions twice")
			require.EqualValues(t, 0, response.Kafka.EndOffset)
		})
	}
}

func TestKafkaRateSnapshotConcurrentResponses(t *testing.T) {
	c := newManualKafkaStreamRateCalculator()
	const count = 200
	c.setKafkaHighWatermark(count)
	records := make([]partition.Record, count)
	for i := range records {
		records[i].Offset = int64(i)
	}
	batch := c.trackKafkaBatch(records)
	done := make(chan struct{})
	go func() {
		defer close(done)
		observation := &rateObservation{}
		ctx := context.WithValue(context.Background(), rateObservationKey{}, observation)
		for i := range records {
			// Each sample's byte count identifies the offset it was published with.
			c.Record(ctx, "tenant", 9, 7, i+1)
			c.recordKafkaAttempt(batch, i, observation, true)
			c.updateRates()
		}

	}()
	t.Cleanup(func() { <-done })

	check := func(response *logproto.StreamRatesResponse) {
		if len(response.StreamRates) == 0 {
			return // No sample has been published yet.
		}
		require.Len(t, response.StreamRates, 1)
		require.Equal(t, response.Kafka.EndOffset+1, response.StreamRates[0].Rate, "rates and progress must come from the same snapshot")
	}
	for {
		check(c.buildRatesResponse())
		select {
		case <-done:
			response := c.buildRatesResponse()
			check(response)
			require.EqualValues(t, count-1, response.Kafka.EndOffset)
			return
		default:
		}
	}
}

func TestKafkaRateSnapshotRecordingDuringPublication(t *testing.T) {
	c := newReadyKafkaStreamRateCalculator(2)
	batch := c.trackKafkaBatch([]partition.Record{{Offset: 0}, {Offset: 1}})
	observation := &rateObservation{}
	ctx := context.WithValue(context.Background(), rateObservationKey{}, observation)
	finish := func(index, bytes int) {
		c.Record(ctx, "tenant", 9, 7, bytes)
		c.recordKafkaAttempt(batch, index, observation, true)
	}
	finish(0, 10)

	published := make(chan struct{})
	func() {
		// Delay publication so another record completes after the sample is captured.
		c.rateLock.Lock()
		defer c.rateLock.Unlock()
		go func() {
			c.updateRates()
			close(published)
		}()
		require.Eventually(t, func() bool {
			if !c.kafka.mu.TryLock() {
				return false
			}
			defer c.kafka.mu.Unlock()
			return c.kafka.startOffset == 0
		}, time.Second, time.Millisecond, "workers must be able to record before publication finishes")
		finish(1, 20)
	}()
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("sample was not published")
	}

	first := c.buildRatesResponse()
	require.Len(t, first.StreamRates, 1)
	require.EqualValues(t, 10, first.StreamRates[0].Rate)
	require.EqualValues(t, 0, first.Kafka.EndOffset)
	c.updateRates()
	second := c.buildRatesResponse()
	require.Len(t, second.StreamRates, 1)
	require.EqualValues(t, 20, second.StreamRates[0].Rate)
	require.EqualValues(t, 0, second.Kafka.StartOffset)
	require.EqualValues(t, 1, second.Kafka.EndOffset)
	require.EqualValues(t, 10, first.StreamRates[0].Rate)
	require.EqualValues(t, 0, first.Kafka.EndOffset)
}

func TestKafkaRateSnapshotWaitsForAttempt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		completed bool
		wantEnd   int64
	}{
		{name: "completed attempt", completed: true, wantEnd: 0},
		{name: "retryable attempt", wantEnd: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newReadyKafkaStreamRateCalculator(1)
			batch := c.trackKafkaBatch([]partition.Record{{Offset: 0}})
			obs := &rateObservation{}
			c.Record(context.WithValue(context.Background(), rateObservationKey{}, obs), "tenant", 9, 7, 10)
			attemptDone, sampleDone := make(chan struct{}), make(chan struct{})
			func() {
				// Pause Kafka completion; sampling must wait for both the bytes and progress update.
				c.kafka.mu.Lock()
				defer c.kafka.mu.Unlock()
				go func() {
					c.recordKafkaAttempt(batch, 0, obs, tc.completed)
					close(attemptDone)
				}()
				require.Eventually(t, func() bool {
					if c.locks[9].TryLock() {
						c.locks[9].Unlock()
						return false
					}
					return true
				}, 5*time.Second, time.Millisecond, "attempt must hold its stripe until Kafka state is updated")
				go func() {
					c.updateRates()
					close(sampleDone)
				}()
			}()
			for _, done := range []chan struct{}{attemptDone, sampleDone} {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("attempt or sampling deadlocked")
				}
			}
			got := c.buildRatesResponse()
			require.Len(t, got.StreamRates, 1)
			require.EqualValues(t, 10, got.StreamRates[0].Rate)
			require.Equal(t, tc.completed, got.Kafka.ProgressValid)
			require.Equal(t, tc.wantEnd, got.Kafka.EndOffset)
			require.Zero(t, *obs)
		})
	}
}
