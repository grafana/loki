package arrowagg

import (
	"hash/maphash"

	"github.com/apache/arrow-go/v18/arrow"
)

// hashSchema hashes the schema into h.
func hashSchema(h *maphash.Hash, schema *arrow.Schema) {
	_, _ = h.WriteString(schema.Endianness().String())

	// [arrow.Schema.Fields] creates a copy of the fields slice, so we want to
	// iterate over indices instead to avoid unnecessary allocations.
	for i := range schema.NumFields() {
		hashField(h, schema.Field(i))
	}

	hashMetadata(h, schema.Metadata())
}

// hashField hashes a field into h.
func hashField(h *maphash.Hash, field arrow.Field) {
	_, _ = h.WriteString(field.Name)
	_, _ = h.WriteString(field.Type.Fingerprint())

	if field.Nullable {
		_ = h.WriteByte(1)
	} else {
		_ = h.WriteByte(0)
	}

	hashMetadata(h, field.Metadata)
}

// hashMetadata hashes Arrow metadata into h.
func hashMetadata(h *maphash.Hash, md arrow.Metadata) {
	for _, key := range md.Keys() {
		value, _ := md.GetValue(key)

		_, _ = h.WriteString(key)
		_, _ = h.WriteString(value)
	}
}
