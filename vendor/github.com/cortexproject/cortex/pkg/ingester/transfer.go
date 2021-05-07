package ingester

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
)

var (
	errTransferNoPendingIngesters = errors.New("no pending ingesters")
)

// returns source ingesterID, number of received series, added chunks and error
func (i *Ingester) fillUserStatesFromStream(userStates *userStates, stream client.Ingester_TransferChunksServer) (fromIngesterID string, seriesReceived int, retErr error) {
	chunksAdded := 0.0

	defer func() {
		if retErr != nil {
			// Ensure the in memory chunks are updated to reflect the number of dropped chunks from the transfer
			i.metrics.memoryChunks.Sub(chunksAdded)

			// If an error occurs during the transfer and the user state is to be discarded,
			// ensure the metrics it exports reflect this.
			userStates.teardown()
		}
	}()

	for {
		wireSeries, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			retErr = errors.Wrap(err, "TransferChunks: Recv")
			return
		}

		// We can't send "extra" fields with a streaming call, so we repeat
		// wireSeries.FromIngesterId and assume it is the same every time
		// round this loop.
		if fromIngesterID == "" {
			fromIngesterID = wireSeries.FromIngesterId
			level.Info(i.logger).Log("msg", "processing TransferChunks request", "from_ingester", fromIngesterID)

			// Before transfer, make sure 'from' ingester is in correct state to call ClaimTokensFor later
			err := i.checkFromIngesterIsInLeavingState(stream.Context(), fromIngesterID)
			if err != nil {
				retErr = errors.Wrap(err, "TransferChunks: checkFromIngesterIsInLeavingState")
				return
			}
		}
		descs, err := fromWireChunks(wireSeries.Chunks)
		if err != nil {
			retErr = errors.Wrap(err, "TransferChunks: fromWireChunks")
			return
		}

		state, fp, series, err := userStates.getOrCreateSeries(stream.Context(), wireSeries.UserId, wireSeries.Labels, nil)
		if err != nil {
			retErr = errors.Wrapf(err, "TransferChunks: getOrCreateSeries: user %s series %s", wireSeries.UserId, wireSeries.Labels)
			return
		}
		prevNumChunks := len(series.chunkDescs)

		err = series.setChunks(descs)
		state.fpLocker.Unlock(fp) // acquired in getOrCreateSeries
		if err != nil {
			retErr = errors.Wrapf(err, "TransferChunks: setChunks: user %s series %s", wireSeries.UserId, wireSeries.Labels)
			return
		}

		seriesReceived++
		chunksDelta := float64(len(series.chunkDescs) - prevNumChunks)
		chunksAdded += chunksDelta
		i.metrics.memoryChunks.Add(chunksDelta)
		i.metrics.receivedChunks.Add(float64(len(descs)))
	}

	if seriesReceived == 0 {
		level.Error(i.logger).Log("msg", "received TransferChunks request with no series", "from_ingester", fromIngesterID)
		retErr = fmt.Errorf("TransferChunks: no series")
		return
	}

	if fromIngesterID == "" {
		level.Error(i.logger).Log("msg", "received TransferChunks request with no ID from ingester")
		retErr = fmt.Errorf("no ingester id")
		return
	}

	if err := i.lifecycler.ClaimTokensFor(stream.Context(), fromIngesterID); err != nil {
		retErr = errors.Wrap(err, "TransferChunks: ClaimTokensFor")
		return
	}

	return
}

// TransferChunks receives all the chunks from another ingester.
func (i *Ingester) TransferChunks(stream client.Ingester_TransferChunksServer) error {
	fromIngesterID := ""
	seriesReceived := 0

	xfer := func() error {
		userStates := newUserStates(i.limiter, i.cfg, i.metrics, i.logger)

		var err error
		fromIngesterID, seriesReceived, err = i.fillUserStatesFromStream(userStates, stream)

		if err != nil {
			return err
		}

		i.userStatesMtx.Lock()
		defer i.userStatesMtx.Unlock()

		i.userStates = userStates

		return nil
	}

	if err := i.transfer(stream.Context(), xfer); err != nil {
		return err
	}

	// Close the stream last, as this is what tells the "from" ingester that
	// it's OK to shut down.
	if err := stream.SendAndClose(&client.TransferChunksResponse{}); err != nil {
		level.Error(i.logger).Log("msg", "Error closing TransferChunks stream", "from_ingester", fromIngesterID, "err", err)
		return err
	}
	level.Info(i.logger).Log("msg", "Successfully transferred chunks", "from_ingester", fromIngesterID, "series_received", seriesReceived)

	return nil
}

// Ring gossiping: check if "from" ingester is in LEAVING state. It should be, but we may not see that yet
// when using gossip ring. If we cannot see ingester is the LEAVING state yet, we don't accept this
// transfer, as claiming tokens would possibly end up with this ingester owning no tokens, due to conflict
// resolution in ring merge function. Hopefully the leaving ingester will retry transfer again.
func (i *Ingester) checkFromIngesterIsInLeavingState(ctx context.Context, fromIngesterID string) error {
	v, err := i.lifecycler.KVStore.Get(ctx, i.lifecycler.RingKey)
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

func (i *Ingester) transfer(ctx context.Context, xfer func() error) error {
	// Enter JOINING state (only valid from PENDING)
	if err := i.lifecycler.ChangeState(ctx, ring.JOINING); err != nil {
		return err
	}

	// The ingesters state effectively works as a giant mutex around this whole
	// method, and as such we have to ensure we unlock the mutex.
	defer func() {
		state := i.lifecycler.GetState()
		if i.lifecycler.GetState() == ring.ACTIVE {
			return
		}

		level.Error(i.logger).Log("msg", "TransferChunks failed, not in ACTIVE state.", "state", state)

		// Enter PENDING state (only valid from JOINING)
		if i.lifecycler.GetState() == ring.JOINING {
			if err := i.lifecycler.ChangeState(ctx, ring.PENDING); err != nil {
				level.Error(i.logger).Log("msg", "error rolling back failed TransferChunks", "err", err)
				os.Exit(1)
			}
		}
	}()

	if err := xfer(); err != nil {
		return err
	}

	if err := i.lifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrap(err, "Transfer: ChangeState")
	}

	return nil
}

// The passed wireChunks slice is for re-use.
func toWireChunks(descs []*desc, wireChunks []client.Chunk) ([]client.Chunk, error) {
	if cap(wireChunks) < len(descs) {
		wireChunks = make([]client.Chunk, len(descs))
	} else {
		wireChunks = wireChunks[:len(descs)]
	}
	for i, d := range descs {
		wireChunk := client.Chunk{
			StartTimestampMs: int64(d.FirstTime),
			EndTimestampMs:   int64(d.LastTime),
			Encoding:         int32(d.C.Encoding()),
		}

		slice := wireChunks[i].Data[:0] // try to re-use the memory from last time
		if cap(slice) < d.C.Size() {
			slice = make([]byte, 0, d.C.Size())
		}
		buf := bytes.NewBuffer(slice)

		if err := d.C.Marshal(buf); err != nil {
			return nil, err
		}

		wireChunk.Data = buf.Bytes()
		wireChunks[i] = wireChunk
	}
	return wireChunks, nil
}

func fromWireChunks(wireChunks []client.Chunk) ([]*desc, error) {
	descs := make([]*desc, 0, len(wireChunks))
	for _, c := range wireChunks {
		desc := &desc{
			FirstTime:  model.Time(c.StartTimestampMs),
			LastTime:   model.Time(c.EndTimestampMs),
			LastUpdate: model.Now(),
		}

		var err error
		desc.C, err = encoding.NewForEncoding(encoding.Encoding(byte(c.Encoding)))
		if err != nil {
			return nil, err
		}

		if err := desc.C.UnmarshalFromBuf(c.Data); err != nil {
			return nil, err
		}

		descs = append(descs, desc)
	}
	return descs, nil
}

// TransferOut finds an ingester in PENDING state and transfers our chunks to it.
// Called as part of the ingester shutdown process.
func (i *Ingester) TransferOut(ctx context.Context) error {
	// The blocks storage doesn't support blocks transferring.
	if i.cfg.BlocksStorageEnabled {
		level.Info(i.logger).Log("msg", "transfer between a LEAVING ingester and a PENDING one is not supported for the blocks storage")
		return ring.ErrTransferDisabled
	}

	if i.cfg.MaxTransferRetries <= 0 {
		return ring.ErrTransferDisabled
	}
	backoff := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 5 * time.Second,
		MaxRetries: i.cfg.MaxTransferRetries,
	})

	// Keep track of the last error so that we can log it with the highest level
	// once all retries have completed
	var err error

	for backoff.Ongoing() {
		err = i.transferOut(ctx)
		if err == nil {
			level.Info(i.logger).Log("msg", "transfer successfully completed")
			return nil
		}

		level.Warn(i.logger).Log("msg", "transfer attempt failed", "err", err, "attempt", backoff.NumRetries()+1, "max_retries", i.cfg.MaxTransferRetries)

		backoff.Wait()
	}

	level.Error(i.logger).Log("msg", "all transfer attempts failed", "err", err)
	return backoff.Err()
}

func (i *Ingester) transferOut(ctx context.Context) error {
	userStatesCopy := i.userStates.cp()
	if len(userStatesCopy) == 0 {
		level.Info(i.logger).Log("msg", "nothing to transfer")
		return nil
	}

	targetIngester, err := i.findTargetIngester(ctx)
	if err != nil {
		return fmt.Errorf("cannot find ingester to transfer chunks to: %w", err)
	}

	level.Info(i.logger).Log("msg", "sending chunks", "to_ingester", targetIngester.Addr)
	c, err := i.cfg.ingesterClientFactory(targetIngester.Addr, i.clientConfig)
	if err != nil {
		return err
	}
	defer c.Close()

	ctx = user.InjectOrgID(ctx, "-1")
	stream, err := c.TransferChunks(ctx)
	if err != nil {
		return errors.Wrap(err, "TransferChunks")
	}

	var chunks []client.Chunk
	for userID, state := range userStatesCopy {
		for pair := range state.fpToSeries.iter() {
			state.fpLocker.Lock(pair.fp)

			if len(pair.series.chunkDescs) == 0 { // Nothing to send?
				state.fpLocker.Unlock(pair.fp)
				continue
			}

			chunks, err = toWireChunks(pair.series.chunkDescs, chunks)
			if err != nil {
				state.fpLocker.Unlock(pair.fp)
				return errors.Wrap(err, "toWireChunks")
			}

			err = client.SendTimeSeriesChunk(stream, &client.TimeSeriesChunk{
				FromIngesterId: i.lifecycler.ID,
				UserId:         userID,
				Labels:         cortexpb.FromLabelsToLabelAdapters(pair.series.metric),
				Chunks:         chunks,
			})
			state.fpLocker.Unlock(pair.fp)
			if err != nil {
				return errors.Wrap(err, "Send")
			}

			i.metrics.sentChunks.Add(float64(len(chunks)))
		}
	}

	_, err = stream.CloseAndRecv()
	if err != nil {
		return errors.Wrap(err, "CloseAndRecv")
	}

	// Close & empty all the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.DiscardAndClose()
	}
	i.flushQueuesDone.Wait()

	level.Info(i.logger).Log("msg", "successfully sent chunks", "to_ingester", targetIngester.Addr)
	return nil
}

// findTargetIngester finds an ingester in PENDING state.
func (i *Ingester) findTargetIngester(ctx context.Context) (*ring.InstanceDesc, error) {
	ringDesc, err := i.lifecycler.KVStore.Get(ctx, i.lifecycler.RingKey)
	if err != nil {
		return nil, err
	} else if ringDesc == nil {
		return nil, errTransferNoPendingIngesters
	}

	ingesters := ringDesc.(*ring.Desc).FindIngestersByState(ring.PENDING)
	if len(ingesters) <= 0 {
		return nil, errTransferNoPendingIngesters
	}

	return &ingesters[0], nil
}
