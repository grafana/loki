package ingester

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/dslog"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context"

	"github.com/grafana/loki/pkg/logproto"
	lokiutil "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	sentChunks = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_sent_chunks",
		Help:      "The total number of chunks sent by this ingester whilst leaving.",
	})
	receivedChunks = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_received_chunks",
		Help:      "The total number of chunks received by this ingester whilst joining.",
	})
)

// TransferChunks receives all chunks from another ingester. The Ingester
// must be in PENDING state or else the call will fail.
func (i *Ingester) TransferChunks(stream logproto.Ingester_TransferChunksServer) error {
	logger := dslog.WithContext(stream.Context(), util_log.Logger)
	// Prevent a shutdown from happening until we've completely finished a handoff
	// from a leaving ingester.
	i.shutdownMtx.Lock()
	defer i.shutdownMtx.Unlock()

	// Entry JOINING state (only valid from PENDING)
	if err := i.lifecycler.ChangeState(stream.Context(), ring.JOINING); err != nil {
		return err
	}

	// The ingesters state effectively works as a giant mutex around this
	// whole method, and as such we have to ensure we unlock the mutex.
	defer func() {
		state := i.lifecycler.GetState()
		if i.lifecycler.GetState() == ring.ACTIVE {
			return
		}

		level.Error(logger).Log("msg", "TransferChunks failed, not in ACTIVE state.", "state", state)

		// Enter PENDING state (only valid from JOINING)
		if i.lifecycler.GetState() == ring.JOINING {
			// Create a new context here to attempt to update the state back to pending to allow
			// a failed transfer to try again.  If we fail to set the state back to PENDING then
			// exit Loki as we will effectively be hung anyway stuck in a JOINING state and will
			// never join.
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			if err := i.lifecycler.ChangeState(ctx, ring.PENDING); err != nil {
				level.Error(logger).Log("msg", "failed to update the ring state back to PENDING after "+
					"a chunk transfer failure, there is nothing more Loki can do from this state "+
					"so the process will exit...", "err", err)
				os.Exit(1)
			}
			cancel()
		}
	}()

	fromIngesterID := ""
	seriesReceived := 0

	for {
		chunkSet, err := stream.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		// We can't send "extra" fields with a streaming call, so we repeat
		// chunkSet.FromIngesterId and assume it is the same every time around
		// this loop.
		if fromIngesterID == "" {
			fromIngesterID = chunkSet.FromIngesterId
			level.Info(logger).Log("msg", "processing TransferChunks request", "from_ingester", fromIngesterID)

			// Before transfer, make sure 'from' ingester is in correct state to call ClaimTokensFor later
			err := i.checkFromIngesterIsInLeavingState(stream.Context(), fromIngesterID)
			if err != nil {
				return errors.Wrap(err, "TransferChunks: checkFromIngesterIsInLeavingState")
			}
		}

		userCtx := user.InjectOrgID(stream.Context(), chunkSet.UserId)

		lbls := make([]labels.Label, 0, len(chunkSet.Labels))
		for _, lbl := range chunkSet.Labels {
			lbls = append(lbls, labels.Label{Name: lbl.Name, Value: lbl.Value})
		}

		instance := i.getOrCreateInstance(chunkSet.UserId)
		for _, chunk := range chunkSet.Chunks {
			if err := instance.consumeChunk(userCtx, lbls, chunk); err != nil {
				return err
			}
		}

		seriesReceived++
		receivedChunks.Add(float64(len(chunkSet.Chunks)))
	}

	if seriesReceived == 0 {
		level.Error(logger).Log("msg", "received TransferChunks request with no series", "from_ingester", fromIngesterID)
		return fmt.Errorf("no series")
	} else if fromIngesterID == "" {
		level.Error(logger).Log("msg", "received TransferChunks request with no ID from ingester")
		return fmt.Errorf("no ingester id")
	}

	if err := i.lifecycler.ClaimTokensFor(stream.Context(), fromIngesterID); err != nil {
		return err
	}

	if err := i.lifecycler.ChangeState(stream.Context(), ring.ACTIVE); err != nil {
		return err
	}

	// Close the stream last, as this is what tells the "from" ingester that
	// it's OK to shut down.
	if err := stream.SendAndClose(&logproto.TransferChunksResponse{}); err != nil {
		level.Error(logger).Log("msg", "Error closing TransferChunks stream", "from_ingester", fromIngesterID, "err", err)
		return err
	}
	level.Info(logger).Log("msg", "Successfully transferred chunks", "from_ingester", fromIngesterID, "series_received", seriesReceived)
	return nil
}

// Ring gossiping: check if "from" ingester is in LEAVING state. It should be, but we may not see that yet
// when using gossip ring. If we cannot see ingester is the LEAVING state yet, we don't accept this
// transfer, as claiming tokens would possibly end up with this ingester owning no tokens, due to conflict
// resolution in ring merge function. Hopefully the leaving ingester will retry transfer again.
func (i *Ingester) checkFromIngesterIsInLeavingState(ctx context.Context, fromIngesterID string) error {
	v, err := i.lifecycler.KVStore.Get(ctx, ring.IngesterRingKey)
	if err != nil {
		return errors.Wrap(err, "get ring")
	}
	if v == nil {
		return fmt.Errorf("ring not found when checking state of source ingester")
	}
	r, ok := v.(*ring.Desc)
	if !ok || r == nil {
		return fmt.Errorf("ring not found, got %T", v)
	}

	if r.Ingesters == nil || r.Ingesters[fromIngesterID].State != ring.LEAVING {
		return fmt.Errorf("source ingester is not in a LEAVING state, found state=%v", r.Ingesters[fromIngesterID].State)
	}

	// all fine
	return nil
}

// stopIncomingRequests is called when ingester is stopping
func (i *Ingester) stopIncomingRequests() {
	i.shutdownMtx.Lock()
	defer i.shutdownMtx.Unlock()

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()

	i.readonly = true
}

// TransferOut implements ring.Lifecycler.
func (i *Ingester) TransferOut(ctx context.Context) error {
	if i.cfg.MaxTransferRetries <= 0 {
		return ring.ErrTransferDisabled
	}

	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		MaxRetries: i.cfg.MaxTransferRetries,
	})

	for backoff.Ongoing() {
		err := i.transferOut(ctx)
		if err == nil {
			return nil
		}

		level.Error(dslog.WithContext(ctx, util_log.Logger)).Log("msg", "transfer failed", "err", err)
		backoff.Wait()
	}

	return backoff.Err()
}

func (i *Ingester) transferOut(ctx context.Context) error {
	logger := dslog.WithContext(ctx, util_log.Logger)
	targetIngester, err := i.findTransferTarget(ctx)
	if err != nil {
		return fmt.Errorf("cannot find ingester to transfer chunks to: %v", err)
	}

	level.Info(logger).Log("msg", "sending chunks", "to_ingester", targetIngester.Addr)
	c, err := i.cfg.ingesterClientFactory(i.clientConfig, targetIngester.Addr)
	if err != nil {
		return err
	}
	if c, ok := c.(io.Closer); ok {
		defer lokiutil.LogErrorWithContext(ctx, "closing client", c.Close)
	}
	ic := c.(logproto.IngesterClient)

	ctx = user.InjectOrgID(ctx, "-1")
	stream, err := ic.TransferChunks(ctx)
	if err != nil {
		return errors.Wrap(err, "TransferChunks")
	}

	for instanceID, inst := range i.instances {
		for _, istream := range inst.streams {
			err = func() error {
				istream.chunkMtx.Lock()
				defer istream.chunkMtx.Unlock()
				lbls := []*logproto.LabelPair{}
				for _, lbl := range istream.labels {
					lbls = append(lbls, &logproto.LabelPair{Name: lbl.Name, Value: lbl.Value})
				}

				// We moved to sending one chunk at a time in a stream instead of sending all chunks for a stream
				// as large chunks can create large payloads of >16MB which can hit GRPC limits,
				// typically streams won't have many chunks in memory so sending one at a time
				// shouldn't add too much overhead.
				for _, c := range istream.chunks {
					// Close the chunk first, writing any data in the headblock to a new block.
					err := c.chunk.Close()
					if err != nil {
						return err
					}

					bb, err := c.chunk.Bytes()
					if err != nil {
						return err
					}

					chunks := make([]*logproto.Chunk, 1)
					chunks[0] = &logproto.Chunk{
						Data: bb,
					}

					err = stream.Send(&logproto.TimeSeriesChunk{
						Chunks:         chunks,
						UserId:         instanceID,
						Labels:         lbls,
						FromIngesterId: i.lifecycler.ID,
					})
					if err != nil {
						level.Error(logger).Log("msg", "failed sending stream's chunks to ingester", "to_ingester", targetIngester.Addr, "err", err)
						return err
					}

					sentChunks.Add(float64(len(chunks)))
				}
				return nil
			}()
			if err != nil {
				return err
			}
		}
	}

	_, err = stream.CloseAndRecv()
	if err != nil {
		return errors.Wrap(err, "CloseAndRecv")
	}

	for _, flushQueue := range i.flushQueues {
		flushQueue.DiscardAndClose()
	}
	i.flushQueuesDone.Wait()

	level.Info(logger).Log("msg", "successfully sent chunks", "to_ingester", targetIngester.Addr)
	return nil
}

// findTransferTarget finds an ingester in a PENDING state to use for transferring
// chunks to.
func (i *Ingester) findTransferTarget(ctx context.Context) (*ring.InstanceDesc, error) {
	ringDesc, err := i.lifecycler.KVStore.Get(ctx, ring.IngesterRingKey)
	if err != nil {
		return nil, err
	}

	ingesters := ringDesc.(*ring.Desc).FindIngestersByState(ring.PENDING)
	if len(ingesters) == 0 {
		return nil, fmt.Errorf("no pending ingesters")
	}

	return &ingesters[0], nil
}
