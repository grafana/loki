package base

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/querier/batch"
	"github.com/grafana/loki/pkg/querier/chunkstore"
	"github.com/grafana/loki/pkg/storage/chunk"
)

func getTarDataFromEnv(t testing.TB) (query string, from, through time.Time, step time.Duration, store chunkstore.ChunkStore) {
	var (
		err            error
		chunksFilename = os.Getenv("CHUNKS")
		userID         = os.Getenv("USERID")
	)
	query = os.Getenv("QUERY")

	if len(chunksFilename) == 0 || len(userID) == 0 || len(query) == 0 {
		return query, from, through, step, store
	}

	chunks, err := loadChunks(userID, chunksFilename)
	require.NoError(t, err)

	from, err = parseTime(os.Getenv("FROM"))
	require.NoError(t, err)

	through, err = parseTime(os.Getenv("THROUGH"))
	require.NoError(t, err)

	step, err = parseDuration(os.Getenv("STEP"))
	require.NoError(t, err)

	return query, from, through, step, &mockChunkStore{chunks}
}

func runRangeQuery(t testing.TB, query string, from, through time.Time, step time.Duration, store chunkstore.ChunkStore) {
	dir := t.TempDir()
	queryTracker := promql.NewActiveQueryTracker(dir, 1, log.NewNopLogger())

	if len(query) == 0 || store == nil {
		return
	}
	queryable := newChunkStoreQueryable(store, batch.NewChunkMergeIterator)
	engine := promql.NewEngine(promql.EngineOpts{
		Logger:             log.NewNopLogger(),
		ActiveQueryTracker: queryTracker,
		MaxSamples:         math.MaxInt32,
		Timeout:            10 * time.Minute,
	})
	rangeQuery, err := engine.NewRangeQuery(queryable, query, from, through, step)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "0")
	r := rangeQuery.Exec(ctx)
	_, err = r.Matrix()
	require.NoError(t, err)
}

func TestChunkTar(t *testing.T) {
	query, from, through, step, store := getTarDataFromEnv(t)
	runRangeQuery(t, query, from, through, step, store)
}

func parseTime(s string) (time.Time, error) {
	t, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return time.Time{}, err
	}
	secs, ns := math.Modf(t)
	tm := time.Unix(int64(secs), int64(ns*float64(time.Second)))
	return tm, nil
}

func parseDuration(s string) (time.Duration, error) {
	t, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return time.Duration(0), err
	}
	return time.Duration(t * float64(time.Second)), nil
}

func loadChunks(userID, filename string) ([]chunk.Chunk, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gzipReader, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}

	var chunks []chunk.Chunk
	tarReader := tar.NewReader(gzipReader)
	ctx := chunk.NewDecodeContext()
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.Wrap(err, "here 1")
		}

		c, err := chunk.ParseExternalKey(userID, hdr.Name)
		if err != nil {
			return nil, errors.Wrap(err, "here 2")
		}

		var buf = make([]byte, int(hdr.Size))
		if _, err := io.ReadFull(tarReader, buf); err != nil {
			return nil, errors.Wrap(err, "here 3")
		}

		if err := c.Decode(ctx, buf); err != nil {
			return nil, errors.Wrap(err, "here 4")
		}

		chunks = append(chunks, c)
	}

	return chunks, nil
}
