package ingester

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context"
)

var (
	sentChunks = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_sent_chunks",
		Help: "The total number of chunks sent by this ingester whilst leaving.",
	})
	receivedChunks = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loki_ingester_received_chunks",
		Help: "The total number of chunks received by this ingester whilst joining.",
	})
)

// TransferChunks receives all chunks from another ingester. The Ingester
// must be in PENDING state or else the call will fail.
func (i *Ingester) TransferChunks(stream logproto.Ingester_TransferChunksServer) error {
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

		level.Error(util.Logger).Log("msg", "TransferChunks failed, not in ACTIVE state.", "state", state)

		// Enter PENDING state (only valid from JOINING)
		if i.lifecycler.GetState() == ring.JOINING {
			if err := i.lifecycler.ChangeState(stream.Context(), ring.PENDING); err != nil {
				level.Error(util.Logger).Log("msg", "error rolling back failed TransferChunks", "err", err)
				os.Exit(1)
			}
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
			level.Info(util.Logger).Log("msg", "processing TransferChunks request", "from_ingester", fromIngesterID)
		}

		userCtx := user.InjectOrgID(stream.Context(), chunkSet.UserId)

		lbls := []client.LabelAdapter{}
		for _, lbl := range chunkSet.Labels {
			lbls = append(lbls, client.LabelAdapter{Name: lbl.Name, Value: lbl.Value})
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
		level.Error(util.Logger).Log("msg", "received TransferChunks request with no series", "from_ingester", fromIngesterID)
		return fmt.Errorf("no series")
	} else if fromIngesterID == "" {
		level.Error(util.Logger).Log("msg", "received TransferChunks request with no ID from ingester")
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
		level.Error(util.Logger).Log("msg", "Error closing TransferChunks stream", "from_ingester", fromIngesterID, "err", err)
		return err
	}
	level.Info(util.Logger).Log("msg", "Successfully transferred chunks", "from_ingester", fromIngesterID, "series_received", seriesReceived)
	return nil
}

// StopIncomingRequests implements ring.Lifecycler.
func (i *Ingester) StopIncomingRequests() {
	i.shutdownMtx.Lock()
	defer i.shutdownMtx.Unlock()

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()

	i.readonly = true
}

// TransferOut implements ring.Lifecycler.
func (i *Ingester) TransferOut(ctx context.Context) error {
	backoff := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		MaxRetries: i.cfg.MaxTransferRetries,
	})

	for backoff.Ongoing() {
		err := i.transferOut(ctx)
		if err == nil {
			return nil
		}

		level.Error(util.Logger).Log("msg", "transfer failed", "err", err)
		backoff.Wait()
	}

	return backoff.Err()
}

func (i *Ingester) transferOut(ctx context.Context) error {
	targetIngester, err := i.findTransferTarget(ctx)
	if err != nil {
		return fmt.Errorf("cannot find ingester to transfer chunks to: %v", err)
	}

	level.Info(util.Logger).Log("msg", "sending chunks", "to_ingester", targetIngester.Addr)
	c, err := i.cfg.ingesterClientFactory(i.clientConfig, targetIngester.Addr)
	if err != nil {
		return err
	}
	if c, ok := c.(io.Closer); ok {
		defer helpers.LogError("closing client", c.Close)
	}
	ic := c.(logproto.IngesterClient)

	ctx = user.InjectOrgID(ctx, "-1")
	stream, err := ic.TransferChunks(ctx)
	if err != nil {
		return errors.Wrap(err, "TransferChunks")
	}

	for instanceID, inst := range i.instances {
		for _, istream := range inst.streams {
			chunks := make([]*logproto.Chunk, 0, len(istream.chunks))

			for _, c := range istream.chunks {
				bb, err := c.chunk.Bytes()
				if err != nil {
					return err
				}

				chunks = append(chunks, &logproto.Chunk{
					Data: bb,
				})
			}

			lbls := []*logproto.LabelPair{}
			for _, lbl := range istream.labels {
				lbls = append(lbls, &logproto.LabelPair{Name: lbl.Name, Value: lbl.Value})
			}

			err := stream.Send(&logproto.TimeSeriesChunk{
				Chunks:         chunks,
				UserId:         instanceID,
				Labels:         lbls,
				FromIngesterId: i.lifecycler.ID,
			})
			if err != nil {
				level.Error(util.Logger).Log("msg", "failed sending stream's chunks to ingester", "to_ingester", targetIngester.Addr, "err", err)
				return err
			}

			sentChunks.Add(float64(len(chunks)))
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

	level.Info(util.Logger).Log("msg", "successfully sent chunks", "to_ingester", targetIngester.Addr)
	return nil
}

// findTransferTarget finds an ingester in a PENDING state to use for transferring
// chunks to.
func (i *Ingester) findTransferTarget(ctx context.Context) (*ring.IngesterDesc, error) {
	ringDesc, err := i.lifecycler.KVStore.Get(ctx, ring.ConsulKey)
	if err != nil {
		return nil, err
	}

	ingesters := ringDesc.(*ring.Desc).FindIngestersByState(ring.PENDING)
	if len(ingesters) == 0 {
		return nil, fmt.Errorf("no pending ingesters")
	}

	return &ingesters[0], nil
}
