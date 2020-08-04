package ingester

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/thanos/pkg/shipper"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk/encoding"
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
			level.Info(util.Logger).Log("msg", "processing TransferChunks request", "from_ingester", fromIngesterID)

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
		level.Error(util.Logger).Log("msg", "received TransferChunks request with no series", "from_ingester", fromIngesterID)
		retErr = fmt.Errorf("TransferChunks: no series")
		return
	}

	if fromIngesterID == "" {
		level.Error(util.Logger).Log("msg", "received TransferChunks request with no ID from ingester")
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
		userStates := newUserStates(i.limiter, i.cfg, i.metrics)

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
		level.Error(util.Logger).Log("msg", "Error closing TransferChunks stream", "from_ingester", fromIngesterID, "err", err)
		return err
	}
	level.Info(util.Logger).Log("msg", "Successfully transferred chunks", "from_ingester", fromIngesterID, "series_received", seriesReceived)

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

		level.Error(util.Logger).Log("msg", "TransferChunks failed, not in ACTIVE state.", "state", state)

		// Enter PENDING state (only valid from JOINING)
		if i.lifecycler.GetState() == ring.JOINING {
			if err := i.lifecycler.ChangeState(ctx, ring.PENDING); err != nil {
				level.Error(util.Logger).Log("msg", "error rolling back failed TransferChunks", "err", err)
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

// TransferTSDB receives all the file chunks from another ingester, and writes them to tsdb directories
func (i *Ingester) TransferTSDB(stream client.Ingester_TransferTSDBServer) error {
	fromIngesterID := ""

	xfer := func() error {

		// Validate the final directory is empty, if it exists and is empty delete it so a move can succeed
		err := removeEmptyDir(i.cfg.BlocksStorageConfig.TSDB.Dir)
		if err != nil {
			return errors.Wrap(err, "remove existing TSDB directory")
		}

		tmpDir, err := ioutil.TempDir("", "tsdb_xfer")
		if err != nil {
			return errors.Wrap(err, "unable to create temporary directory to store transferred TSDB blocks")
		}
		defer os.RemoveAll(tmpDir)

		bytesXfer := 0
		filesXfer := 0

		files := make(map[string]*os.File)
		defer func() {
			for _, f := range files {
				if err := f.Close(); err != nil {
					level.Warn(util.Logger).Log("msg", "failed to close xfer file", "err", err)
				}
			}
		}()
		for {
			f, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return errors.Wrap(err, "TransferTSDB: Recv")
			}
			if fromIngesterID == "" {
				fromIngesterID = f.FromIngesterId
				level.Info(util.Logger).Log("msg", "processing TransferTSDB request", "from_ingester", fromIngesterID)

				// Before transfer, make sure 'from' ingester is in correct state to call ClaimTokensFor later
				err := i.checkFromIngesterIsInLeavingState(stream.Context(), fromIngesterID)
				if err != nil {
					return errors.Wrap(err, "TransferTSDB: checkFromIngesterIsInLeavingState")
				}
			}
			bytesXfer += len(f.Data)

			createfile := func(f *client.TimeSeriesFile) (*os.File, error) {
				dir := filepath.Join(tmpDir, filepath.Dir(f.Filename))
				if err := os.MkdirAll(dir, 0777); err != nil {
					return nil, errors.Wrap(err, "TransferTSDB: MkdirAll")
				}
				file, err := os.Create(filepath.Join(tmpDir, f.Filename))
				if err != nil {
					return nil, errors.Wrap(err, "TransferTSDB: Create")
				}

				_, err = file.Write(f.Data)
				return file, errors.Wrap(err, "TransferTSDB: Write")
			}

			// Create or get existing open file
			file, ok := files[f.Filename]
			if !ok {
				file, err = createfile(f)
				if err != nil {
					return errors.Wrapf(err, "unable to create file %s to store incoming TSDB block", f)
				}
				filesXfer++
				files[f.Filename] = file
			} else {

				// Write to existing file
				if _, err := file.Write(f.Data); err != nil {
					return errors.Wrap(err, "TransferTSDB: Write")
				}
			}
		}

		if err := i.lifecycler.ClaimTokensFor(stream.Context(), fromIngesterID); err != nil {
			return errors.Wrap(err, "TransferTSDB: ClaimTokensFor")
		}

		i.metrics.receivedBytes.Add(float64(bytesXfer))
		i.metrics.receivedFiles.Add(float64(filesXfer))
		level.Info(util.Logger).Log("msg", "Total xfer", "from_ingester", fromIngesterID, "files", filesXfer, "bytes", bytesXfer)

		// Move the tmpdir to the final location
		err = os.Rename(tmpDir, i.cfg.BlocksStorageConfig.TSDB.Dir)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("unable to move received TSDB blocks from %s to %s", tmpDir, i.cfg.BlocksStorageConfig.TSDB.Dir))
		}

		// At this point all TSDBs have been received, so we can proceed loading TSDBs in memory.
		// This is required because of two reasons:
		// 1. No WAL replay performance penalty once the ingester switches to ACTIVE state
		// 2. If a query is received on user X, for which the TSDB has been transferred, before
		//    the first series is ingested, if we don't open the TSDB the query will return an
		//    empty result (because the TSDB is opened only on first push or transfer)
		userIDs, err := ioutil.ReadDir(i.cfg.BlocksStorageConfig.TSDB.Dir)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("unable to list TSDB users in %s", i.cfg.BlocksStorageConfig.TSDB.Dir))
		}

		for _, user := range userIDs {
			userID := user.Name()

			level.Info(util.Logger).Log("msg", fmt.Sprintf("Loading TSDB for user %s", userID))
			_, err = i.getOrCreateTSDB(userID, true)

			if err != nil {
				level.Error(util.Logger).Log("msg", fmt.Sprintf("Unable to load TSDB for user %s", userID), "err", err)
			} else {
				level.Info(util.Logger).Log("msg", fmt.Sprintf("Loaded TSDB for user %s", userID))
			}
		}

		return nil
	}

	if err := i.transfer(stream.Context(), xfer); err != nil {
		return err
	}

	// Close the stream last, as this is what tells the "from" ingester that
	// it's OK to shut down.
	if err := stream.SendAndClose(&client.TransferTSDBResponse{}); err != nil {
		level.Error(util.Logger).Log("msg", "Error closing TransferTSDB stream", "from_ingester", fromIngesterID, "err", err)
		return err
	}
	level.Info(util.Logger).Log("msg", "Successfully transferred tsdbs", "from_ingester", fromIngesterID)

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
			level.Info(util.Logger).Log("msg", "transfer successfully completed")
			return nil
		}

		level.Warn(util.Logger).Log("msg", "transfer attempt failed", "err", err, "attempt", backoff.NumRetries()+1, "max_retries", i.cfg.MaxTransferRetries)

		backoff.Wait()
	}

	level.Error(util.Logger).Log("msg", "all transfer attempts failed", "err", err)
	return backoff.Err()
}

func (i *Ingester) transferOut(ctx context.Context) error {
	if i.cfg.BlocksStorageEnabled {
		return i.v2TransferOut(ctx)
	}

	userStatesCopy := i.userStates.cp()
	if len(userStatesCopy) == 0 {
		level.Info(util.Logger).Log("msg", "nothing to transfer")
		return nil
	}

	targetIngester, err := i.findTargetIngester(ctx)
	if err != nil {
		return fmt.Errorf("cannot find ingester to transfer chunks to: %w", err)
	}

	level.Info(util.Logger).Log("msg", "sending chunks", "to_ingester", targetIngester.Addr)
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
				Labels:         client.FromLabelsToLabelAdapters(pair.series.metric),
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

	level.Info(util.Logger).Log("msg", "successfully sent chunks", "to_ingester", targetIngester.Addr)
	return nil
}

func (i *Ingester) v2TransferOut(ctx context.Context) error {
	// Skip TSDB transfer if there are no DBs
	i.userStatesMtx.RLock()
	skip := len(i.TSDBState.dbs) == 0
	i.userStatesMtx.RUnlock()

	if skip {
		level.Info(util.Logger).Log("msg", "the ingester has nothing to transfer")
		return nil
	}

	// This transfer function may be called multiple times in case of error,
	// until the max number of retries is reached. For this reason, we run
	// some initialization only once.
	i.TSDBState.transferOnce.Do(func() {
		// In order to transfer TSDB WAL without closing the TSDB itself - which is a
		// pre-requisite to continue serving read requests while transferring - we need
		// to make sure no more series will be written to the TSDB. For this reason, we
		// wait until all in-flight write requests have been completed. No new write
		// requests will be accepted because the "stopped" flag has already been set.
		level.Info(util.Logger).Log("msg", "waiting for in-flight write requests to complete")

		// Do not use the parent context cause we don't want to interrupt while waiting
		// for in-flight requests to complete if the parent context is cancelled, given
		// this logic run only once.
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer waitCancel()

		if err := util.WaitGroup(waitCtx, &i.TSDBState.inflightWriteReqs); err != nil {
			level.Warn(util.Logger).Log("msg", "timeout expired while waiting in-flight write requests to complete, transfer will continue anyway", "err", err)
		}

		// Before beginning transfer, we need to make sure no WAL compaction will occur.
		// If there's an on-going compaction, the DisableCompactions() will wait until
		// completed.
		level.Info(util.Logger).Log("msg", "disabling compaction on all TSDBs")

		i.userStatesMtx.RLock()
		wg := &sync.WaitGroup{}
		wg.Add(len(i.TSDBState.dbs))

		for _, userDB := range i.TSDBState.dbs {
			go func(db *userTSDB) {
				defer wg.Done()
				db.DisableCompactions()
			}(userDB)
		}

		i.userStatesMtx.RUnlock()
		wg.Wait()
	})

	// Look for a joining ingester to transfer blocks and WAL to
	targetIngester, err := i.findTargetIngester(ctx)
	if err != nil {
		return errors.Wrap(err, "cannot find ingester to transfer blocks to")
	}

	level.Info(util.Logger).Log("msg", "begin transferring TSDB blocks and WAL to joining ingester", "to_ingester", targetIngester.Addr)
	c, err := i.cfg.ingesterClientFactory(targetIngester.Addr, i.clientConfig)
	if err != nil {
		return err
	}
	defer c.Close()

	ctx = user.InjectOrgID(ctx, "-1")
	stream, err := c.TransferTSDB(ctx)
	if err != nil {
		return errors.Wrap(err, "TransferTSDB() has failed")
	}

	// Grab a list of all blocks that need to be shipped
	blocks, err := unshippedBlocks(i.cfg.BlocksStorageConfig.TSDB.Dir)
	if err != nil {
		return err
	}

	for user, blockIDs := range blocks {
		// Transfer the users TSDB
		// TODO(thor) transferring users can be done concurrently
		i.transferUser(ctx, stream, i.cfg.BlocksStorageConfig.TSDB.Dir, i.lifecycler.ID, user, blockIDs)
	}

	_, err = stream.CloseAndRecv()
	if err != nil {
		return errors.Wrap(err, "CloseAndRecv")
	}

	// The transfer out has been successfully completed. Now we should close
	// all open TSDBs: the Close() will wait until all on-going read operations
	// will be completed.
	i.closeAllTSDB()

	return nil
}

// findTargetIngester finds an ingester in PENDING state.
func (i *Ingester) findTargetIngester(ctx context.Context) (*ring.IngesterDesc, error) {
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

// unshippedBlocks returns a ulid list of blocks that haven't been shipped
func unshippedBlocks(dir string) (map[string][]string, error) {
	userIDs, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list the directory containing TSDB blocks")
	}

	blocks := make(map[string][]string, len(userIDs))
	for _, user := range userIDs {
		userID := user.Name()
		userDir := filepath.Join(dir, userID)

		// Ensure the user dir is actually a directory. There may be spurious files
		// in the storage, especially when using Minio in the local development environment.
		if stat, err := os.Stat(userDir); err == nil && !stat.IsDir() {
			level.Warn(util.Logger).Log("msg", "skipping entry while transferring TSDB blocks because not a directory", "path", userDir)
			continue
		}

		// Seed the map with the userID to ensure we transfer the WAL, even if all blocks are shipped.
		blocks[userID] = []string{}

		blockIDs, err := ioutil.ReadDir(userDir)
		if err != nil {
			return nil, err
		}

		m, err := shipper.ReadMetaFile(userDir)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}

			// If the meta file doesn't exit, it means the first sync for this
			// user didn't occur yet, so we're going to consider all blocks unshipped.
			m = &shipper.Meta{}
		}

		shipped := make(map[string]bool)
		for _, u := range m.Uploaded {
			shipped[u.String()] = true
		}

		for _, blockID := range blockIDs {
			_, err := ulid.Parse(blockID.Name())
			if err != nil {
				continue
			}

			if _, ok := shipped[blockID.Name()]; !ok {
				blocks[userID] = append(blocks[userID], blockID.Name())
			}
		}
	}

	return blocks, nil
}

func (i *Ingester) transferUser(ctx context.Context, stream client.Ingester_TransferTSDBClient, dir, ingesterID, userID string, blocks []string) {
	level.Info(util.Logger).Log("msg", "transferring user blocks", "user", userID)
	// Transfer all blocks
	for _, blk := range blocks {
		err := filepath.Walk(filepath.Join(dir, userID, blk), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			if info.IsDir() {
				return nil
			}

			b, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}

			p, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}

			if err := batchSend(1024*1024, b, stream, &client.TimeSeriesFile{
				FromIngesterId: ingesterID,
				UserId:         userID,
				Filename:       p,
			}, i.metrics.sentBytes); err != nil {
				return err
			}

			i.metrics.sentFiles.Add(1)
			return nil
		})
		if err != nil {
			level.Warn(util.Logger).Log("msg", "failed to transfer all user blocks", "err", err)
		}
	}

	// Transfer WAL
	level.Info(util.Logger).Log("msg", "transferring user WAL", "user", userID)
	err := filepath.Walk(filepath.Join(dir, userID, "wal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		b, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		p, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if err := batchSend(1024*1024, b, stream, &client.TimeSeriesFile{
			FromIngesterId: ingesterID,
			UserId:         userID,
			Filename:       p,
		}, i.metrics.sentBytes); err != nil {
			return err
		}

		i.metrics.sentFiles.Add(1)
		return nil
	})

	if err != nil {
		level.Warn(util.Logger).Log("msg", "failed to transfer user WAL", "err", err)
	}

	level.Info(util.Logger).Log("msg", "user blocks and WAL transfer completed", "user", userID)
}

func batchSend(batch int, b []byte, stream client.Ingester_TransferTSDBClient, tsfile *client.TimeSeriesFile, sentBytes prometheus.Counter) error {
	// Split file into smaller blocks for xfer
	i := 0
	for ; i+batch < len(b); i += batch {
		tsfile.Data = b[i : i+batch]
		err := client.SendTimeSeriesFile(stream, tsfile)
		if err != nil {
			return err
		}
		sentBytes.Add(float64(len(tsfile.Data)))
	}

	// Send final data
	if i < len(b) {
		tsfile.Data = b[i:]
		err := client.SendTimeSeriesFile(stream, tsfile)
		if err != nil {
			return err
		}
		sentBytes.Add(float64(len(tsfile.Data)))
	}

	return nil
}

func removeEmptyDir(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return os.Remove(dir)
}
