// Package maxminddb provides a reader for the MaxMind DB file format.
//
// This package provides an API for reading MaxMind GeoIP2 and GeoLite2
// databases in the MaxMind DB file format (.mmdb files). The API is designed
// to be simple to use while providing high performance for IP geolocation
// lookups and related data.
//
// # Basic Usage
//
// The most common use case is looking up geolocation data for an IP address:
//
//	db, err := maxminddb.Open("GeoLite2-City.mmdb")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer db.Close()
//
//	ip, err := netip.ParseAddr("81.2.69.142")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	var record struct {
//		Country struct {
//			ISOCode string `maxminddb:"iso_code"`
//			Names   map[string]string `maxminddb:"names"`
//		} `maxminddb:"country"`
//		City struct {
//			Names map[string]string `maxminddb:"names"`
//		} `maxminddb:"city"`
//	}
//
//	err = db.Lookup(ip).Decode(&record)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	fmt.Printf("Country: %s\n", record.Country.Names["en"])
//	fmt.Printf("City: %s\n", record.City.Names["en"])
//
// # Database Types
//
// This library supports all MaxMind database types:
//   - GeoLite2/GeoIP2 City: Comprehensive location data including city, country, subdivisions
//   - GeoLite2/GeoIP2 Country: Country-level geolocation data
//   - GeoLite2 ASN: Autonomous System Number and organization data
//   - GeoIP2 Anonymous IP: Anonymous network and proxy detection
//   - GeoIP2 Enterprise: Enhanced City data with additional business fields
//   - GeoIP2 ISP: Internet service provider information
//   - GeoIP2 Domain: Second-level domain data
//   - GeoIP2 Connection Type: Connection type identification
//
// # Performance
//
// For maximum performance in high-throughput applications, consider:
//
//  1. Using custom struct types that only include the fields you need
//  2. Implementing the Unmarshaler interface for custom decoding
//  3. Reusing the Reader instance across multiple goroutines (it's thread-safe)
//
// # Custom Unmarshaling
//
// For custom decoding logic, you can implement the mmdbdata.Unmarshaler interface,
// similar to how encoding/json's json.Unmarshaler works. Types implementing this
// interface will automatically use custom decoding logic when used with Reader.Lookup:
//
//	type FastCity struct {
//		CountryISO string
//		CityName   string
//	}
//
//	func (c *FastCity) UnmarshalMaxMindDB(d *mmdbdata.Decoder) error {
//		// Custom decoding logic using d.ReadMap(), d.ReadString(), etc.
//		// Allows fine-grained control over how MaxMind DB data is decoded
//		// See mmdbdata package documentation and ExampleUnmarshaler for complete examples
//	}
//
// # Network Iteration
//
// You can iterate over all networks in a database:
//
//	for result := range db.Networks() {
//		var record struct {
//			Country struct {
//				ISOCode string `maxminddb:"iso_code"`
//			} `maxminddb:"country"`
//		}
//		err := result.Decode(&record)
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Printf("%s: %s\n", result.Prefix(), record.Country.ISOCode)
//	}
//
// # Database Files
//
// MaxMind provides both free (GeoLite2) and commercial (GeoIP2) databases:
//   - Free: https://dev.maxmind.com/geoip/geolite2-free-geolocation-data
//   - Commercial: https://www.maxmind.com/en/geoip2-databases
//
// # Thread Safety
//
// All Reader methods are thread-safe. The Reader can be safely shared across
// multiple goroutines.
package maxminddb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/oschwald/maxminddb-golang/v2/internal/decoder"
	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

const dataSectionSeparatorSize = 16

var metadataStartMarker = []byte("\xAB\xCD\xEFMaxMind.com")

// mmapCleanup holds the data needed to safely cleanup memory-mapped files.
type mmapCleanup struct {
	hasMapped *atomic.Bool
	data      []byte
}

// Reader holds the data corresponding to the MaxMind DB file. Its only public
// field is Metadata, which contains the metadata from the MaxMind DB file.
//
// All of the methods on Reader are thread-safe. The struct may be safely
// shared across goroutines.
type Reader struct {
	hasMappedFile     *atomic.Bool
	decoder           decoder.ReflectionDecoder
	buffer            []byte
	Metadata          Metadata
	ipv4Start         uint
	ipv4StartBitDepth int
	nodeOffsetMult    uint
}

// Metadata holds the metadata decoded from the MaxMind DB file.
//
// Key fields include:
//   - DatabaseType: indicates the structure of data records (e.g., "GeoIP2-City")
//   - Description: localized descriptions in various languages
//   - Languages: locale codes for which the database may contain localized data
//   - BuildEpoch: database build timestamp as Unix epoch seconds
//   - IPVersion: supported IP version (4 for IPv4-only, 6 for IPv4/IPv6)
//   - NodeCount: number of nodes in the search tree
//   - RecordSize: size in bits of each record in the search tree (24, 28, or 32)
//
// For detailed field descriptions, see the MaxMind DB specification:
// https://maxmind.github.io/MaxMind-DB/
type Metadata struct {
	// Description contains localized database descriptions.
	// Keys are language codes (e.g., "en", "zh-CN"), values are UTF-8 descriptions.
	Description map[string]string `maxminddb:"description"`

	// DatabaseType indicates the structure of data records associated with IP addresses.
	// Names starting with "GeoIP" are reserved for MaxMind databases.
	DatabaseType string `maxminddb:"database_type"`

	// Languages lists locale codes for which this database may contain localized data.
	// Records should not contain localized data for locales not in this array.
	Languages []string `maxminddb:"languages"`

	// BinaryFormatMajorVersion is the major version of the MaxMind DB binary format.
	// Current supported version is 2.
	BinaryFormatMajorVersion uint `maxminddb:"binary_format_major_version"`

	// BinaryFormatMinorVersion is the minor version of the MaxMind DB binary format.
	// Current supported version is 0.
	BinaryFormatMinorVersion uint `maxminddb:"binary_format_minor_version"`

	// BuildEpoch contains the database build timestamp as Unix epoch seconds.
	// Use BuildTime() method for a time.Time representation.
	BuildEpoch uint `maxminddb:"build_epoch"`

	// IPVersion indicates the IP version support:
	//   4: IPv4 addresses only
	//   6: Both IPv4 and IPv6 addresses
	IPVersion uint `maxminddb:"ip_version"`

	// NodeCount is the number of nodes in the search tree.
	NodeCount uint `maxminddb:"node_count"`

	// RecordSize is the size in bits of each record in the search tree.
	// Valid values are 24, 28, or 32.
	RecordSize uint `maxminddb:"record_size"`
}

// BuildTime returns the database build time as a time.Time.
// This is a convenience method that converts the BuildEpoch field
// from Unix epoch seconds to a time.Time value.
func (m Metadata) BuildTime() time.Time {
	return time.Unix(int64(m.BuildEpoch), 0)
}

type readerOptions struct{}

// ReaderOption are options for [Open] and [OpenBytes].
//
// This was added to allow for future options, e.g., for caching, without
// causing a breaking API change.
type ReaderOption func(*readerOptions)

// Open takes a string path to a MaxMind DB file and any options. It returns a
// Reader structure or an error. The database file is opened using a memory
// map on supported platforms. On platforms without memory map support, such
// as WebAssembly or Google App Engine, or if the memory map attempt fails
// due to lack of support from the filesystem, the database is loaded into memory.
// Use the Close method on the Reader object to return the resources to the system.
func Open(file string, options ...ReaderOption) (*Reader, error) {
	mapFile, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer mapFile.Close() //nolint:errcheck // error is generally not relevant

	stats, err := mapFile.Stat()
	if err != nil {
		return nil, err
	}

	size64 := stats.Size()
	// mmapping an empty file returns -EINVAL on Unix platforms,
	// and ERROR_FILE_INVALID on Windows.
	if size64 == 0 {
		return nil, errors.New("file is empty")
	}

	size := int(size64)
	// Check for overflow.
	if int64(size) != size64 {
		return nil, errors.New("file too large")
	}

	data, err := openMmap(mapFile, size)
	if err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			data, err = openFallback(mapFile, size)
			if err != nil {
				return nil, err
			}
			return OpenBytes(data, options...)
		}
		return nil, err
	}

	reader, err := OpenBytes(data, options...)
	if err != nil {
		_ = munmap(data)
		return nil, err
	}

	reader.hasMappedFile.Store(true)
	cleanup := &mmapCleanup{
		data:      data,
		hasMapped: reader.hasMappedFile,
	}
	runtime.AddCleanup(reader, func(mc *mmapCleanup) {
		if mc.hasMapped.CompareAndSwap(true, false) {
			_ = munmap(mc.data)
		}
	}, cleanup)
	return reader, nil
}

func openMmap(f *os.File, size int) (data []byte, err error) {
	rawConn, err := f.SyscallConn()
	if err != nil {
		return nil, err
	}

	if cerr := rawConn.Control(func(fd uintptr) {
		data, err = mmap(int(fd), size)
	}); cerr != nil {
		return nil, cerr
	}

	return data, err
}

func openFallback(f *os.File, size int) (data []byte, err error) {
	data = make([]byte, size)
	_, err = io.ReadFull(f, data)
	return data, err
}

// Close returns the resources used by the database to the system.
func (r *Reader) Close() error {
	var err error
	if r.hasMappedFile.CompareAndSwap(true, false) {
		err = munmap(r.buffer)
	}
	r.buffer = nil
	return err
}

// OpenBytes takes a byte slice corresponding to a MaxMind DB file and any
// options. It returns a Reader structure or an error.
func OpenBytes(buffer []byte, options ...ReaderOption) (*Reader, error) {
	opts := &readerOptions{}
	for _, option := range options {
		option(opts)
	}

	metadataStart := bytes.LastIndex(buffer, metadataStartMarker)

	if metadataStart == -1 {
		return nil, mmdberrors.NewInvalidDatabaseError(
			"error opening database: invalid MaxMind DB file",
		)
	}

	metadataStart += len(metadataStartMarker)
	metadataDecoder := decoder.New(buffer[metadataStart:])

	var metadata Metadata

	err := metadataDecoder.Decode(0, &metadata)
	if err != nil {
		return nil, err
	}

	// Check for integer overflow in search tree size calculation
	if metadata.NodeCount > 0 && metadata.RecordSize > 0 {
		recordSizeQuarter := metadata.RecordSize / 4
		if recordSizeQuarter > 0 {
			maxNodes := ^uint(0) / recordSizeQuarter
			if metadata.NodeCount > maxNodes {
				return nil, mmdberrors.NewInvalidDatabaseError("database tree size would overflow")
			}
		}
	}

	searchTreeSize := metadata.NodeCount * (metadata.RecordSize / 4)
	dataSectionStart := searchTreeSize + dataSectionSeparatorSize
	dataSectionEnd := uint(metadataStart - len(metadataStartMarker))
	if dataSectionStart > dataSectionEnd {
		return nil, mmdberrors.NewInvalidDatabaseError("the MaxMind DB contains invalid metadata")
	}
	d := decoder.New(
		buffer[searchTreeSize+dataSectionSeparatorSize : metadataStart-len(metadataStartMarker)],
	)

	reader := &Reader{
		buffer:         buffer,
		decoder:        d,
		Metadata:       metadata,
		ipv4Start:      0,
		nodeOffsetMult: metadata.RecordSize / 4,
		hasMappedFile:  &atomic.Bool{},
	}

	err = reader.setIPv4Start()
	if err != nil {
		return nil, err
	}

	return reader, nil
}

// Lookup retrieves the database record for ip and returns a Result, which can
// be used to decode the data.
func (r *Reader) Lookup(ip netip.Addr) Result {
	if r.buffer == nil {
		return Result{err: errors.New("cannot call Lookup on a closed database")}
	}
	pointer, prefixLen, err := r.lookupPointer(ip)
	if err != nil {
		return Result{
			ip:        ip,
			prefixLen: uint8(prefixLen),
			err:       err,
		}
	}
	if pointer == 0 {
		return Result{
			ip:        ip,
			prefixLen: uint8(prefixLen),
			offset:    notFound,
		}
	}
	offset, err := r.resolveDataPointer(pointer)
	return Result{
		reader:    r,
		ip:        ip,
		offset:    uint(offset),
		prefixLen: uint8(prefixLen),
		err:       err,
	}
}

// LookupOffset returns the Result for the specified offset. Note that
// netip.Prefix returned by Networks will be invalid when using LookupOffset.
func (r *Reader) LookupOffset(offset uintptr) Result {
	if r.buffer == nil {
		return Result{err: errors.New("cannot call LookupOffset on a closed database")}
	}

	return Result{reader: r, offset: uint(offset)}
}

func (r *Reader) setIPv4Start() error {
	if r.Metadata.IPVersion != 6 {
		r.ipv4StartBitDepth = 96
		return nil
	}

	nodeCount := r.Metadata.NodeCount

	node := uint(0)
	i := 0
	for ; i < 96 && node < nodeCount; i++ {
		var err error
		node, err = readNodeBySize(r.buffer, node*r.nodeOffsetMult, 0, r.Metadata.RecordSize)
		if err != nil {
			return err
		}
	}
	r.ipv4Start = node
	r.ipv4StartBitDepth = i

	return nil
}

var zeroIP = netip.MustParseAddr("::")

func (r *Reader) lookupPointer(ip netip.Addr) (uint, int, error) {
	if r.Metadata.IPVersion == 4 && ip.Is6() {
		return 0, 0, fmt.Errorf(
			"error looking up '%s': you attempted to look up an IPv6 address in an IPv4-only database",
			ip.String(),
		)
	}

	node, prefixLength, err := r.traverseTree(ip, 0, 128)
	if err != nil {
		return 0, 0, err
	}

	nodeCount := r.Metadata.NodeCount
	if node == nodeCount {
		// Record is empty
		return 0, prefixLength, nil
	} else if node > nodeCount {
		return node, prefixLength, nil
	}

	return 0, prefixLength, mmdberrors.NewInvalidDatabaseError("invalid node in search tree")
}

// readNodeBySize reads a node value from the buffer based on record size and bit.
func readNodeBySize(buffer []byte, offset, bit, recordSize uint) (uint, error) {
	bufferLen := uint(len(buffer))
	switch recordSize {
	case 24:
		offset += bit * 3
		if offset > bufferLen-3 {
			return 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed: insufficient buffer for 24-bit node read",
			)
		}
		return (uint(buffer[offset]) << 16) |
			(uint(buffer[offset+1]) << 8) |
			uint(buffer[offset+2]), nil
	case 28:
		if bit == 0 {
			if offset > bufferLen-4 {
				return 0, mmdberrors.NewInvalidDatabaseError(
					"bounds check failed: insufficient buffer for 28-bit node read",
				)
			}
			return ((uint(buffer[offset+3]) & 0xF0) << 20) |
				(uint(buffer[offset]) << 16) |
				(uint(buffer[offset+1]) << 8) |
				uint(buffer[offset+2]), nil
		}
		if offset > bufferLen-7 {
			return 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed: insufficient buffer for 28-bit node read",
			)
		}
		return ((uint(buffer[offset+3]) & 0x0F) << 24) |
			(uint(buffer[offset+4]) << 16) |
			(uint(buffer[offset+5]) << 8) |
			uint(buffer[offset+6]), nil
	case 32:
		offset += bit * 4
		if offset > bufferLen-4 {
			return 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed: insufficient buffer for 32-bit node read",
			)
		}
		return (uint(buffer[offset]) << 24) |
			(uint(buffer[offset+1]) << 16) |
			(uint(buffer[offset+2]) << 8) |
			uint(buffer[offset+3]), nil
	default:
		return 0, mmdberrors.NewInvalidDatabaseError("unsupported record size")
	}
}

// readNodePairBySize reads both left (bit=0) and right (bit=1) child pointers
// for a node at the given base offset according to the record size. This reduces
// duplicate bound checks and byte fetches when both children are needed.
func readNodePairBySize(buffer []byte, baseOffset, recordSize uint) (left, right uint, err error) {
	bufferLen := uint(len(buffer))
	switch recordSize {
	case 24:
		// Each child is 3 bytes; total 6 bytes starting at baseOffset
		if baseOffset > bufferLen-6 {
			return 0, 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed: insufficient buffer for 24-bit node pair read",
			)
		}
		o := baseOffset
		left = (uint(buffer[o]) << 16) | (uint(buffer[o+1]) << 8) | uint(buffer[o+2])
		o += 3
		right = (uint(buffer[o]) << 16) | (uint(buffer[o+1]) << 8) | uint(buffer[o+2])
		return left, right, nil
	case 28:
		// Left uses high nibble of shared byte, right uses low nibble.
		// Layout: [A B C S][D E F] where S provides 4 shared bits for each child
		if baseOffset > bufferLen-7 {
			return 0, 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed: insufficient buffer for 28-bit node pair read",
			)
		}
		// Left child (bit=0): uses high nibble of shared byte
		shared := uint(buffer[baseOffset+3])
		left = ((shared & 0xF0) << 20) |
			(uint(buffer[baseOffset]) << 16) |
			(uint(buffer[baseOffset+1]) << 8) |
			uint(buffer[baseOffset+2])
		// Right child (bit=1): uses low nibble of shared byte, next 3 bytes
		right = ((shared & 0x0F) << 24) |
			(uint(buffer[baseOffset+4]) << 16) |
			(uint(buffer[baseOffset+5]) << 8) |
			uint(buffer[baseOffset+6])
		return left, right, nil
	case 32:
		// Each child is 4 bytes; total 8 bytes
		if baseOffset > bufferLen-8 {
			return 0, 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed: insufficient buffer for 32-bit node pair read",
			)
		}
		o := baseOffset
		left = (uint(buffer[o]) << 24) |
			(uint(buffer[o+1]) << 16) |
			(uint(buffer[o+2]) << 8) |
			uint(buffer[o+3])
		o += 4
		right = (uint(buffer[o]) << 24) |
			(uint(buffer[o+1]) << 16) |
			(uint(buffer[o+2]) << 8) |
			uint(buffer[o+3])
		return left, right, nil
	default:
		return 0, 0, mmdberrors.NewInvalidDatabaseError("unsupported record size")
	}
}

func (r *Reader) traverseTree(ip netip.Addr, node uint, stopBit int) (uint, int, error) {
	switch r.Metadata.RecordSize {
	case 24:
		return r.traverseTree24(ip, node, stopBit)
	case 28:
		return r.traverseTree28(ip, node, stopBit)
	case 32:
		return r.traverseTree32(ip, node, stopBit)
	default:
		return 0, 0, mmdberrors.NewInvalidDatabaseError(
			"unsupported record size: %d",
			r.Metadata.RecordSize,
		)
	}
}

func (r *Reader) traverseTree24(ip netip.Addr, node uint, stopBit int) (uint, int, error) {
	i := 0
	if ip.Is4() {
		i = r.ipv4StartBitDepth
		node = r.ipv4Start
	}
	nodeCount := r.Metadata.NodeCount
	buffer := r.buffer
	bufferLen := uint(len(buffer))
	ip16 := ip.As16()

	for ; i < stopBit && node < nodeCount; i++ {
		byteIdx := i >> 3
		bitPos := 7 - (i & 7)
		bit := (uint(ip16[byteIdx]) >> bitPos) & 1

		baseOffset := node * 6
		offset := baseOffset + bit*3

		if offset > bufferLen-3 {
			return 0, 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed during tree traversal",
			)
		}

		node = (uint(buffer[offset]) << 16) |
			(uint(buffer[offset+1]) << 8) |
			uint(buffer[offset+2])
	}

	return node, i, nil
}

func (r *Reader) traverseTree28(ip netip.Addr, node uint, stopBit int) (uint, int, error) {
	i := 0
	if ip.Is4() {
		i = r.ipv4StartBitDepth
		node = r.ipv4Start
	}
	nodeCount := r.Metadata.NodeCount
	buffer := r.buffer
	bufferLen := uint(len(buffer))
	ip16 := ip.As16()

	for ; i < stopBit && node < nodeCount; i++ {
		byteIdx := i >> 3
		bitPos := 7 - (i & 7)
		bit := (uint(ip16[byteIdx]) >> bitPos) & 1

		baseOffset := node * 7
		offset := baseOffset + bit*4

		if baseOffset > bufferLen-4 || offset > bufferLen-3 {
			return 0, 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed during tree traversal",
			)
		}

		sharedByte := uint(buffer[baseOffset+3])
		mask := uint(0xF0 >> (bit * 4))
		shift := 20 + bit*4
		nibble := ((sharedByte & mask) << shift)

		node = nibble |
			(uint(buffer[offset]) << 16) |
			(uint(buffer[offset+1]) << 8) |
			uint(buffer[offset+2])
	}

	return node, i, nil
}

func (r *Reader) traverseTree32(ip netip.Addr, node uint, stopBit int) (uint, int, error) {
	i := 0
	if ip.Is4() {
		i = r.ipv4StartBitDepth
		node = r.ipv4Start
	}
	nodeCount := r.Metadata.NodeCount
	buffer := r.buffer
	bufferLen := uint(len(buffer))
	ip16 := ip.As16()

	for ; i < stopBit && node < nodeCount; i++ {
		byteIdx := i >> 3
		bitPos := 7 - (i & 7)
		bit := (uint(ip16[byteIdx]) >> bitPos) & 1

		baseOffset := node * 8
		offset := baseOffset + bit*4

		if offset > bufferLen-4 {
			return 0, 0, mmdberrors.NewInvalidDatabaseError(
				"bounds check failed during tree traversal",
			)
		}

		node = (uint(buffer[offset]) << 24) |
			(uint(buffer[offset+1]) << 16) |
			(uint(buffer[offset+2]) << 8) |
			uint(buffer[offset+3])
	}

	return node, i, nil
}

func (r *Reader) resolveDataPointer(pointer uint) (uintptr, error) {
	// Check for integer underflow: pointer must be greater than nodeCount + separator
	minPointer := r.Metadata.NodeCount + dataSectionSeparatorSize
	if pointer >= minPointer {
		resolved := uintptr(pointer - minPointer)
		bufferLen := uintptr(len(r.buffer))
		if resolved < bufferLen {
			return resolved, nil
		}
		// Error case - bounds exceeded
		return 0, mmdberrors.NewInvalidDatabaseError("the MaxMind DB file's search tree is corrupt")
	}
	// Error case - underflow
	return 0, mmdberrors.NewInvalidDatabaseError("the MaxMind DB file's search tree is corrupt")
}
