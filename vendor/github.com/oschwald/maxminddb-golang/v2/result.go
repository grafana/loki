package maxminddb

import (
	"errors"
	"math"
	"net/netip"
	"runtime"
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
// UnmarshalTypeError is returned. An InvalidDatabaseError is returned when the
// data is malformed or rejected by structural, resource, or schema validation,
// including maxsize and generated duplicate-field checks.
//
// An error will also be returned if there was an error during the
// Reader.Lookup call.
//
// If the Reader.Lookup call did not find a value for the IP address, no error
// will be returned and v will be unchanged.
//
// Reflection decoding limits each operation to 32,768 declared container child
// slots and a separate exact 2 MiB allowance for all materialized string,
// byte-slice, and dynamic map-key payload. Maps and slices reserve their
// children before allocation or traversal; repeated pointer targets share the
// same limits. Decoding into any activates the limits even for a root scalar.
// A standalone scalar decoded into a directly typed destination or a named
// empty-interface type is decoded without them because it cannot amplify.
// Custom unmarshalers control and must bound their own traversal and allocation.
// Call [Reader.Verify] before using an untrusted database with custom or
// low-level decoding, and keep the verified backing data unchanged for the
// Reader's lifetime.
func (r Result) Decode(v any) error {
	if r.err != nil {
		return r.err
	}
	if r.offset == notFound {
		return nil
	}
	if r.reader == nil || r.reader.buffer == nil {
		return errors.New("cannot call Decode on a closed database")
	}

	err := r.reader.decoder.Decode(r.offset, v)
	runtime.KeepAlive(r.reader)
	return err
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
// If the path is empty, the entire data structure is decoded into v. A non-empty
// path shares the operation limits described by [Result.Decode] across path
// navigation and the selected value. Every inspected map key consumes its full
// size from the shared payload allowance, and skipped inline containers consume
// child slots without following pointer targets.
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
	if r.reader == nil || r.reader.buffer == nil {
		return errors.New("cannot call DecodePath on a closed database")
	}
	err := r.reader.decoder.DecodePath(r.offset, path, v)
	runtime.KeepAlive(r.reader)
	return err
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
		var isIPv4 bool
		prefixLen, isIPv4 = r.ipv4PrefixLen(prefixLen)
		if !isIPv4 {
			return netip.PrefixFrom(zeroIP, prefixLen)
		}
	}

	prefix, _ := ip.Prefix(prefixLen)
	return prefix
}

func (r Result) ipv4PrefixLen(prefixLen int) (int, bool) {
	if r.reader != nil && r.reader.hasIPv4Subtree() {
		if prefixLen < r.reader.ipv4StartBitDepth {
			return prefixLen, false
		}
		return prefixLen - r.reader.ipv4StartBitDepth, true
	}

	if prefixLen < 96 {
		return prefixLen, false
	}

	return prefixLen - 96, true
}
