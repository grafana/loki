package tenant

import (
	"fmt"
	"iter"
	"strings"
)

const (
	// metadataSeparator separates the tenant ID from the metadata. The format is
	// "tenantID:key=value" (e.g. "123456:product=k6"). The colon is not a valid
	// character in tenant IDs, making it safe to use as a separator.
	metadataSeparator = ':'
	// metadataKVSeparator separates individual key value pairs of metadata. The
	// format is "key=value" (e.g., "source=test"). The equal sign is not a valid
	// character in tenant IDs, making it safe to use as a separator.
	metadataKVSeparator = '='
)

var validMetadataChars [256]bool

func init() {
	validMetadataChars[metadataSeparator] = true
	validMetadataChars[metadataKVSeparator] = true

	for c := 'a'; c <= 'z'; c++ {
		validMetadataChars[c] = true
	}
	for c := 'A'; c <= 'Z'; c++ {
		validMetadataChars[c] = true
	}
	for c := '0'; c <= '9'; c++ {
		validMetadataChars[c] = true
	}
	for _, c := range "-_" {
		validMetadataChars[c] = true
	}
}

type errMetadataUnsupportedCharacter struct {
	pos      int
	metadata string
}

func (e *errMetadataUnsupportedCharacter) Error() string {
	return fmt.Sprintf(
		"metadata '%s' contains unsupported character '%c'",
		e.metadata,
		e.metadata[e.pos],
	)
}

type Metadata struct {
	source string // invariant: b, err := ParseMetadata(a.source); err == nil && a == b
}

// NewMetadata returns tenant ID metadata from a source string.
//
// The format for metadata must be a colon-delimited list of key value pairs,
// lexicographically sorted by key. Examples:
// - :key=value
// - :key=value:foo=bar
//
// If you don't trust the source to be already valid, use ParseMetadata instead.
func NewMetadata(source string) Metadata {
	return Metadata{source: source}
}

// ParseMetadata validates and constructs a [Metadata]. See [NewMetadata].
//
// It doesn't allocate.
func ParseMetadata(source string) (Metadata, error) {
	if err := ValidMetadata(source); err != nil || source == "" {
		return Metadata{}, err
	}

	remaining := source
	if remaining[0] != metadataSeparator {
		return Metadata{}, errMalformedMetadata{source: source, reason: fmt.Sprintf("missing '%c' at start", metadataSeparator)}
	}
	prevKey := "" // < than every other string
	for len(remaining) > 0 {
		next := strings.IndexByte(remaining[1:], metadataSeparator)
		if next < 0 {
			next = len(remaining)
		} else {
			next += 1 // Add back the ':'
		}

		kv := remaining[:next]
		remaining = remaining[next:]

		key, _, ok := stringsCut(kv, metadataKVSeparator)
		if !ok {
			return Metadata{}, errMalformedMetadata{source: source, reason: fmt.Sprintf("no key-value separator '%c' in %q", metadataKVSeparator, kv)}
		}
		if prevKey >= key {
			return Metadata{}, errMalformedMetadata{source: source, reason: "keys must be unique and sorted"}
		}
		prevKey = key
	}
	return NewMetadata(source), nil
}

// ValidMetadata returns an error if the metadata is invalid, nil otherwise.
// Metadata must contain only lowercase letters, digits, '-', and '='.
func ValidMetadata(s string) error {
	for i := 0; i < len(s); i++ {
		if !validMetadataChars[s[i]] {
			return &errMetadataUnsupportedCharacter{metadata: s, pos: i}
		}
	}
	if len(s) > MaxMetadataLength {
		return fmt.Errorf("metadata too long: %d", len(s))
	}
	return nil
}

func (m Metadata) Encode() string {
	return m.source
}

// IsEmpty returns true iff metadata holds no key-value pairs.
func (m Metadata) IsEmpty() bool {
	return m.source == ""
}

// Divide iterates over Metadata, mapping each key-value pair to a new Metadata
// that only contains it.
//
// It doesn't allocate.
func (m Metadata) Divide() iter.Seq[Metadata] {
	return func(yield func(Metadata) bool) {
		remaining := m.source
		for len(remaining) > 0 {
			next := strings.IndexByte(remaining[1:], metadataSeparator)
			if next < 0 {
				next = len(remaining)
			} else {
				next += 1 // Add back the ':'
			}
			if !yield(NewMetadata(remaining[:next])) {
				return
			}
			remaining = remaining[next:]
		}
	}
}

// Iter returns a new iterator that yields key/value pairs sorted by keys.
//
// It doesn't allocate.
func (m Metadata) Iter() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for sub := range m.Divide() {
			k, v, _ := stringsCut(sub.Encode()[1:], metadataKVSeparator)
			if !yield(k, v) {
				return
			}
		}
	}
}

// Set a key value pair.
//
// It is O(n) over the size of Metadata and allocates.
func (m *Metadata) Set(key string, val string) {
	var source strings.Builder
	inserted := false
	for prevKey, prevVal := range m.Iter() {
		if inserted {
			writeMetadataKV(&source, prevKey, prevVal)
			continue
		}

		switch strings.Compare(prevKey, key) {
		case -1: // prevKey < key
			writeMetadataKV(&source, prevKey, prevVal)
		case 0: // prevKey == key, key replaces prevKey
			writeMetadataKV(&source, key, val)
			inserted = true
		case 1: // prevKey > key, insert key before it
			writeMetadataKV(&source, key, val)
			writeMetadataKV(&source, prevKey, prevVal)
			inserted = true
		}
	}
	if !inserted {
		writeMetadataKV(&source, key, val)
	}
	m.source = source.String()
}

func writeMetadataKV(into *strings.Builder, key, val string) {
	_ = into.WriteByte(metadataSeparator)
	_, _ = into.WriteString(key)
	_ = into.WriteByte(metadataKVSeparator)
	_, _ = into.WriteString(val)
}

// With returns a new Metadata, setting key to value.
//
// It is O(n) over the size of Metadata and allocates.
func (m Metadata) With(key string, val string) Metadata {
	m.Set(key, val)
	return m
}

// Has checks whether a specific metadata key is present.
func (m Metadata) Has(key string) bool {
	_, ok := m.Get(key)
	return ok
}

// Get the value set for key.
func (m Metadata) Get(key string) (string, bool) {
	for k, v := range m.Iter() {
		if k == key {
			return v, true
		}
	}
	return "", false
}

// WithTenant encodes the metadata as a tenant-prefixed string.
// The format is "tenantID:key1=val1:key2=val2" with keys sorted alphabetically.
func (m Metadata) WithTenant(tenantID string) string {
	return tenantID + m.source
}
