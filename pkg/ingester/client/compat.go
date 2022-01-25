package client

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	offset64 = 14695981039346656037
	prime64  = 1099511628211
)

// hashNew initializes a new fnv64a hash value.
func hashNew() uint64 {
	return offset64
}

// LabelsToKeyString is used to form a string to be used as
// the hashKey. Don't print, use l.String() for printing.
func LabelsToKeyString(l labels.Labels) string {
	// We are allocating 1024, even though most series are less than 600b long.
	// But this is not an issue as this function is being inlined when called in a loop
	// and buffer allocated is a static buffer and not a dynamic buffer on the heap.
	b := make([]byte, 0, 1024)
	return string(l.Bytes(b))
}

// Fingerprint runs the same algorithm as Prometheus labelSetToFingerprint()
func Fingerprint(labels labels.Labels) model.Fingerprint {
	sum := hashNew()
	for _, label := range labels {
		sum = hashAddString(sum, label.Name)
		sum = hashAddByte(sum, model.SeparatorByte)
		sum = hashAddString(sum, label.Value)
		sum = hashAddByte(sum, model.SeparatorByte)
	}
	return model.Fingerprint(sum)
}
