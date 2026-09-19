package wirecodec

import "fmt"

// dictionary accumulates strings and assigns monotonically increasing indices.
// It is used during marshaling (add) and unmarshaling (lookup).
type dictionary struct {
	keys    []string
	indices map[string]uint64
}

// newDictionary creates a dictionary pre-populated with the given keys.
func newDictionary(keys []string) *dictionary {
	d := &dictionary{
		keys:    keys,
		indices: make(map[string]uint64, len(keys)),
	}
	for i, k := range keys {
		d.indices[k] = uint64(i)
	}
	return d
}

// add inserts a string into the dictionary if not already present and returns
// its index.
func (d *dictionary) add(s string) uint64 {
	if idx, ok := d.indices[s]; ok {
		return idx
	}
	idx := uint64(len(d.keys))
	d.keys = append(d.keys, s)
	if d.indices == nil {
		d.indices = make(map[string]uint64)
	}
	d.indices[s] = idx
	return idx
}

// lookup returns the string at the given index. Returns an error if the index
// is out of bounds.
func (d *dictionary) lookup(i uint64) (string, error) {
	if i >= uint64(len(d.keys)) {
		return "", fmt.Errorf("wirecodec: dictionary index %d out of range (size %d)", i, len(d.keys))
	}
	return d.keys[i], nil
}
