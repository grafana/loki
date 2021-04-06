package retention

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"
	"go.etcd.io/bbolt"
)

var (
	bucketName    = []byte("index")
	chunkBucket   = []byte("chunks")
	seriesBucket  = []byte("series")
	empty         = []byte("-")
	logMetricName = "logs"
)

// todo test double open db file.
// markForDelete delete index entries for expired chunk in `in` and add chunkid to delete in `marker`.
// All of this inside a single transaction.
func markForDelete(in, marker *bbolt.DB, expiration ExpirationChecker, config chunk.PeriodConfig) error {
	return in.Update(func(inTx *bbolt.Tx) error {
		return marker.Update(func(outTx *bbolt.Tx) error {
			bucket := inTx.Bucket(bucketName)
			if bucket == nil {
				return nil
			}
			deleteChunkBucket, err := outTx.CreateBucket(chunkBucket)
			if err != nil {
				return err
			}
			deleteSeriesBucket, err := outTx.CreateBucket(seriesBucket)
			if err != nil {
				return err
			}
			// Phase 1 we mark chunkID that needs to be deleted in marker DB
			c := bucket.Cursor()
			var aliveChunk bool
			if err := forAllChunkRef(c, func(ref *ChunkRef) error {
				if expiration.Expired(ref) {
					if err := deleteChunkBucket.Put(ref.ChunkID, empty); err != nil {
						return err
					}
					// todo: we should not overrides series for different userid. just merge both
					if err := deleteSeriesBucket.Put(ref.SeriesID, ref.UserID); err != nil {
						return err
					}
					if err := c.Delete(); err != nil {
						return err
					}
					return nil
				}
				// we found a key that will stay.
				aliveChunk = true
				return nil
			}); err != nil {
				return err
			}
			// shortcircuit: no chunks remaining we can delete everything.
			if !aliveChunk {
				if err := inTx.DeleteBucket(bucketName); err != nil {
					return err
				}
				return outTx.DeleteBucket(seriesBucket)
			}
			// Phase 2 verify series that have marked chunks have still other chunks in the index per buckets.
			// If not this means we can delete labels index entries for the given series.
			seriesCursor := deleteSeriesBucket.Cursor()
		Outer:
			for seriesID, userID := seriesCursor.First(); seriesID != nil; seriesID, userID = seriesCursor.Next() {
				// for all buckets if seek to bucket.hashKey + ":" + string(seriesID) is not nil we still have chunks.
				bucketHashes := allBucketsHashes(config, unsafeGetString(userID))
				for _, bucketHash := range bucketHashes {
					if key, _ := c.Seek([]byte(bucketHash + ":" + string(seriesID))); key != nil {
						continue Outer
					}
					// this bucketHash doesn't contains the given series. Let's remove it.
					if err := forAllLabelRef(c, bucketHash, seriesID, config, func(keyType rune) error {
						if err := c.Delete(); err != nil {
							return err
						}
						return nil
					}); err != nil {
						return err
					}
				}
			}
			// we don't need the series bucket anymore.
			return outTx.DeleteBucket(seriesBucket)
		})
	})
}

func forAllChunkRef(c *bbolt.Cursor, callback func(ref *ChunkRef) error) error {
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		ref, ok, err := parseChunkRef(decodeKey(k))
		if err != nil {
			return err
		}
		// skiping anything else than chunk index entries.
		if !ok {
			continue
		}
		if err := callback(ref); err != nil {
			return err
		}
	}
	return nil
}

func forAllLabelRef(c *bbolt.Cursor, bucketHash string, seriesID []byte, config chunk.PeriodConfig, callback func(keyType rune) error) error {
	// todo reuse memory and refactor
	var (
		prefix       string
		components   [][]byte
		seriesIDRead []byte
	)
	// todo refactor ParseLabelRef. => keyType,SeriesID
	switch config.Schema {
	case "v11":
		shard := binary.BigEndian.Uint32(seriesID) % config.RowShards
		prefix = fmt.Sprintf("%02d:%s:%s", shard, bucketHash, logMetricName)
	default:
		prefix = fmt.Sprintf("%s:%s", bucketHash, logMetricName)
	}
	for k, _ := c.Seek([]byte(prefix)); k != nil; k, _ = c.Next() {
		_, rv := decodeKey(k)
		components = decodeRangeKey(rv, components)
		if len(components) > 4 {
			continue
		}
		keyType := components[len(components)-1]
		if len(keyType) == 0 {
			continue
		}
		switch keyType[0] {
		case labelSeriesRangeKeyV1:
			seriesIDRead = components[1]
		case seriesRangeKeyV1:
			seriesIDRead = components[0]
		default:
			continue
		}
		if !bytes.Equal(seriesID, seriesIDRead) {
			continue
		}
		if err := callback(rune(keyType[0])); err != nil {
			return err
		}
	}

	return nil
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
