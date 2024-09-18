// Package maxminddb provides a reader for the MaxMind DB file format.
package maxminddb

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"reflect"
)

const (
	// NotFound is returned by LookupOffset when a matched root record offset
	// cannot be found.
	NotFound = ^uintptr(0)

	dataSectionSeparatorSize = 16
)

var metadataStartMarker = []byte("\xAB\xCD\xEFMaxMind.com")

// Reader holds the data corresponding to the MaxMind DB file. Its only public
// field is Metadata, which contains the metadata from the MaxMind DB file.
//
// All of the methods on Reader are thread-safe. The struct may be safely
// shared across goroutines.
type Reader struct {
	nodeReader        nodeReader
	buffer            []byte
	decoder           decoder
	Metadata          Metadata
	ipv4Start         uint
	ipv4StartBitDepth int
	nodeOffsetMult    uint
	hasMappedFile     bool
}

// Metadata holds the metadata decoded from the MaxMind DB file. In particular
// it has the format version, the build time as Unix epoch time, the database
// type and description, the IP version supported, and a slice of the natural
// languages included.
type Metadata struct {
	Description              map[string]string `maxminddb:"description"`
	DatabaseType             string            `maxminddb:"database_type"`
	Languages                []string          `maxminddb:"languages"`
	BinaryFormatMajorVersion uint              `maxminddb:"binary_format_major_version"`
	BinaryFormatMinorVersion uint              `maxminddb:"binary_format_minor_version"`
	BuildEpoch               uint              `maxminddb:"build_epoch"`
	IPVersion                uint              `maxminddb:"ip_version"`
	NodeCount                uint              `maxminddb:"node_count"`
	RecordSize               uint              `maxminddb:"record_size"`
}

// FromBytes takes a byte slice corresponding to a MaxMind DB file and returns
// a Reader structure or an error.
func FromBytes(buffer []byte) (*Reader, error) {
	metadataStart := bytes.LastIndex(buffer, metadataStartMarker)

	if metadataStart == -1 {
		return nil, newInvalidDatabaseError("error opening database: invalid MaxMind DB file")
	}

	metadataStart += len(metadataStartMarker)
	metadataDecoder := decoder{buffer[metadataStart:]}

	var metadata Metadata

	rvMetadata := reflect.ValueOf(&metadata)
	_, err := metadataDecoder.decode(0, rvMetadata, 0)
	if err != nil {
		return nil, err
	}

	searchTreeSize := metadata.NodeCount * metadata.RecordSize / 4
	dataSectionStart := searchTreeSize + dataSectionSeparatorSize
	dataSectionEnd := uint(metadataStart - len(metadataStartMarker))
	if dataSectionStart > dataSectionEnd {
		return nil, newInvalidDatabaseError("the MaxMind DB contains invalid metadata")
	}
	d := decoder{
		buffer[searchTreeSize+dataSectionSeparatorSize : metadataStart-len(metadataStartMarker)],
	}

	nodeBuffer := buffer[:searchTreeSize]
	var nodeReader nodeReader
	switch metadata.RecordSize {
	case 24:
		nodeReader = nodeReader24{buffer: nodeBuffer}
	case 28:
		nodeReader = nodeReader28{buffer: nodeBuffer}
	case 32:
		nodeReader = nodeReader32{buffer: nodeBuffer}
	default:
		return nil, newInvalidDatabaseError("unknown record size: %d", metadata.RecordSize)
	}

	reader := &Reader{
		buffer:         buffer,
		nodeReader:     nodeReader,
		decoder:        d,
		Metadata:       metadata,
		ipv4Start:      0,
		nodeOffsetMult: metadata.RecordSize / 4,
	}

	reader.setIPv4Start()

	return reader, err
}

func (r *Reader) setIPv4Start() {
	if r.Metadata.IPVersion != 6 {
		return
	}

	nodeCount := r.Metadata.NodeCount

	node := uint(0)
	i := 0
	for ; i < 96 && node < nodeCount; i++ {
		node = r.nodeReader.readLeft(node * r.nodeOffsetMult)
	}
	r.ipv4Start = node
	r.ipv4StartBitDepth = i
}

// Lookup retrieves the database record for ip and stores it in the value
// pointed to by result. If result is nil or not a pointer, an error is
// returned. If the data in the database record cannot be stored in result
// because of type differences, an UnmarshalTypeError is returned. If the
// database is invalid or otherwise cannot be read, an InvalidDatabaseError
// is returned.
func (r *Reader) Lookup(ip net.IP, result any) error {
	if r.buffer == nil {
		return errors.New("cannot call Lookup on a closed database")
	}
	pointer, _, _, err := r.lookupPointer(ip)
	if pointer == 0 || err != nil {
		return err
	}
	return r.retrieveData(pointer, result)
}

// LookupNetwork retrieves the database record for ip and stores it in the
// value pointed to by result. The network returned is the network associated
// with the data record in the database. The ok return value indicates whether
// the database contained a record for the ip.
//
// If result is nil or not a pointer, an error is returned. If the data in the
// database record cannot be stored in result because of type differences, an
// UnmarshalTypeError is returned. If the database is invalid or otherwise
// cannot be read, an InvalidDatabaseError is returned.
func (r *Reader) LookupNetwork(
	ip net.IP,
	result any,
) (network *net.IPNet, ok bool, err error) {
	if r.buffer == nil {
		return nil, false, errors.New("cannot call Lookup on a closed database")
	}
	pointer, prefixLength, ip, err := r.lookupPointer(ip)

	network = r.cidr(ip, prefixLength)
	if pointer == 0 || err != nil {
		return network, false, err
	}

	return network, true, r.retrieveData(pointer, result)
}

// LookupOffset maps an argument net.IP to a corresponding record offset in the
// database. NotFound is returned if no such record is found, and a record may
// otherwise be extracted by passing the returned offset to Decode. LookupOffset
// is an advanced API, which exists to provide clients with a means to cache
// previously-decoded records.
func (r *Reader) LookupOffset(ip net.IP) (uintptr, error) {
	if r.buffer == nil {
		return 0, errors.New("cannot call LookupOffset on a closed database")
	}
	pointer, _, _, err := r.lookupPointer(ip)
	if pointer == 0 || err != nil {
		return NotFound, err
	}
	return r.resolveDataPointer(pointer)
}

func (r *Reader) cidr(ip net.IP, prefixLength int) *net.IPNet {
	// This is necessary as the node that the IPv4 start is at may
	// be at a bit depth that is less that 96, i.e., ipv4Start points
	// to a leaf node. For instance, if a record was inserted at ::/8,
	// the ipv4Start would point directly at the leaf node for the
	// record and would have a bit depth of 8. This would not happen
	// with databases currently distributed by MaxMind as all of them
	// have an IPv4 subtree that is greater than a single node.
	if r.Metadata.IPVersion == 6 &&
		len(ip) == net.IPv4len &&
		r.ipv4StartBitDepth != 96 {
		return &net.IPNet{IP: net.ParseIP("::"), Mask: net.CIDRMask(r.ipv4StartBitDepth, 128)}
	}

	mask := net.CIDRMask(prefixLength, len(ip)*8)
	return &net.IPNet{IP: ip.Mask(mask), Mask: mask}
}

// Decode the record at |offset| into |result|. The result value pointed to
// must be a data value that corresponds to a record in the database. This may
// include a struct representation of the data, a map capable of holding the
// data or an empty any value.
//
// If result is a pointer to a struct, the struct need not include a field
// for every value that may be in the database. If a field is not present in
// the structure, the decoder will not decode that field, reducing the time
// required to decode the record.
//
// As a special case, a struct field of type uintptr will be used to capture
// the offset of the value. Decode may later be used to extract the stored
// value from the offset. MaxMind DBs are highly normalized: for example in
// the City database, all records of the same country will reference a
// single representative record for that country. This uintptr behavior allows
// clients to leverage this normalization in their own sub-record caching.
func (r *Reader) Decode(offset uintptr, result any) error {
	if r.buffer == nil {
		return errors.New("cannot call Decode on a closed database")
	}
	return r.decode(offset, result)
}

func (r *Reader) decode(offset uintptr, result any) error {
	rv := reflect.ValueOf(result)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("result param must be a pointer")
	}

	if dser, ok := result.(deserializer); ok {
		_, err := r.decoder.decodeToDeserializer(uint(offset), dser, 0, false)
		return err
	}

	_, err := r.decoder.decode(uint(offset), rv, 0)
	return err
}

func (r *Reader) lookupPointer(ip net.IP) (uint, int, net.IP, error) {
	if ip == nil {
		return 0, 0, nil, errors.New("IP passed to Lookup cannot be nil")
	}

	ipV4Address := ip.To4()
	if ipV4Address != nil {
		ip = ipV4Address
	}
	if len(ip) == 16 && r.Metadata.IPVersion == 4 {
		return 0, 0, ip, fmt.Errorf(
			"error looking up '%s': you attempted to look up an IPv6 address in an IPv4-only database",
			ip.String(),
		)
	}

	bitCount := uint(len(ip) * 8)

	var node uint
	if bitCount == 32 {
		node = r.ipv4Start
	}
	node, prefixLength := r.traverseTree(ip, node, bitCount)

	nodeCount := r.Metadata.NodeCount
	if node == nodeCount {
		// Record is empty
		return 0, prefixLength, ip, nil
	} else if node > nodeCount {
		return node, prefixLength, ip, nil
	}

	return 0, prefixLength, ip, newInvalidDatabaseError("invalid node in search tree")
}

func (r *Reader) traverseTree(ip net.IP, node, bitCount uint) (uint, int) {
	nodeCount := r.Metadata.NodeCount

	i := uint(0)
	for ; i < bitCount && node < nodeCount; i++ {
		bit := uint(1) & (uint(ip[i>>3]) >> (7 - (i % 8)))

		offset := node * r.nodeOffsetMult
		if bit == 0 {
			node = r.nodeReader.readLeft(offset)
		} else {
			node = r.nodeReader.readRight(offset)
		}
	}

	return node, int(i)
}

func (r *Reader) retrieveData(pointer uint, result any) error {
	offset, err := r.resolveDataPointer(pointer)
	if err != nil {
		return err
	}
	return r.decode(offset, result)
}

func (r *Reader) resolveDataPointer(pointer uint) (uintptr, error) {
	resolved := uintptr(pointer - r.Metadata.NodeCount - dataSectionSeparatorSize)

	if resolved >= uintptr(len(r.buffer)) {
		return 0, newInvalidDatabaseError("the MaxMind DB file's search tree is corrupt")
	}
	return resolved, nil
}
