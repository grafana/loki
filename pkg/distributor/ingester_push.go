package distributor

import (
	"context"

	"github.com/gogo/status"
	"github.com/grafana/dskit/ring"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/atomic"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// TODO taken from Cortex, see if we can refactor out an usable interface.
type streamTracker struct {
	KeyedStream
	minSuccess  int
	maxFailures int
	succeeded   atomic.Int32
	failed      atomic.Int32
}

type pushIngesterTask struct {
	streamTracker []*streamTracker
	pushTracker   *PushTracker
	ingester      ring.InstanceDesc
	ctx           context.Context
	cancel        context.CancelFunc
}

// sendStreamsToIngesters resolves the replication set of each stream, groups
// the streams by ingester and queues one push task per ingester for the
// pushIngesterWorker pool. Results are reported through tracker; the returned
// error is only for ring lookup failures, in which case nothing is queued.
func (d *Distributor) sendStreamsToIngesters(ctx context.Context, streams []KeyedStream, tracker *PushTracker) error {
	const maxExpectedReplicationSet = 5 // typical replication factor 3 plus one for inactive plus one for luck
	var descs [maxExpectedReplicationSet]ring.InstanceDesc

	streamTrackers := make([]streamTracker, len(streams))
	streamsByIngester := map[string][]*streamTracker{}
	ingesterDescs := map[string]ring.InstanceDesc{}

	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("started to query ingesters ring")
	for i, stream := range streams {
		replicationSet, err := d.ingestersRing.Get(stream.HashKey, ring.WriteNoExtend, descs[:0], nil, nil)
		if err != nil {
			sp.AddEvent("finished to query ingesters ring")
			return err
		}

		streamTrackers[i] = streamTracker{
			KeyedStream: stream,
			minSuccess:  len(replicationSet.Instances) - replicationSet.MaxErrors,
			maxFailures: replicationSet.MaxErrors,
		}
		for _, ingester := range replicationSet.Instances {
			streamsByIngester[ingester.Addr] = append(streamsByIngester[ingester.Addr], &streamTrackers[i])
			ingesterDescs[ingester.Addr] = ingester
		}
	}
	sp.AddEvent("finished to query ingesters ring")

	for addr, samples := range streamsByIngester {
		// Clone the context using WithoutCancel, which is not canceled when parent is canceled.
		// This is to make sure all ingesters get samples even if we return early
		localCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.clientCfg.RemoteTimeout)
		localCtx = trace.ContextWithSpan(localCtx, sp)

		select {
		case <-ctx.Done():
			cancel()
		case d.ingesterTasks <- pushIngesterTask{
			ingester:      ingesterDescs[addr],
			streamTracker: samples,
			pushTracker:   tracker,
			ctx:           localCtx,
			cancel:        cancel,
		}:
		}
	}
	return nil
}

func (d *Distributor) pushIngesterWorker(ctx context.Context) {
	defer d.ingesterTaskWg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-d.ingesterTasks:
			d.sendStreams(task)
		}
	}
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendStreams(task pushIngesterTask) {
	defer task.cancel()
	err := d.sendStreamsErr(task.ctx, task.ingester, task.streamTracker)

	// If we succeed, decrement each stream's pending count by one.
	// If we reach the required number of successful puts on this stream, then
	// decrement the number of pending streams by one.
	// If we successfully push all streams to min success ingesters, wake up the
	// waiting rpc so it can return early. Similarly, track the number of errors,
	// and if it exceeds maxFailures shortcut the waiting rpc.
	//
	// The use of atomic increments here guarantees only a single sendStreams
	// goroutine will write to either channel.
	for i := range task.streamTracker {
		if err != nil {
			if task.streamTracker[i].failed.Inc() <= int32(task.streamTracker[i].maxFailures) {
				continue
			}
			task.pushTracker.doneWithResult(err)
		} else {
			if task.streamTracker[i].succeeded.Inc() != int32(task.streamTracker[i].minSuccess) {
				continue
			}
			task.pushTracker.doneWithResult(nil)
		}
	}
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendStreamsErr(ctx context.Context, ingester ring.InstanceDesc, streams []*streamTracker) error {
	c, err := d.ingesterClients.GetClientFor(ingester.Addr)
	if err != nil {
		return err
	}

	req := &logproto.PushRequest{
		Streams: make([]logproto.Stream, len(streams)),
	}
	for i, s := range streams {
		// Ingester RPCs serialize the flat view without any modifications.
		req.Streams[i] = s.Stream.FlatView()
	}

	_, err = c.(logproto.PusherClient).Push(ctx, req)
	d.m.ingesterAppends.WithLabelValues(ingester.Addr).Inc()
	if err != nil {
		if e, ok := status.FromError(err); ok {
			switch e.Code() {
			case codes.DeadlineExceeded:
				d.m.ingesterAppendTimeouts.WithLabelValues(ingester.Addr).Inc()
			}
		}
	}
	return err
}
