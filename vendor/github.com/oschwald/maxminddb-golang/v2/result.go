package maxminddb

import (
	"math"
	"net/netip"
)

const notFound uint = math.MaxUint

// Result holds the result of the database lookup.
type Result struct {
	ip        netip.Addr
	err       error
	reader    *Reader
	offset    uint
	prefixLen uint8
}

// Decode unmarshals the data from the data section into the value pointed to
// by v. If v is nil or not a pointer, an error is returned. If the data in
// the database record cannot be stored in v because of type differences, an
// UnmarshalTypeError is returned. If the database is invalid or otherwise
// cannot be read, an InvalidDatabaseError is returned.
//
// An error will also be returned if there was an error during the
// Reader.Lookup call.
//
// If the Reader.Lookup call did not find a value for the IP address, no error
// will be returned and v will be unchanged.
func (r Result) Decode(v any) error {
	if r.err != nil {
		return r.err
	}
	if r.offset == notFound {
		return nil
	}

	return r.reader.decoder.Decode(r.offset, v)
}

// DecodePath unmarshals a value from data section into v, following the
// specified path.
//
// The v parameter should be a pointer to the value where the decoded data
// will be stored. If v is nil or not a pointer, an error is returned. If the
// data in the database record cannot be stored in v because of type
// differences, an UnmarshalTypeError is returned.
//
// The path is a variadic list of keys (strings) and/or indices (ints) that
// describe the nested structure to traverse in the data to reach the desired
// value.
//
// For maps, string path elements are used as keys.
// For arrays, int path elements are used as indices. A negative offset will
// return values from the end of the array, e.g., -1 will return the last
// element.
//
// If the path is empty, the entire data structure is decoded into v.
//
// To check if a path exists (rather than relying on zero values), decode
// into a pointer and check if it remains nil:
//
//	var city *string
//	err := result.DecodePath(&city, "city", "names", "en")
//	if err != nil {
//		// Handle error
//	}
//	if city == nil {
//		// Path not found
//	} else {
//		// Path exists, city contains the value
//	}
//
// Returns an error if:
//   - the path is invalid
//   - the data cannot be decoded into the type of v
//   - v is not a pointer or the database record cannot be stored in v due to
//     type mismatch
//   - the Result does not contain valid data
//
// Example usage:
//
//	var city string
//	err := result.DecodePath(&city, "city", "names", "en")
//
//	var geonameID int
//	err := result.DecodePath(&geonameID, "subdivisions", 0, "geoname_id")
func (r Result) DecodePath(v any, path ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.offset == notFound {
		return nil
	}
	return r.reader.decoder.DecodePath(r.offset, path, v)
}

// Err provides a way to check whether there was an error during the lookup
// without calling Result.Decode. If there was an error, it will also be
// returned from Result.Decode.
func (r Result) Err() error {
	return r.err
}

// Found will return true if the IP was found in the search tree. It will
// return false if the IP was not found or if there was an error.
func (r Result) Found() bool {
	return r.err == nil && r.offset != notFound
}

// Offset returns the offset of the record in the database. This can be
// passed to (*Reader).LookupOffset. It can also be used as a unique
// identifier for the data record in the particular database to cache the data
// record across lookups. Note that while the offset uniquely identifies the
// data record, other data in Result may differ between lookups. The offset
// is only valid for the current database version. If you update the database
// file, you must invalidate any cache associated with the previous version.
func (r Result) Offset() uintptr {
	return uintptr(r.offset)
}

// Prefix returns the netip.Prefix representing the network associated with
// the data record in the database.
func (r Result) Prefix() netip.Prefix {
	ip := r.ip
	prefixLen := int(r.prefixLen)

	if ip.Is4() {
		// This is necessary as the node that the IPv4 start is at may
		// be at a bit depth that is less that 96, i.e., ipv4Start points
		// to a leaf node. For instance, if a record was inserted at ::/8,
		// the ipv4Start would point directly at the leaf node for the
		// record and would have a bit depth of 8. This would not happen
		// with databases currently distributed by MaxMind as all of them
		// have an IPv4 subtree that is greater than a single node.
		if prefixLen < 96 {
			return netip.PrefixFrom(zeroIP, prefixLen)
		}
		prefixLen -= 96
	}

	prefix, _ := ip.Prefix(prefixLen)
	return prefix
}
