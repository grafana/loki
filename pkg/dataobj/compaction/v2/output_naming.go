package compactionv2

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// hashReader streams r through SHA-224 and returns the hex digest.
func hashReader(r io.Reader) (string, error) {
	h := sha256.New224()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	var sum [sha256.Size224]byte
	return hex.EncodeToString(h.Sum(sum[:0])), nil
}

// CompactedIndexPath returns the object-storage key for a compacted index object
// whose content is read from r, using the same content hash as ingestion object
// naming. Layout: indexes/tenants/<tenant>/<h2>/<hrest>.
func CompactedIndexPath(tenant string, r io.Reader) (string, error) {
	sum, err := hashReader(r)
	if err != nil {
		return "", err
	}
	return "indexes/tenants/" + tenant + "/" + sum[:2] + "/" + sum[2:], nil
}

// CompactedLogObjectPath returns the object-storage key for a compacted log
// object whose content is read from r. Layout: objects/tenants/<tenant>/<h2>/<hrest>.
func CompactedLogObjectPath(tenant string, r io.Reader) (string, error) {
	sum, err := hashReader(r)
	if err != nil {
		return "", err
	}
	return "objects/tenants/" + tenant + "/" + sum[:2] + "/" + sum[2:], nil
}
