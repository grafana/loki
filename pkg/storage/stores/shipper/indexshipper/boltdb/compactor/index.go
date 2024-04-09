package compactor

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
)

const (
	chunkTimeRangeKeyV3   = '3'
	seriesRangeKeyV1      = '7'
	labelSeriesRangeKeyV1 = '8'
)

var ErrInvalidIndexKey = errors.New("invalid index key")

type InvalidIndexKeyError struct {
	HashKey  string
	RangeKey string
}

func newInvalidIndexKeyError(h, r []byte) InvalidIndexKeyError {
	return InvalidIndexKeyError{
		HashKey:  string(h),
		RangeKey: string(r),
	}
}

func (e InvalidIndexKeyError) Error() string {
	return fmt.Sprintf("%s: hash_key:%s range_key:%s", ErrInvalidIndexKey, e.HashKey, e.RangeKey)
}

func (e InvalidIndexKeyError) Is(target error) bool {
	return target == ErrInvalidIndexKey
}

func parseChunkRef(hashKey, rangeKey []byte) (retention.ChunkRef, bool, error) {
	componentsRef := getComponents()
	defer putComponents(componentsRef)
	components := componentsRef.components

	components = decodeRangeKey(rangeKey, components)
	if len(components) == 0 {
		return retention.ChunkRef{}, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}

	keyType := components[len(components)-1]
	if len(keyType) == 0 || keyType[0] != chunkTimeRangeKeyV3 {
		return retention.ChunkRef{}, false, nil
	}
	chunkID := components[len(components)-2]

	userID, hexFrom, hexThrough, ok := parseChunkID(chunkID)
	if !ok {
		return retention.ChunkRef{}, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}
	from, err := strconv.ParseInt(unsafeGetString(hexFrom), 16, 64)
	if err != nil {
		return retention.ChunkRef{}, false, err
	}
	through, err := strconv.ParseInt(unsafeGetString(hexThrough), 16, 64)
	if err != nil {
		return retention.ChunkRef{}, false, err
	}

	return retention.ChunkRef{
		UserID:   userID,
		SeriesID: seriesFromHash(hashKey),
		From:     model.Time(from),
		Through:  model.Time(through),
		ChunkID:  chunkID,
	}, true, nil
}

func parseChunkID(chunkID []byte) (userID []byte, hexFrom, hexThrough []byte, valid bool) {
	var (
		j, i int
		hex  []byte
	)

	for j < len(chunkID) {
		if chunkID[j] != '/' {
			j++
			continue
		}
		userID = chunkID[:j]
		hex = chunkID[j+1:]
		break
	}

	if len(userID) == 0 {
		return nil, nil, nil, false
	}

	// v12+ chunk id format `<user>/<fprint>/<start>:<end>:<checksum>`
	// older than v12 chunk id format `<user id>/<fingerprint>:<start time>:<end time>:<checksum>`
	if idx := bytes.IndexByte(hex, '/'); idx != -1 {
		// v12+ chunk id format, let us skip through the fingerprint using '/`
		hex = hex[idx+1:]
	} else {
		// older than v12 chunk id format, let us skip through the fingerprint using ':'
		_, i = readOneHexPart(hex)
		if i == 0 {
			return nil, nil, nil, false
		}
		hex = hex[i+1:]
	}
	hexFrom, i = readOneHexPart(hex)
	if i == 0 {
		return nil, nil, nil, false
	}
	hex = hex[i+1:]
	hexThrough, i = readOneHexPart(hex)
	if i == 0 {
		return nil, nil, nil, false
	}
	return userID, hexFrom, hexThrough, true
}

func readOneHexPart(hex []byte) (part []byte, i int) {
	for i < len(hex) {
		if hex[i] != ':' {
			i++
			continue
		}
		return hex[:i], i
	}
	return nil, 0
}

func parseLabelIndexSeriesID(hashKey, rangeKey []byte) ([]byte, bool, error) {
	componentsRef := getComponents()
	defer putComponents(componentsRef)
	components := componentsRef.components
	var seriesID []byte
	components = decodeRangeKey(rangeKey, components)
	if len(components) < 4 {
		return nil, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}
	keyType := components[len(components)-1]
	if len(keyType) == 0 {
		return nil, false, nil
	}
	switch keyType[0] {
	case labelSeriesRangeKeyV1:
		seriesID = components[1]
	case seriesRangeKeyV1:
		seriesID = components[0]
	default:
		return nil, false, nil
	}
	return seriesID, true, nil
}

type LabelSeriesRangeKey struct {
	SeriesID []byte
	UserID   []byte
	Name     []byte
}

func (l LabelSeriesRangeKey) String() string {
	return fmt.Sprintf("%s:%s:%s", l.SeriesID, l.UserID, l.Name)
}

func parseLabelSeriesRangeKey(hashKey, rangeKey []byte) (LabelSeriesRangeKey, bool, error) {
	rangeComponentsRef := getComponents()
	defer putComponents(rangeComponentsRef)
	rangeComponents := rangeComponentsRef.components
	hashComponentsRef := getComponents()
	defer putComponents(hashComponentsRef)
	hashComponents := hashComponentsRef.components

	rangeComponents = decodeRangeKey(rangeKey, rangeComponents)
	if len(rangeComponents) < 4 {
		return LabelSeriesRangeKey{}, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}
	keyType := rangeComponents[len(rangeComponents)-1]
	if len(keyType) == 0 || keyType[0] != labelSeriesRangeKeyV1 {
		return LabelSeriesRangeKey{}, false, nil
	}
	hashComponents = splitBytesBy(hashKey, ':', hashComponents)
	// 	> v10		HashValue:  fmt.Sprintf("%02d:%s:%s:%s", shard, bucket.hashKey , metricName, v.Name),
	// < v10		HashValue:  fmt.Sprintf("%s:%s:%s", bucket.hashKey, metricName, v.Name),

	if len(hashComponents) < 4 {
		return LabelSeriesRangeKey{}, false, newInvalidIndexKeyError(hashKey, rangeKey)
	}
	return LabelSeriesRangeKey{
		SeriesID: rangeComponents[1],
		Name:     hashComponents[len(hashComponents)-1],
		UserID:   hashComponents[len(hashComponents)-4],
	}, true, nil
}

func seriesFromHash(h []byte) (seriesID []byte) {
	var index int
	for i := range h {
		if h[i] == ':' {
			index++
		}
		if index == 2 {
			seriesID = h[i+1:]
			return
		}
	}
	return
}

// decodeKey decodes hash and range value from a boltdb key.
func decodeKey(k []byte) (hashValue, rangeValue []byte) {
	// hashValue + 0 + string(rangeValue)
	for i := range k {
		if k[i] == 0 {
			hashValue = k[:i]
			rangeValue = k[i+1:]
			return
		}
	}
	return
}

func splitBytesBy(value []byte, by byte, components [][]byte) [][]byte {
	components = components[:0]
	i, j := 0, 0
	for j < len(value) {
		if value[j] != by {
			j++
			continue
		}
		components = append(components, value[i:j])
		j++
		i = j
	}
	components = append(components, value[i:])
	return components
}

func decodeRangeKey(value []byte, components [][]byte) [][]byte {
	components = components[:0]
	i, j := 0, 0
	for j < len(value) {
		if value[j] != 0 {
			j++
			continue
		}
		components = append(components, value[i:j])
		j++
		i = j
	}
	return components
}
