package metastore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/tools"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
	"github.com/thanos-io/objstore/providers/gcs"
	"github.com/thanos-io/objstore/providers/s3"
	"golang.org/x/sync/errgroup"
)

func opsBucket(t *testing.T) objstore.Bucket {
	bucketName := "ops-eu-south-0-loki-ops-002-data"
	cloudBucket, err := s3.NewBucketWithConfig(log.NewNopLogger(), s3.Config{
		Bucket:   bucketName,
		Endpoint: "s3.eu-south-2.amazonaws.com",
		Region:   "eu-south-2",
	}, "metastore-test", nil)
	require.NoError(t, err)

	return objstore.NewPrefixedBucket(cloudBucket, "dataobj")
}

func devBucket(t *testing.T) objstore.Bucket {
	bucketName := "dev-us-central-0-loki-dev-005-data"
	cloudBucket, err := gcs.NewBucketWithConfig(context.Background(), log.NewNopLogger(), gcs.Config{
		Bucket: bucketName,
	}, "config-gen", nil)
	require.NoError(t, err)
	return objstore.NewPrefixedBucket(cloudBucket, "dataobj")
}

func opsCacheBucket(t *testing.T) objstore.Bucket {
	dir := "/Users/benclive/dev/loki/dataobjs-ops/cache"
	cacheBucket, err := filesystem.NewBucket(dir)
	require.NoError(t, err)
	return cacheBucket
}

func TestReadMetastore(t *testing.T) {
	opsBucket := opsBucket(t)
	cacheBucket := opsCacheBucket(t)
	tenantID := "29"

	startTime := time.Date(2025, 2, 11, 15, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, 2, 11, 16, 0, 0, 0, time.UTC)

	var query dataobj.StreamsPredicate
	query = dataobj.AndPredicate[dataobj.StreamsPredicate]{
		Left: dataobj.TimeRangePredicate[dataobj.StreamsPredicate]{
			StartTime:    startTime,
			EndTime:      endTime,
			IncludeStart: true,
			IncludeEnd:   true,
		},
		Right: dataobj.LabelFilterPredicate{
			Name: "container",
			Keep: func(name, value string) bool {
				return value == "querier"
			},
		},
	}

	paths, err := ListDataObjects(context.Background(), opsBucket, tenantID, startTime, endTime)
	require.NoError(t, err)
	require.Greaterf(t, len(paths), 0, "no dataobj paths found")

	objs := fetchStreamSections(t, paths, opsBucket, cacheBucket)

	objectsSkippedTooOld := atomic.Int32{}
	objectWithMatchingStreams := atomic.Int32{}
	totalMatchingStreams := atomic.Int32{}

	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(100)

	// Read metastore
	startLookup := time.Now()
	for _, path := range paths {
		g.Go(func() error {
			matchingObject := false
			fmt.Println(path)
			dat, _ := objs.Load(path)
			datBytes := dat.([]byte)
			object := dataobj.FromReaderAt(bytes.NewReader(datBytes), int64(len(datBytes)))

			attr, err := opsBucket.Attributes(context.Background(), path)
			require.NoError(t, err)
			if attr.LastModified.Sub(endTime) > time.Hour*1 {
				objectsSkippedTooOld.Add(1)
				return nil
			}

			forEachStream(t, object, query, func(stream dataobj.Stream) {
				totalMatchingStreams.Add(1)
				matchingObject = true
			})
			if matchingObject {
				objectWithMatchingStreams.Add(1)
			}
			return nil
		})
	}
	g.Wait()
	fmt.Printf("Total objects for time range: %d\n", len(paths))
	fmt.Printf("Objects skipped too new: %d\n", objectsSkippedTooOld.Load())
	fmt.Printf("Objects searched: %d\n", len(paths)-int(objectsSkippedTooOld.Load()))
	fmt.Printf("Objects with matching streams: %d\n", objectWithMatchingStreams.Load())
	fmt.Printf("Total matching streams: %d\n", totalMatchingStreams.Load())
	fmt.Printf("Time taken: %s\n", time.Since(startLookup))
}

func TestRebuildingDataobj(t *testing.T) {
	ctx := context.Background()
	opsBucket := opsBucket(t)
	cacheBucket := opsCacheBucket(t)
	tenantID := "29"

	startTime := time.Date(2025, 2, 11, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, 2, 11, 11, 59, 59, 0, time.UTC)

	paths, err := ListDataObjects(ctx, opsBucket, tenantID, startTime, endTime)
	require.NoError(t, err)
	fmt.Println("Paths: ", len(paths), paths)
	require.Greaterf(t, len(paths), 0, "no dataobj paths found")

	objs := fetchStreamSections(t, paths, opsBucket, cacheBucket)

	mtx := sync.Mutex{}
	metastore, err := dataobj.NewBuilder(metastoreBuilderCfg)
	require.NoError(t, err)

	start := time.Now()
	recordsAdded := 0
	emptyCount := 0

	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(30)
	for _, s := range paths {
		g.Go(func() error {
			data, _ := objs.Load(s)
			object := dataobj.FromReaderAt(bytes.NewReader(data.([]byte)), int64(len(data.([]byte))))

			//drawHistogramOfStreamCounts(t, object, s)
			mtx.Lock()
			addStreamsToMetastore(t, object, metastore)
			mtx.Unlock()
			return nil
		})
	}
	g.Wait()

	fmt.Printf("paths :%d, emptyCount: %d\n", len(paths), emptyCount)

	output := bytes.NewBuffer(make([]byte, 0, 100<<20))

	fmt.Printf("Metastore records added: %d; time taken: %s\n", recordsAdded, time.Since(start))
	_, err = metastore.Flush(output)
	require.NoError(t, err)
	/* 			fmt.Printf("Metastore flush: Streams: %d; obj size: %s\n", stats.StreamCount, humanize.Bytes(uint64(output.Len())))

	   			os.WriteFile("metastore-ops.store", output.Bytes(), 0644) */
	tools.Inspect(bytes.NewReader(output.Bytes()), int64(output.Len()))
}

func fetchStreamSections(t *testing.T, paths []string, sourceBucket objstore.Bucket, cacheBucket objstore.Bucket) *sync.Map {
	fg, ctx := errgroup.WithContext(context.Background())
	fg.SetLimit(200)

	objs := &sync.Map{}
	for _, path := range paths {
		fg.Go(func() error {
			if exists, err := cacheBucket.Exists(ctx, path); exists && err == nil {
				reader, err := cacheBucket.Get(ctx, path)
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				objs.Store(path, data)
				return nil
			}
			attrs, err := sourceBucket.Attributes(ctx, path)
			require.NoError(t, err)

			// Read first 1mb to ensure we have the streams section
			reader, err := sourceBucket.GetRange(ctx, path, 0, 1<<20)
			require.NoError(t, err)

			data, err := io.ReadAll(reader)
			require.NoError(t, err)

			// Read last 256kb to ensure we have the tailer
			reader, err = sourceBucket.GetRange(ctx, path, attrs.Size-1<<18, 1<<18)
			require.NoError(t, err)
			data2, err := io.ReadAll(reader)
			require.NoError(t, err)

			data = append(data, data2...)

			objs.Store(path, data)
			cacheBucket.Upload(ctx, path, bytes.NewReader(data))
			return nil
		})
	}
	fg.Wait()
	return objs
}

func forEachStream(t *testing.T, object *dataobj.Object, predicate dataobj.StreamsPredicate, f func(dataobj.Stream)) {
	ctx := context.Background()
	md, err := object.Metadata(ctx)
	require.NoError(t, err)

	streams := make([]dataobj.Stream, 1000)
	for i := 0; i < md.StreamsSections; i++ {
		reader := dataobj.NewStreamsReader(object, i)
		if predicate != nil {
			reader.SetPredicate(predicate)
		}
		for {
			num, err := reader.Read(ctx, streams)
			if err != io.EOF {
				require.NoError(t, err)
			}
			if num == 0 && err == io.EOF {
				break
			}
			for _, stream := range streams[:num] {
				f(stream)
			}
		}
	}

}

func drawHistogramOfStreamCounts(t *testing.T, object *dataobj.Object, path string) error {
	var minTs, maxTs time.Time
	forEachStream(t, object, nil, func(stream dataobj.Stream) {
		if minTs.IsZero() || stream.MinTime.Before(minTs) {
			minTs = stream.MinTime
		}
		if maxTs.IsZero() || stream.MaxTime.After(maxTs) {
			maxTs = stream.MaxTime
		}
	})

	if maxTs.Sub(minTs) < time.Hour*3 {
		return nil
	}

	ts := make([]int, int(maxTs.Sub(minTs).Hours())+1)
	lbs := make([]string, int(maxTs.Sub(minTs).Hours())+1)
	forEachStream(t, object, nil, func(stream dataobj.Stream) {
		for hour := stream.MinTime; hour.Before(stream.MaxTime); hour = hour.Add(time.Hour) {
			idx := int(hour.Sub(minTs).Hours())
			ts[idx]++
			if ts[idx] == 1 {
				lbs[idx] = stream.Labels.String()
			}
		}
	})

	maxCnt := 0
	for _, cnt := range ts {
		maxCnt = max(cnt, maxCnt)
	}

	fmt.Println()
	fmt.Printf("%s\n", path)
	maxWidth := 180
	for hour := minTs.Truncate(time.Hour); hour.Before(maxTs); hour = hour.Add(time.Hour) {
		cnt := int(hour.Sub(minTs).Hours())
		stars := int(ts[cnt]/maxCnt) * maxWidth
		if ts[cnt] > 0 && stars == 0 {
			stars = 1
		}
		lbls := ""
		if ts[cnt] == 1 {
			lbls = lbs[cnt]
		}
		fmt.Printf("%s %s %d %s\n", hour.Format(time.RFC3339), strings.Repeat("*", stars), ts[cnt], lbls)
	}

	return nil
}

func addStreamsToMetastore(t *testing.T, object *dataobj.Object, metastore *dataobj.Builder) {
	forEachStream(t, object, nil, func(stream dataobj.Stream) {
		err := metastore.AppendLabels(logproto.Stream{
			Entries: []logproto.Entry{
				{
					Timestamp: stream.MinTime,
					Line:      "", //dir + "/" + s,
				},
			},
		}, stream.Labels)
		require.NoError(t, err)
	})
}

func TestUpdatingMetastore(t *testing.T) {
	ctx := context.Background()
	logger := log.NewNopLogger()
	/*dir := "/Users/benclive/dev/loki/dataobjs-ops/cache"
		bucketName := "dev-us-central-0-loki-dev-005-data"
	  	cloudBucket, err := gcs.NewBucketWithConfig(ctx, logger, gcs.Config{
	  		Bucket: gcsBucketName,
	  	}, "config-gen", nil) */

	dir := "/Users/benclive/dev/loki/dataobjs-ops/cache"
	bucketName := "ops-eu-south-0-loki-ops-002-data"
	// nocommit
	cloudBucket, err := s3.NewBucketWithConfig(logger, s3.Config{
		Bucket:   bucketName,
		Endpoint: "s3.eu-south-2.amazonaws.com",
		Region:   "eu-south-2",
	}, "metastore-test", nil)
	require.NoError(t, err)

	bucket := objstore.NewPrefixedBucket(cloudBucket, "dataobj")
	cacheBucket, err := filesystem.NewBucket(dir)
	require.NoError(t, err)
	tenantID := "29"

	startTime := time.Date(2025, 2, 11, 15, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, 2, 11, 16, 0, 0, 0, time.UTC)

	paths, err := ListDataObjects(ctx, bucket, tenantID, startTime, endTime)
	require.NoError(t, err)
	fmt.Println("Paths: ", len(paths), paths)
	require.Greaterf(t, len(paths), 0, "no dataobj paths found")

	fg, _ := errgroup.WithContext(ctx)
	fg.SetLimit(200)

	objs := sync.Map{}
	for _, path := range paths {
		fg.Go(func() error {
			if exists, err := cacheBucket.Exists(ctx, path); exists && err == nil {
				reader, err := cacheBucket.Get(ctx, path)
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				objs.Store(path, data)
				return nil
			}
			attrs, err := bucket.Attributes(ctx, path)
			require.NoError(t, err)

			// Read first 1mb to ensure we have the streams section
			reader, err := bucket.GetRange(ctx, path, 0, 1<<20)
			require.NoError(t, err)

			data, err := io.ReadAll(reader)
			require.NoError(t, err)

			// Read last 256kb to ensure we have the tailer
			reader, err = bucket.GetRange(ctx, path, attrs.Size-1<<18, 1<<18)
			require.NoError(t, err)
			data2, err := io.ReadAll(reader)
			require.NoError(t, err)

			data = append(data, data2...)

			objs.Store(path, data)
			cacheBucket.Upload(ctx, path, bytes.NewReader(data))
			return nil
		})
	}

	fg.Wait()

	recordsAdded := 0
	streams := make([]dataobj.Stream, 1000)

	//mgr := NewManager(cacheBucket, tenantID, log.NewLogfmtLogger(os.Stderr))

	//exists := map[uint64]*dataobj.StreamStats{}

	skipped := 0

	for _, s := range paths {

		/* 		flushStats := dataobj.FlushStats{
			MinTimestamp: startTime,
			MaxTimestamp: endTime,
			StreamStats:  []dataobj.StreamStats{},
		} */
		func() error {
			attrs, err := bucket.Attributes(ctx, s)
			require.NoError(t, err)
			if time.Since(attrs.LastModified) > time.Hour*2 {
				skipped++
				return nil
			}
			objWithStreams := 0
			data, _ := objs.Load(s)
			object := dataobj.FromReaderAt(bytes.NewReader(data.([]byte)), int64(len(data.([]byte))))
			md, err := object.Metadata(ctx)
			require.NoError(t, err)

			for i := 0; i < md.StreamsSections; i++ {
				reader := dataobj.NewStreamsReader(object, i)

				reader.SetPredicate(dataobj.AndPredicate[dataobj.StreamsPredicate]{
					Left: dataobj.TimeRangePredicate[dataobj.StreamsPredicate]{
						StartTime:    startTime,
						EndTime:      endTime,
						IncludeStart: true,
						IncludeEnd:   true,
					},
					Right: dataobj.LabelFilterPredicate{
						Name: "container",
						Keep: func(name, value string) bool {
							return value == "querier"
						},
					},
				})

				for {
					num, err := reader.Read(ctx, streams)
					if err != io.EOF {
						require.NoError(t, err)
					}
					recordsAdded += num
					objWithStreams += num
					if num == 0 && err == io.EOF {
						break
					}

					/* 					for _, stream := range streams[:num] {
						hash := stream.Labels.Hash()
						if _, ok := exists[hash]; !ok {
							flushStats.StreamStats = append(flushStats.StreamStats, dataobj.StreamStats{
								Labels:  stream.Labels,
								MinTime: stream.MinTime,
								MaxTime: stream.MaxTime,
							})
							exists[hash] = &flushStats.StreamStats[len(flushStats.StreamStats)-1]
							continue
						}
						// Update min/max times using time.Before/After comparisons
						if stream.MinTime.Before(exists[hash].MinTime) {
							exists[hash].MinTime = stream.MinTime
						}
						if stream.MaxTime.After(exists[hash].MaxTime) {
							exists[hash].MaxTime = stream.MaxTime
						}
					} */
				}
			}

			return nil
		}()

		//before := time.Now()
		//err := mgr.UpdateMetastore(ctx, s, flushStats)
		//require.NoError(t, err)
		//fmt.Printf("UpdateMetastore time: %s %s\n", time.Since(before), s)
	}
	/*
		// Read metastore
		startLookup := time.Now()
		for path := range Iter("29", startTime, endTime) {
			object := dataobj.FromBucket(cacheBucket, path)

			md, err := object.Metadata(ctx)
			require.NoError(t, err)

			for i := 0; i < md.StreamsSections; i++ {
				reader := dataobj.NewStreamsReader(object, i)

				reader.SetPredicate(dataobj.AndPredicate[dataobj.StreamsPredicate]{
					Left: dataobj.TimeRangePredicate[dataobj.StreamsPredicate]{
						StartTime:    startTime,
						EndTime:      endTime,
						IncludeStart: true,
						IncludeEnd:   true,
					},
					Right: dataobj.LabelFilterPredicate{
						Name: "container",
						Keep: func(name, value string) bool {
							return value == "querier"
						},
					},
				})

				streams := make([]dataobj.Stream, 100)
				n, err := reader.Read(ctx, streams)
				if err != io.EOF {
					require.NoError(t, err)
				}

				if n == 0 && err == io.EOF {
					break
				}

				for _, stream := range streams {
					fmt.Printf("Found matching stream %s\n", stream.Labels)
				}
			}
			fmt.Printf("Finished lookup in %s\n", time.Since(startLookup))
		}
	*/
}
