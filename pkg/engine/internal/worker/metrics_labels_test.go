package worker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

type metricLabelSet map[string]string

func TestWorkerHandlerPhaseLabelsForTaskAssign(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	listener := &wire.Local{Address: wire.LocalScheduler}
	t.Cleanup(func() { require.NoError(t, listener.Close(context.Background())) })

	acceptedConn := make(chan wire.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			acceptErr <- err
			return
		}
		acceptedConn <- conn
	}()

	workerConn, err := listener.DialFrom(ctx, wire.LocalWorker)
	require.NoError(t, err)

	var schedulerConn wire.Conn
	select {
	case err := <-acceptErr:
		require.NoError(t, err)
	case schedulerConn = <-acceptedConn:
	}

	helloReceived := make(chan struct{})
	schedulerPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    schedulerConn,
		Handler: func(_ context.Context, _ *wire.Peer, msg wire.Message) error {
			if _, ok := msg.(wire.WorkerHelloMessage); ok {
				select {
				case <-helloReceived:
				default:
					close(helloReceived)
				}
			}
			return nil
		},
	}
	go func() { _ = schedulerPeer.Serve(ctx) }()

	m := newMetrics()
	w := &Worker{
		logger:      log.NewNopLogger(),
		numThreads:  1,
		wireMetrics: wire.NewMetrics(),
		metrics:     m,
		jobManager:  newJobManager(),
		sources:     make(map[ulid.ULID]*streamSource),
		sinks:       make(map[ulid.ULID]*streamSink),
		jobs:        make(map[ulid.ULID]*threadJob),
	}

	workerDone := make(chan error, 1)
	go func() { workerDone <- w.handleSchedulerConn(ctx, log.NewNopLogger(), workerConn) }()

	select {
	case <-helloReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for WorkerHello")
	}

	err = schedulerPeer.SendMessage(ctx, wire.TaskAssignMessage{
		Task: &workflow.Task{
			ULID:     ulid.Make(),
			TenantID: "test-tenant",
		},
		Metadata: http.Header{},
	})
	var wireErr *wire.Error
	require.ErrorAs(t, err, &wireErr)
	require.EqualValues(t, http.StatusTooManyRequests, wireErr.Code)

	// The handler now records only its total phase: the lock_wait phase is
	// covered by the worker lock_wait_seconds metric and the job_manager_send
	// phase by job_handoff_seconds.
	require.ElementsMatch(t, []metricLabelSet{
		{"message_type": wire.TaskAssignMessage{}.Kind().String(), "phase": phaseTotal.String(), "outcome": outcomeNack.String()},
	}, collectLabelSets(t, m.handlerPhaseSeconds))

	cancel()
	select {
	case err := <-workerDone:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, wire.ErrConnClosed) {
			require.NoError(t, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker connection to stop")
	}
}

func TestWorkerHandlerPhaseLabelsForStreamData(t *testing.T) {
	streamID := ulid.Make()
	source := &streamSource{}
	node := &nodeSource{}
	require.NoError(t, source.Bind(node))

	readDone := make(chan error, 1)
	go func() {
		_, err := node.Read(t.Context())
		readDone <- err
	}()

	m := newMetrics()
	w := &Worker{
		metrics: m,
		sources: map[ulid.ULID]*streamSource{streamID: source},
	}
	rec := arrowtest.Rows{{"a": int64(1)}}.Record(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil))

	var err error
	func() {
		phases := m.startHandler(wire.StreamDataMessage{}.Kind())
		defer func() { phases.Done(handlerOutcome(err)) }()
		err = w.handleDataMessage(t.Context(), wire.StreamDataMessage{StreamID: streamID, Data: rec}, phases)
	}()
	require.NoError(t, err)
	require.NoError(t, <-readDone)

	// lock_wait is no longer recorded as a handler phase (it lives in
	// lock_wait_seconds); source_write_wait and the total remain.
	require.ElementsMatch(t, []metricLabelSet{
		{"message_type": wire.StreamDataMessage{}.Kind().String(), "phase": phaseTotal.String(), "outcome": outcomeAck.String()},
		{"message_type": wire.StreamDataMessage{}.Kind().String(), "phase": phaseSourceWriteWait.String(), "outcome": outcomeAck.String()},
	}, collectLabelSets(t, m.handlerPhaseSeconds))
}

func TestWorkerStreamDataCommSiteLabels(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	listener := &wire.Local{Address: wire.LocalWorker}
	t.Cleanup(func() { require.NoError(t, listener.Close(context.Background())) })

	received := make(chan wire.StreamDataMessage, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		peer := &wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(_ context.Context, _ *wire.Peer, msg wire.Message) error {
				if data, ok := msg.(wire.StreamDataMessage); ok {
					received <- data
				}
				time.Sleep(time.Millisecond)
				return nil
			},
		}
		_ = peer.Serve(ctx)
	}()

	m := newMetrics()
	streamID := ulid.Make()
	sink := &streamSink{
		Logger:      log.NewNopLogger(),
		Metrics:     m,
		WireMetrics: wire.NewMetrics(),
		Stream:      &workflow.Stream{ULID: streamID, TenantID: "test-tenant"},
		TaskType:    taskTypeLeaf,
		Dialer: func(ctx context.Context, _ net.Addr) (wire.Conn, error) {
			return listener.DialFrom(ctx, wire.LocalScheduler)
		},
	}
	sink.lazyInit()
	t.Cleanup(sink.cancel)
	sink.destination = wire.LocalWorker
	close(sink.bound)

	rec := arrowtest.Rows{{"a": int64(1)}}.Record(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil))
	require.NoError(t, sink.Send(ctx, rec))
	select {
	case got := <-received:
		require.Equal(t, streamID, got.StreamID)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for StreamDataMessage")
	}

	// The stream-data send site records its wait as the attribution layer. Slot
	// comm/compute utilization is accumulated separately on the main runJob
	// goroutine and is exercised by the slot_phase tests.
	require.ElementsMatch(t, []metricLabelSet{
		{"site": "stream_data_sync", "mode": sendModeSync.String(), "message_type": wire.StreamDataMessage{}.Kind().String(), "task_type": taskTypeLeaf.String(), "outcome": outcomeSuccess.String()},
	}, collectLabelSets(t, m.commSiteWaitSeconds))
}

func collectLabelSets(t *testing.T, collector prometheus.Collector) []metricLabelSet {
	t.Helper()

	metrics := make(chan prometheus.Metric)
	go func() {
		defer close(metrics)
		collector.Collect(metrics)
	}()

	var labelSets []metricLabelSet
	for metric := range metrics {
		var dtoMetric dto.Metric
		require.NoError(t, metric.Write(&dtoMetric))

		labels := make(metricLabelSet, len(dtoMetric.Label))
		for _, label := range dtoMetric.Label {
			labels[label.GetName()] = label.GetValue()
		}
		labelSets = append(labelSets, labels)
	}
	sort.Slice(labelSets, func(i, j int) bool { return labelSetKey(labelSets[i]) < labelSetKey(labelSets[j]) })
	return labelSets
}

func labelSetKey(labels metricLabelSet) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out string
	for _, key := range keys {
		out += key + "=" + labels[key] + ";"
	}
	return out
}
