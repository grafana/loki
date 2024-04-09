package bloom

import "github.com/prometheus/common/model"

type Metadata interface {
	Version() uint32
	NumSeries() uint64
	NumChunks() uint64
	Size() uint64 // bytes

	// timestamps
	From() int64
	Through() int64

	// series
	FromFingerprint() model.Fingerprint
	ThroughFingerprint() model.Fingerprint
}

type Iterator[K any, V any] interface {
	Next() bool
	Err() error
	At() V
	Seek(K) Iterator[K, V]
}

type Block interface {
	SeriesIterator() Iterator[model.Fingerprint, []byte]
}
