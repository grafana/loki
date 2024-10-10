//go:build appengine || plan9 || js || wasip1 || wasi
// +build appengine plan9 js wasip1 wasi

package maxminddb

import "io/ioutil"

// Open takes a string path to a MaxMind DB file and returns a Reader
// structure or an error. The database file is opened using a memory map
// on supported platforms. On platforms without memory map support, such
// as WebAssembly or Google App Engine, the database is loaded into memory.
// Use the Close method on the Reader object to return the resources to the system.
func Open(file string) (*Reader, error) {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	return FromBytes(bytes)
}

// Close returns the resources used by the database to the system.
func (r *Reader) Close() error {
	r.buffer = nil
	return nil
}
