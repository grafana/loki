package retention

import (
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"
	"go.etcd.io/bbolt"
)

var bucketName = []byte("index")

// todo test double open db file.
// markForDelete delete index entries for expired chunk in `in` and add chunkid to delete in `marker`.
// All of this inside a single transaction.
func markForDelete(in, marker *bbolt.DB, expiration ExpirationChecker, buckets []string) error {
	return in.Update(func(inTx *bbolt.Tx) error {
		return marker.Update(func(outTx *bbolt.Tx) error {
			bucket := inTx.Bucket(bucketName)
			if bucket == nil {
				return nil
			}
			bucketOut, err := outTx.CreateBucket(bucketName)
			if err != nil {
				return err
			}
			// Phase 1 we mark chunkID that needs to be deleted in marker DB
			c := bucket.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				ref, ok, err := parseChunkRef(decodeKey(k))
				if err != nil {
					return err
				}
				// skiping anything else than chunk index entries.
				if !ok {
					continue
				}
				if expiration.Expired(&ref) {
					if err := bucketOut.Put(ref.ChunkID, ref.SeriesID); err != nil {
						return err
					}
					if err := c.Delete(); err != nil {
						return err
					}
				}
			}
			// Phase 2 verify series that have marked chunks have still other chunks in the index.
			// If not this means we can delete labels index entries for the given series.
			cOut := bucketOut.Cursor()
			for chunkID, seriesID := cOut.First(); chunkID != nil; chunkID, seriesID = cOut.Next() {
				// for all buckets if seek to bucket.hashKey + ":" + string(seriesID) is not nil we still have chunks.
				var found bool
				for _, bucket := range buckets {
					if key, _ := c.Seek([]byte(bucket + ":" + string(seriesID))); key != nil {
						found = true
						break
					}
				}
				// we need to delete all index label entry for this series.
				if !found {
					// entries := []IndexEntry{
					// 	// Entry for metricName -> seriesID
					// 	{
					// 		TableName:  bucket.tableName,
					// 		HashValue:  bucket.hashKey + ":" + metricName,
					// 		RangeValue: encodeRangeKey(seriesRangeKeyV1, seriesID, nil, nil),
					// 		Value:      empty,
					// 	},
					// }

					// // Entries for metricName:labelName -> hash(value):seriesID
					// // We use a hash of the value to limit its length.
					// for _, v := range labels {
					// 	if v.Name == model.MetricNameLabel {
					// 		continue
					// 	}
					// 	valueHash := sha256bytes(v.Value)
					// 	entries = append(entries, IndexEntry{
					// 		TableName:  bucket.tableName,
					// 		HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, v.Name),
					// 		RangeValue: encodeRangeKey(labelSeriesRangeKeyV1, valueHash, seriesID, nil),
					// 		Value:      []byte(v.Value),
					// 	})
					// }
				}

			}
			return nil
		})
	})
}

func allBucketsHashes(config chunk.PeriodConfig, userID string) []string {
	return bucketsHashes(config.From.Time, config.From.Add(config.IndexTables.Period), config, userID)
}

func bucketsHashes(from, through model.Time, config chunk.PeriodConfig, userID string) []string {
	var (
		fromDay    = from.Unix() / int64(config.IndexTables.Period/time.Second)
		throughDay = through.Unix() / int64(config.IndexTables.Period/time.Second)
		result     = []string{}
	)
	for i := fromDay; i <= throughDay; i++ {
		result = append(result, fmt.Sprintf("%s:d%d", userID, i))
	}
	return result
}
